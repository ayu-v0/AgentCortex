package http

import (
	"errors"
	"fmt"
	"log"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/utils"
	"github.com/gin-gonic/gin"
)

const defaultMemoryMarkdownDir = ".memory"

type handlers struct {
	memoryService     *memory.Service
	embedder          embedding.Embedder
	memoryMarkdownDir string
	memoryMarkdownMu  sync.Mutex
}

func newHandlers(service *memory.Service, embedder embedding.Embedder, memoryMarkdownDir string) *handlers {
	memoryMarkdownDir = strings.TrimSpace(memoryMarkdownDir)
	if memoryMarkdownDir == "" {
		memoryMarkdownDir = defaultMemoryMarkdownDir
	}
	return &handlers{
		memoryService:     service,
		embedder:          embedder,
		memoryMarkdownDir: memoryMarkdownDir,
	}
}

func (h *handlers) health(c *gin.Context) {
	writeJSON(c, stdhttp.StatusOK, healthResponse{Status: "ok"})
}

func (h *handlers) createMemory(c *gin.Context) {
	var req createMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorJSON(c, stdhttp.StatusBadRequest, err.Error())
		return
	}

	if err := h.memoryService.Save(req.toMemory()); err != nil {
		writeHTTPError(c, err)
		return
	}

	if err := h.ensureMemoryMarkdown(req); err != nil {
		log.Printf("memory markdown error: %v", err)
		writeErrorJSON(c, stdhttp.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(c, stdhttp.StatusCreated, createMemoryResponse{ID: req.ID})
}

func (h *handlers) searchMemory(c *gin.Context) {
	var req searchMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorJSON(c, stdhttp.StatusBadRequest, err.Error())
		return
	}

	vector, err := h.embedder.Embed(c.Request.Context(), embedding.Input{Text: req.Question})
	if err != nil {
		writeHTTPError(c, err)
		return
	}

	results, err := h.memoryService.Search(req.AgentID, req.UserID, []float32(vector), req.searchLimit())
	if err != nil {
		writeHTTPError(c, err)
		return
	}
	if len(results) > 0 {
		if err := h.replaceSearchContentFromMarkdown(req.UserID, req.AgentID, results); err != nil {
			log.Printf("memory markdown search error: %v", err)
			writeErrorJSON(c, stdhttp.StatusInternalServerError, "internal server error")
			return
		}
	}

	writeJSON(c, stdhttp.StatusOK, searchMemoryResponse{Results: results})
}

func (h *handlers) replaceSearchContentFromMarkdown(userID, agentID string, results []memory.SearchResult) error {
	filename, err := memoryMarkdownFilename(userID, agentID)
	if err != nil {
		return err
	}

	content, err := os.ReadFile(filepath.Join(h.memoryMarkdownDir, filename))
	if err != nil {
		return fmt.Errorf("read memory markdown: %w", err)
	}

	entries := memoryMarkdownEntriesByID(string(content))
	for i := range results {
		if entry, ok := entries[results[i].ID]; ok {
			results[i].Content = entry
		}
	}
	return nil
}

func memoryMarkdownEntriesByID(content string) map[string]string {
	entries := make(map[string]string)
	for _, chunk := range strings.Split(content, "\n---\n") {
		entry := strings.TrimSpace(chunk)
		if start := strings.Index(entry, "## Memory\n"); start >= 0 {
			entry = entry[start:]
		}
		id, ok := memoryIDFromMarkdownEntry(entry)
		if ok {
			entries[id] = entry
		}
	}
	return entries
}

func memoryIDFromMarkdownEntry(entry string) (string, bool) {
	for _, line := range strings.Split(entry, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "MemoryID:") {
			id := strings.TrimSpace(strings.TrimPrefix(line, "MemoryID:"))
			return id, id != ""
		}
	}
	return "", false
}

func (h *handlers) ensureMemoryMarkdown(req createMemoryRequest) error {
	filename, err := memoryMarkdownFilename(req.UserID, req.AgentID)
	if err != nil {
		return err
	}

	h.memoryMarkdownMu.Lock()
	defer h.memoryMarkdownMu.Unlock()

	exists, err := utils.MarkdownFileExists(h.memoryMarkdownDir, filename)
	if err != nil {
		return fmt.Errorf("check memory markdown: %w", err)
	}
	if exists {
		if _, err := utils.AppendMarkdownFile(h.memoryMarkdownDir, filename, memoryMarkdownAppendContent(req)); err != nil {
			return fmt.Errorf("append memory markdown: %w", err)
		}
		return nil
	}

	_, err = utils.CreateMarkdownFile(h.memoryMarkdownDir, filename, memoryMarkdownContent(req))
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			if _, err := utils.AppendMarkdownFile(h.memoryMarkdownDir, filename, memoryMarkdownAppendContent(req)); err != nil {
				return fmt.Errorf("append memory markdown after concurrent create: %w", err)
			}
			return nil
		}
		return fmt.Errorf("create memory markdown: %w", err)
	}
	return nil
}

func memoryMarkdownFilename(userID, agentID string) (string, error) {
	userID = sanitizeMarkdownFilenamePart(userID)
	agentID = sanitizeMarkdownFilenamePart(agentID)
	if userID == "" || agentID == "" {
		return "", fmt.Errorf("memory markdown filename requires user and agent IDs")
	}
	return userID + "_" + agentID + "_Memory.md", nil
}

func sanitizeMarkdownFilenamePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	var builder strings.Builder
	lastUnderscore := false
	for _, r := range value {
		allowed := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_'
		if allowed {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}

	return strings.Trim(builder.String(), "_")
}

func memoryMarkdownContent(req createMemoryRequest) string {
	return fmt.Sprintf(`# Memory

UserID: %s
AgentID: %s

%s`, req.UserID, req.AgentID, memoryMarkdownEntry(req))
}

func memoryMarkdownAppendContent(req createMemoryRequest) string {
	return "\n---\n\n" + memoryMarkdownEntry(req)
}

func memoryMarkdownEntry(req createMemoryRequest) string {
	return fmt.Sprintf(`## Memory

MemoryID: %s

## Question

%s

## Answer

%s
`, req.ID, req.Question, req.Answer)
}
