package session

import (
	"context"
	"time"
)

type State string

const (
	StateCreated     State = "created"
	StateRunning     State = "running"
	StateInterrupted State = "interrupted"
	StateCompleted   State = "completed"
	StateFailed      State = "failed"
	StateExpired     State = "expired"
)

type Status struct {
	SessionID string
	State     State
	StartedAt time.Time
	UpdatedAt time.Time
	Metadata  map[string]string
	Err       string
}

type sessionState struct {
	id        string
	state     State
	startedAt time.Time
	updatedAt time.Time
	metadata  map[string]string
	errText   string
	cancel    context.CancelFunc
	events    chan Event
}

func terminal(state State) bool {
	return state == StateInterrupted || state == StateCompleted || state == StateFailed || state == StateExpired
}
