package http

import (
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/gin-gonic/gin"
)

type healthResponse struct {
	Status string `json:"status"`
}

type createMemoryResponse struct {
	ID string `json:"id"`
}

type searchMemoryResponse struct {
	Results []memory.SearchResult `json:"results"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type qaTextDeltaEvent struct {
	Text string `json:"text"`
}

type qaWarningEvent struct {
	Message string `json:"message"`
}

type qaHeartbeatEvent struct {
	TS string `json:"ts"`
}

type qaFinishedEvent struct {
	Answer         string `json:"answer,omitempty"`
	MemoryID       string `json:"memory_id,omitempty"`
	MemorySaved    bool   `json:"memory_saved"`
	MarkdownSynced bool   `json:"markdown_synced"`
	FinishReason   string `json:"finish_reason,omitempty"`
}

type qaErrorEvent struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(c *gin.Context, status int, body any) {
	c.JSON(status, body)
}

func writeErrorJSON(c *gin.Context, status int, message string) {
	writeJSON(c, status, errorResponse{Error: message})
}
