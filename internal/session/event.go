package session

import (
	"time"

	"github.com/ayu-v0/agent-cortex/internal/model"
)

type EventType string

const (
	EventStarted     EventType = "started"
	EventTextDelta   EventType = "text_delta"
	EventToolDelta   EventType = "tool_delta"
	EventFinished    EventType = "finished"
	EventInterrupted EventType = "interrupted"
	EventFailed      EventType = "failed"
)

type Event struct {
	SessionID     string
	Type          EventType
	TextDelta     string
	ToolCallDelta *model.ToolCallDelta
	FinishReason  string
	Model         string
	Usage         model.Usage
	RawID         string
	Err           string
	At            time.Time
}
