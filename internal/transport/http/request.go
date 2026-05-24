package http

import (
	"strings"

	"github.com/ayu-v0/agent-cortex/internal/memory"
)

type createMemoryRequest struct {
	ID        string    `json:"id" binding:"required"`
	AgentID   string    `json:"agent_id" binding:"required"`
	UserID    string    `json:"user_id" binding:"required"`
	Question  string    `json:"question" binding:"required"`
	Answer    string    `json:"answer" binding:"required"`
	Embedding []float32 `json:"embedding" binding:"omitempty"`
}

func (r createMemoryRequest) toMemory() memory.Memory {
	return memory.Memory{
		ID:        r.ID,
		AgentID:   r.AgentID,
		UserID:    r.UserID,
		Question:  r.Question,
		Answer:    r.Answer,
		Content:   strings.TrimSpace(r.Question + "\n" + r.Answer),
		Embedding: r.Embedding,
	}
}

type searchMemoryRequest struct {
	AgentID   string    `json:"agent_id" binding:"required"`
	Embedding []float32 `json:"embedding" binding:"required"`
	Limit     int       `json:"limit" binding:"omitempty,min=1,max=100"`
}
