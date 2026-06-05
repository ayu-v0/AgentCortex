package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/model"
	clockpkg "github.com/ayu-v0/agent-cortex/pkg/clock"
)

const (
	defaultSessionTTL      = 10 * time.Minute
	defaultEventBufferSize = 32
)

type Config struct {
	SessionTTL        time.Duration
	EventBufferSize   int
	MaxActiveSessions int
	Clock             clockpkg.Clock
}

type Manager struct {
	mu                sync.Mutex
	client            model.Streamer
	clock             clockpkg.Clock
	sessionTTL        time.Duration
	eventBufferSize   int
	maxActiveSessions int
	closed            bool
	sessions          map[string]*sessionState
}

type StartRequest struct {
	SessionID    string
	ModelRequest model.Request
	Metadata     map[string]string
}

type Handle struct {
	SessionID string
	Events    <-chan Event
}

func NewManager(client model.Streamer, config Config) (*Manager, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: client is required", ErrInvalidConfig)
	}
	if config.MaxActiveSessions < 0 {
		return nil, fmt.Errorf("%w: max active sessions cannot be negative", ErrInvalidConfig)
	}
	sessionTTL := config.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = defaultSessionTTL
	}
	eventBufferSize := config.EventBufferSize
	if eventBufferSize <= 0 {
		eventBufferSize = defaultEventBufferSize
	}
	clk := config.Clock
	if clk == nil {
		clk = clockpkg.System{}
	}
	return &Manager{
		client:            client,
		clock:             clk,
		sessionTTL:        sessionTTL,
		eventBufferSize:   eventBufferSize,
		maxActiveSessions: config.MaxActiveSessions,
		sessions:          make(map[string]*sessionState),
	}, nil
}

func (m *Manager) Start(ctx context.Context, request StartRequest) (Handle, error) {
	id := strings.TrimSpace(request.SessionID)
	if id == "" {
		return Handle{}, ErrInvalidSessionID
	}
	if err := ctx.Err(); err != nil {
		return Handle{}, err
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	now := m.clock.Now()
	events := make(chan Event, m.eventBufferSize)
	state := &sessionState{
		id:        id,
		state:     StateRunning,
		startedAt: now,
		updatedAt: now,
		metadata:  copyMetadata(request.Metadata),
		cancel:    cancel,
		events:    events,
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		close(events)
		return Handle{}, ErrManagerClosed
	}
	if existing, ok := m.sessions[id]; ok && existing.state != StateExpired {
		m.mu.Unlock()
		cancel()
		close(events)
		return Handle{}, ErrSessionExists
	}
	if m.maxActiveSessions > 0 && m.activeSessionsLocked() >= m.maxActiveSessions {
		m.mu.Unlock()
		cancel()
		close(events)
		return Handle{}, ErrTooManySessions
	}
	m.sessions[id] = state
	m.mu.Unlock()

	go m.runSession(sessionCtx, state, request.ModelRequest)
	return Handle{SessionID: id, Events: events}, nil
}

func (m *Manager) Interrupt(sessionID string) error {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return ErrInvalidSessionID
	}

	m.mu.Lock()
	state, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return ErrSessionNotFound
	}
	if terminal(state.state) {
		m.mu.Unlock()
		return nil
	}
	cancel := state.cancel
	m.mu.Unlock()

	cancel()
	return nil
}

func (m *Manager) Status(sessionID string) (Status, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return Status{}, ErrInvalidSessionID
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.sessions[id]
	if !ok {
		return Status{}, ErrSessionNotFound
	}
	return statusFromState(state), nil
}

func (m *Manager) CleanupExpired(now time.Time) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	removed := 0
	for id, state := range m.sessions {
		if !terminal(state.state) || state.state == StateExpired {
			continue
		}
		if now.Sub(state.updatedAt) > m.sessionTTL {
			state.state = StateExpired
			delete(m.sessions, id)
			removed++
		}
	}
	return removed
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	var cancels []context.CancelFunc
	for _, state := range m.sessions {
		if !terminal(state.state) {
			cancels = append(cancels, state.cancel)
		}
	}
	m.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	return nil
}

func (m *Manager) runSession(ctx context.Context, state *sessionState, request model.Request) {
	defer close(state.events)

	if !m.send(ctx, state, Event{Type: EventStarted}) {
		m.finish(state, StateInterrupted, "")
		return
	}

	stream, err := m.client.Stream(ctx, request)
	if err != nil {
		if ctx.Err() != nil {
			m.sendTerminal(state, Event{Type: EventInterrupted})
			m.finish(state, StateInterrupted, "")
			return
		}
		errText := safeError(err)
		m.sendTerminal(state, Event{Type: EventFailed, Err: errText})
		m.finish(state, StateFailed, errText)
		return
	}

	for event := range stream {
		if event.Err != nil {
			errText := safeError(event.Err)
			m.sendTerminal(state, Event{Type: EventFailed, Err: errText})
			m.finish(state, StateFailed, errText)
			return
		}
		if event.TextDelta != "" {
			if !m.send(ctx, state, eventFromStream(state.id, EventTextDelta, event)) {
				m.finish(state, StateInterrupted, "")
				return
			}
		}
		if event.ToolCallDelta != nil {
			if !m.send(ctx, state, eventFromStream(state.id, EventToolDelta, event)) {
				m.finish(state, StateInterrupted, "")
				return
			}
		}
	}

	if ctx.Err() != nil {
		m.sendTerminal(state, Event{Type: EventInterrupted})
		m.finish(state, StateInterrupted, "")
		return
	}
	m.sendTerminal(state, Event{Type: EventFinished})
	m.finish(state, StateCompleted, "")
}

func (m *Manager) send(ctx context.Context, state *sessionState, event Event) bool {
	event.SessionID = state.id
	event.At = m.clock.Now()
	select {
	case state.events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func (m *Manager) sendTerminal(state *sessionState, event Event) {
	event.SessionID = state.id
	event.At = m.clock.Now()
	state.events <- event
}

func (m *Manager) finish(state *sessionState, final State, errText string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if terminal(state.state) {
		return
	}
	state.state = final
	state.updatedAt = m.clock.Now()
	state.errText = errText
}

func (m *Manager) activeSessionsLocked() int {
	active := 0
	for _, state := range m.sessions {
		if !terminal(state.state) {
			active++
		}
	}
	return active
}

func statusFromState(state *sessionState) Status {
	return Status{
		SessionID: state.id,
		State:     state.state,
		StartedAt: state.startedAt,
		UpdatedAt: state.updatedAt,
		Metadata:  copyMetadata(state.metadata),
		Err:       state.errText,
	}
}

func eventFromStream(sessionID string, eventType EventType, streamEvent model.StreamEvent) Event {
	return Event{
		SessionID:     sessionID,
		Type:          eventType,
		TextDelta:     streamEvent.TextDelta,
		ToolCallDelta: streamEvent.ToolCallDelta,
		FinishReason:  streamEvent.FinishReason,
		Model:         streamEvent.Model,
		Usage:         streamEvent.Usage,
		RawID:         streamEvent.RawID,
	}
}

func copyMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	copied := make(map[string]string, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}
	return copied
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
