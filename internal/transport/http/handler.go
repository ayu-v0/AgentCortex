package http

import (
	"fmt"
	"log"
	stdhttp "net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/model"
	"github.com/ayu-v0/agent-cortex/internal/retrieval"
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
	retrievalService  *retrieval.Service
	conversations     conversationRecorder
	memoryMarkdownDir string
	memoryMarkdownMu  sync.Mutex
	memoryIDSeq       atomic.Uint64
	now               func() time.Time
}

type conversationRecorder interface {
	AppendCompletedTurn(userID, agentID, sessionID, question, answer string) error
}

func newHandlers(service *memory.Service, embedder embedding.Embedder, streamer model.Streamer, retrievalService *retrieval.Service, conversations conversationRecorder, memoryMarkdownDir string) *handlers {
	memoryMarkdownDir = strings.TrimSpace(memoryMarkdownDir)
	if memoryMarkdownDir == "" {
		memoryMarkdownDir = defaultMemoryMarkdownDir
	}
	return &handlers{
		memoryService:     service,
		embedder:          embedder,
		modelStreamer:     streamer,
		retrievalService:  retrievalService,
		conversations:     conversations,
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
	if err := validateMemoryPathID(req.UserID); err != nil {
		writeHTTPError(c, err)
		return
	}
	if err := validateMemoryPathID(req.AgentID); err != nil {
		writeHTTPError(c, err)
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
	if err := validateMemoryPathID(req.UserID); err != nil {
		writeHTTPError(c, err)
		return
	}
	if err := validateMemoryPathID(req.AgentID); err != nil {
		writeHTTPError(c, err)
		return
	}
	if err := validateSessionID(req.SessionID); err != nil {
		writeHTTPError(c, err)
		return
	}

	if h.retrievalService != nil {
		results, err := h.retrievalService.Retrieve(c.Request.Context(), retrieval.Request{
			AgentID:   req.AgentID,
			UserID:    req.UserID,
			SessionID: req.SessionID,
			Question:  req.Question,
			Limit:     req.searchLimit(),
		})
		if err != nil {
			writeHTTPError(c, err)
			return
		}
		writeJSON(c, stdhttp.StatusOK, searchMemoryResponse{Results: results})
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

	if h.conversations != nil {
		if err := h.conversations.AppendCompletedTurn(req.UserID, req.AgentID, req.SessionID, question, answer); err != nil {
			log.Printf("conversation history save failed: %v", err)
			if writeErr := writeSSE(c, "warning", qaWarningEvent{Message: "conversation history save failed"}); writeErr != nil {
				return writeErr
			}
		}
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
	finished.MarkdownSynced = result.markdownSynced
	if err != nil {
		if writeErr := writeSSE(c, "warning", qaWarningEvent{Message: fmt.Sprintf("memory save failed: %v", publicMessageFromError(err))}); writeErr != nil {
			return writeErr
		}
		return nil
	}

	finished.MemoryID = item.ID
	finished.MemorySaved = true
	return nil
}

type saveMemoryResult struct {
	markdownSynced bool
}

func (h *handlers) saveMemory(item memory.Memory) (saveMemoryResult, error) {
	item = normalizeMemory(item)
	if err := validateMemoryPathID(item.UserID); err != nil {
		return saveMemoryResult{}, err
	}
	if err := validateMemoryPathID(item.AgentID); err != nil {
		return saveMemoryResult{}, err
	}
	if err := validateMemoryMarkerID(item.ID); err != nil {
		return saveMemoryResult{}, err
	}
	item.RecordedAt = h.now().UTC()
	item.StorageVersion = memory.DailyMarkdownStorageVersion

	h.memoryMarkdownMu.Lock()
	defer h.memoryMarkdownMu.Unlock()

	stored, found, err := h.memoryService.FindByID(item.ID)
	if err != nil {
		return saveMemoryResult{}, err
	}
	if found {
		if stored.StorageVersion != memory.DailyMarkdownStorageVersion || !memory.SameContent(stored, item) {
			return saveMemoryResult{}, memory.ErrMemoryConflict
		}
		if stored.RecordedAt.IsZero() {
			return saveMemoryResult{}, ErrMemoryMarkdown
		}
		entry, ok, err := h.readMemoryMarkdownEntry(stored.UserID, stored.AgentID, stored.RecordedAt, stored.ID)
		if err != nil {
			return saveMemoryResult{}, err
		}
		if !ok || !parsedEntryMatchesMemory(entry, item) {
			return saveMemoryResult{}, ErrMemoryMarkdown
		}
		return saveMemoryResult{markdownSynced: true}, nil
	}

	orphan, found, err := findMemoryMarkdownEntry(h.memoryMarkdownDir, item.UserID, item.AgentID, item.ID)
	if err != nil {
		return saveMemoryResult{}, err
	}
	if found {
		if !parsedEntryMatchesMemory(orphan, item) {
			return saveMemoryResult{}, memory.ErrMemoryConflict
		}
		item.RecordedAt = orphan.RecordedAt
		if err := h.memoryService.Save(item); err != nil {
			return saveMemoryResult{markdownSynced: true}, err
		}
		return saveMemoryResult{markdownSynced: true}, nil
	}

	if err := writeMemoryMarkdown(h.memoryMarkdownDir, item); err != nil {
		log.Printf("memory markdown error: %v", err)
		return saveMemoryResult{markdownSynced: false}, err
	}
	if err := h.memoryService.Save(item); err != nil {
		return saveMemoryResult{markdownSynced: true}, err
	}

	return saveMemoryResult{markdownSynced: true}, nil
}

func normalizeMemory(item memory.Memory) memory.Memory {
	item.ID = strings.TrimSpace(item.ID)
	item.AgentID = strings.TrimSpace(item.AgentID)
	item.UserID = strings.TrimSpace(item.UserID)
	item.Question = strings.TrimSpace(item.Question)
	item.Answer = strings.TrimSpace(item.Answer)
	item.Content = strings.TrimSpace(item.Question + "\n" + item.Answer)
	return item
}

func (h *handlers) nextMemoryID() string {
	sequence := h.memoryIDSeq.Add(1)
	return fmt.Sprintf("memory-%d-%d", h.now().UnixNano(), sequence)
}

func (h *handlers) replaceSearchContentFromMarkdown(userID, agentID string, results []memory.SearchResult) error {
	byPath := make(map[string][]int)
	for i := range results {
		if results[i].RecordedAt.IsZero() {
			return ErrMemoryMarkdown
		}
		dir, filename, err := memoryMarkdownDailyPath(h.memoryMarkdownDir, userID, agentID, results[i].RecordedAt)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, filename)
		byPath[path] = append(byPath[path], i)
	}

	h.memoryMarkdownMu.Lock()
	defer h.memoryMarkdownMu.Unlock()

	for path, indexes := range byPath {
		entries, err := readMemoryMarkdownEntries(path)
		if err != nil {
			return err
		}
		for _, index := range indexes {
			entry, ok := entries[results[index].ID]
			if !ok {
				return ErrMemoryMarkdown
			}
			results[index].Content = entry.Content
		}
	}
	return nil
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

func (h *handlers) readMemoryMarkdownEntry(userID, agentID string, recordedAt time.Time, memoryID string) (parsedMemoryMarkdownEntry, bool, error) {
	dir, filename, err := memoryMarkdownDailyPath(h.memoryMarkdownDir, userID, agentID, recordedAt)
	if err != nil {
		return parsedMemoryMarkdownEntry{}, false, err
	}
	entries, err := readMemoryMarkdownEntries(filepath.Join(dir, filename))
	if err != nil {
		return parsedMemoryMarkdownEntry{}, false, err
	}
	entry, ok := entries[memoryID]
	return entry, ok, nil
}
