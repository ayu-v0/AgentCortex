package http

import (
	"fmt"
	"strings"

	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/model"
)

const (
	defaultSearchLimit    = 10
	maxSearchLimit        = 100
	messagesMaxCount      = 100
	messagesMaxTotalChars = 24000
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
		ID:        strings.TrimSpace(r.ID),
		AgentID:   strings.TrimSpace(r.AgentID),
		UserID:    strings.TrimSpace(r.UserID),
		Question:  strings.TrimSpace(r.Question),
		Answer:    strings.TrimSpace(r.Answer),
		Content:   strings.TrimSpace(r.Question + "\n" + r.Answer),
		Embedding: r.Embedding,
	}
}

type searchMemoryRequest struct {
	AgentID  string `json:"agent_id" binding:"required"`
	UserID   string `json:"user_id" binding:"required"`
	Question string `json:"question" binding:"required"`
	Limit    *int   `json:"limit" binding:"omitempty,min=1,max=100"`
}

func (r searchMemoryRequest) searchLimit() int {
	if r.Limit == nil {
		return defaultSearchLimit
	}
	return *r.Limit
}

type qaMessage struct {
	Role    string `json:"role" binding:"required"`
	Content string `json:"content" binding:"required"`
}

type qaStreamRequest struct {
	AgentID  string      `json:"agent_id" binding:"required"`
	UserID   string      `json:"user_id" binding:"required"`
	Messages []qaMessage `json:"messages" binding:"required,min=1,max=100"`
}

func (r qaStreamRequest) validate() error {
	if strings.TrimSpace(r.AgentID) == "" {
		return fmt.Errorf("agent_id is required")
	}
	if strings.TrimSpace(r.UserID) == "" {
		return fmt.Errorf("user_id is required")
	}
	if len(r.Messages) == 0 {
		return fmt.Errorf("messages is required")
	}
	if len(r.Messages) > messagesMaxCount {
		return fmt.Errorf("messages exceeds max count %d", messagesMaxCount)
	}

	totalChars := 0
	for i, msg := range r.Messages {
		role := model.Role(strings.TrimSpace(msg.Role))
		if role != model.RoleSystem && role != model.RoleUser && role != model.RoleAssistant {
			return fmt.Errorf("messages[%d].role must be system, user, or assistant", i)
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			return fmt.Errorf("messages[%d].content is required", i)
		}
		totalChars += len([]rune(content))
	}
	if totalChars > messagesMaxTotalChars {
		return fmt.Errorf("messages exceeds max total chars %d", messagesMaxTotalChars)
	}
	if model.Role(strings.TrimSpace(r.Messages[len(r.Messages)-1].Role)) != model.RoleUser {
		return fmt.Errorf("messages last role must be user")
	}
	return nil
}

func (r qaStreamRequest) toModelRequest() (model.Request, error) {
	if err := r.validate(); err != nil {
		return model.Request{}, err
	}

	messages := make([]model.Message, 0, len(r.Messages))
	for _, msg := range r.Messages {
		messages = append(messages, model.Message{
			Role:    model.Role(strings.TrimSpace(msg.Role)),
			Content: strings.TrimSpace(msg.Content),
		})
	}
	return model.Request{Messages: messages}, nil
}

func (r qaStreamRequest) lastUserQuestion() (string, error) {
	if err := r.validate(); err != nil {
		return "", err
	}
	return strings.TrimSpace(r.Messages[len(r.Messages)-1].Content), nil
}
