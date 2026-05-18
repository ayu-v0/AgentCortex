package http

import "github.com/ayu-v0/agent-cortex/internal/memory"

type createMemoryRequest struct {
	ID        string    `json:"id" binding:"required"`
	AgentID   string    `json:"agent_id" binding:"required"`
	Content   string    `json:"content" binding:"required"`
	Embedding []float32 `json:"embedding" binding:"required"`
}

func (r createMemoryRequest) toMemory() memory.Memory {
	return memory.Memory{
		ID:        r.ID,
		AgentID:   r.AgentID,
		Content:   r.Content,
		Embedding: r.Embedding,
	}
}

type searchMemoryRequest struct {
	AgentID   string    `json:"agent_id" binding:"required"`
	Embedding []float32 `json:"embedding" binding:"required"`
	Limit     int       `json:"limit" binding:"omitempty,min=1,max=100"`
}
