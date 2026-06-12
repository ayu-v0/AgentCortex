package http

import (
	"github.com/ayu-v0/agent-cortex/internal/conversation"
	"github.com/ayu-v0/agent-cortex/internal/model"
)

func validateSessionID(value string) error {
	if err := conversation.ValidateSessionID(value); err != nil {
		return model.ErrInvalidRequest
	}
	return nil
}
