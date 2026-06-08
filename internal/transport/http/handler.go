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
	"sync/atomic"
	"time"
	"unicode"

	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/model"
	"github.com/ayu-v0/agent-cortex/internal/utils"
	"github.com/gin-gonic/gin"
)

const (
	defaultMemoryMarkdownDir = ".memory"
)

var heartbeatInterval = 10 * time.Second

func SetHeartbeatIntervalForTest(interval time.Duration) time.Duration {
	previous := heartbeatInterval
	heartbeatInterval = interval
	return previous
}

type handlers struct {
	memoryService     *memory.Service
	embedder          embedding.Embedder
	modelStreamer     model.Streamer
	memoryMarkdownDir string
	memoryMarkdownMu  sync.Mutex
	memoryIDSeq       atomic.Uint64
	now               func() time.Time
}

func newHandlers(service *memory.Service, embedder embedding.Embedder, streamer model.Streamer, memoryMarkdownDir string) *handlers {
	memoryMarkdownDir = strings.TrimSpace(memoryMarkdownDir)
	if memoryMarkdownDir == "" {
		memoryMarkdownDir = defaultMemoryMarkdownDir
	}
	return &handlers{
		memoryService:     service,
		embedder:          embedder,
		modelStreamer:     streamer,
		memoryMarkdownDir: memoryMarkdownDir,
		now:               time.Now,
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

	if _, err := h.saveMemory(req.toMemory()); err != nil {
		writeHTTPError(c, err)
		return
	}

	writeJSON(c, stdhttp.StatusCreated, createMemoryResponse{ID: strings.TrimSpace(req.ID)})
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

func (h *handlers) qaStream(c *gin.Context) {
	if h.modelStreamer == nil {
		writeErrorJSON(c, stdhttp.StatusInternalServerError, "internal server error")
		return
	}

	var req qaStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorJSON(c, stdhttp.StatusBadRequest, err.Error())
		return
	}

	modelRequest, err := req.toModelRequest()
	if err != nil {
		writeErrorJSON(c, stdhttp.StatusBadRequest, err.Error())
		return
	}
	question, err := req.lastUserQuestion()
	if err != nil {
		writeErrorJSON(c, stdhttp.StatusBadRequest, err.Error())
		return
	}

	stream, err := h.modelStreamer.Stream(c.Request.Context(), modelRequest)
	if err != nil {
		writeHTTPError(c, err)
		return
	}

	prepareSSE(c)

	var answerBuilder strings.Builder
	finishReason := ""
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if err := writeSSE(c, "heartbeat", qaHeartbeatEvent{TS: h.now().Format(time.RFC3339)}); err != nil {
				return
			}
		case event, ok := <-stream:
			if !ok {
				finished := qaFinishedEvent{
					Answer:         answerBuilder.String(),
					MemorySaved:    false,
					MarkdownSynced: false,
					FinishReason:   finishReason,
				}
				if err := h.persistQATurn(c, req, question, answerBuilder.String(), &finished); err != nil {
					return
				}
				_ = writeSSE(c, "finished", finished)
				return
			}
			if event.Err != nil {
				_ = writeSSE(c, "error", qaErrorEvent{
					Code:    "model_stream_failed",
					Message: publicMessageFromError(event.Err),
				})
				return
			}
			if event.TextDelta != "" {
				answerBuilder.WriteString(event.TextDelta)
				if err := writeSSE(c, "text_delta", qaTextDeltaEvent{Text: event.TextDelta}); err != nil {
					return
				}
			}
			if strings.TrimSpace(event.FinishReason) != "" {
				finishReason = event.FinishReason
			}
		}
	}
}

func (h *handlers) persistQATurn(c *gin.Context, req qaStreamRequest, question, answer string, finished *qaFinishedEvent) error {
	if strings.TrimSpace(answer) == "" {
		return nil
	}

	vector, err := h.embedder.Embed(c.Request.Context(), embedding.Input{Text: question})
	if err != nil {
		if writeErr := writeSSE(c, "warning", qaWarningEvent{Message: fmt.Sprintf("embedding failed: %v", publicMessageFromError(err))}); writeErr != nil {
			return writeErr
		}
		return nil
	}

	item := memory.Memory{
		ID:        h.nextMemoryID(),
		AgentID:   strings.TrimSpace(req.AgentID),
		UserID:    strings.TrimSpace(req.UserID),
		Question:  question,
		Answer:    strings.TrimSpace(answer),
		Content:   strings.TrimSpace(question + "\n" + answer),
		Embedding: []float32(vector),
	}

	result, err := h.saveMemory(item)
	if err != nil {
		if writeErr := writeSSE(c, "warning", qaWarningEvent{Message: fmt.Sprintf("memory save failed: %v", publicMessageFromError(err))}); writeErr != nil {
			return writeErr
		}
		return nil
	}

	finished.MemoryID = item.ID
	finished.MemorySaved = true
	finished.MarkdownSynced = result.markdownSynced
	if !result.markdownSynced {
		if writeErr := writeSSE(c, "warning", qaWarningEvent{Message: "memory markdown sync failed"}); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

type saveMemoryResult struct {
	markdownSynced bool
}

func (h *handlers) saveMemory(item memory.Memory) (saveMemoryResult, error) {
	if err := h.memoryService.Save(item); err != nil {
		return saveMemoryResult{}, err
	}

	if err := h.ensureMemoryMarkdown(item); err != nil {
		log.Printf("memory markdown error: %v", err)
		return saveMemoryResult{markdownSynced: false}, nil
	}

	return saveMemoryResult{markdownSynced: true}, nil
}

func (h *handlers) nextMemoryID() string {
	sequence := h.memoryIDSeq.Add(1)
	return fmt.Sprintf("memory-%d-%d", h.now().UnixNano(), sequence)
}

func (h *handlers) replaceSearchContentFromMarkdown(userID, agentID string, results []memory.SearchResult) error {
	filename, err := memoryMarkdownFilename(userID, agentID)
	if err != nil {
		return err
	}

	content, err := os.ReadFile(filepath.Join(h.memoryMarkdownDir, filename))
	if err != nil {
		return errors.Join(ErrMemoryMarkdown, err)
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

func (h *handlers) ensureMemoryMarkdown(item memory.Memory) error {
	filename, err := memoryMarkdownFilename(item.UserID, item.AgentID)
	if err != nil {
		return err
	}
	recordedAt := h.now().UTC()

	h.memoryMarkdownMu.Lock()
	defer h.memoryMarkdownMu.Unlock()

	exists, err := utils.MarkdownFileExists(h.memoryMarkdownDir, filename)
	if err != nil {
		return errors.Join(ErrMemoryMarkdown, err)
	}
	if exists {
		if _, err := utils.AppendMarkdownFile(h.memoryMarkdownDir, filename, memoryMarkdownAppendContent(item, recordedAt)); err != nil {
			return errors.Join(ErrMemoryMarkdown, err)
		}
		return nil
	}

	_, err = utils.CreateMarkdownFile(h.memoryMarkdownDir, filename, memoryMarkdownContent(item, recordedAt))
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			if _, err := utils.AppendMarkdownFile(h.memoryMarkdownDir, filename, memoryMarkdownAppendContent(item, recordedAt)); err != nil {
				return errors.Join(ErrMemoryMarkdown, err)
			}
			return nil
		}
		return errors.Join(ErrMemoryMarkdown, err)
	}
	return nil
}

func memoryMarkdownFilename(userID, agentID string) (string, error) {
	userID = sanitizeMarkdownFilenamePart(userID)
	agentID = sanitizeMarkdownFilenamePart(agentID)
	if userID == "" || agentID == "" {
		return "", model.ErrInvalidRequest
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

func memoryMarkdownContent(item memory.Memory, recordedAt time.Time) string {
	return fmt.Sprintf(`# Memory

UserID: %s
AgentID: %s

%s`, item.UserID, item.AgentID, memoryMarkdownEntry(item, recordedAt))
}

func memoryMarkdownAppendContent(item memory.Memory, recordedAt time.Time) string {
	return "\n---\n\n" + memoryMarkdownEntry(item, recordedAt)
}

func memoryMarkdownEntry(item memory.Memory, recordedAt time.Time) string {
	return fmt.Sprintf(`## Memory

MemoryID: %s
RecordedAt: %s

## Question

%s

## Answer

%s
`, item.ID, recordedAt.Format(time.RFC3339), item.Question, item.Answer)
}
