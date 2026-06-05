package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/model"
)

type fakeStreamer struct {
	stream func(ctx context.Context, request model.Request) (<-chan model.StreamEvent, error)
}

func (s fakeStreamer) Stream(ctx context.Context, request model.Request) (<-chan model.StreamEvent, error) {
	return s.stream(ctx, request)
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

func TestNewManagerRejectsInvalidConfig(t *testing.T) {
	if _, err := NewManager(nil, Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for nil client, got %v", err)
	}

	client := newStaticFakeStreamer(nil)
	if _, err := NewManager(client, Config{MaxActiveSessions: -1}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for negative max sessions, got %v", err)
	}
}

func TestManagerStartForwardsEventsAndCompletes(t *testing.T) {
	stream := make(chan model.StreamEvent, 2)
	stream <- model.StreamEvent{TextDelta: "hel"}
	stream <- model.StreamEvent{TextDelta: "lo", FinishReason: "stop", Model: "test-model", RawID: "raw-1", Usage: model.Usage{TotalTokens: 3}}
	close(stream)

	clock := &fakeClock{now: time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)}
	manager, err := NewManager(fakeStreamer{stream: func(ctx context.Context, request model.Request) (<-chan model.StreamEvent, error) {
		return stream, nil
	}}, Config{Clock: clock})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	metadata := map[string]string{"user_id": "user-1"}
	handle, err := manager.Start(context.Background(), StartRequest{
		SessionID:    "session-1",
		ModelRequest: model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}},
		Metadata:     metadata,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	metadata["user_id"] = "mutated"

	events := collectEvents(handle.Events)
	assertEventTypes(t, events, EventStarted, EventTextDelta, EventTextDelta, EventFinished)
	if events[1].TextDelta != "hel" || events[2].TextDelta != "lo" {
		t.Fatalf("unexpected text events: %+v", events)
	}
	if events[2].FinishReason != "stop" || events[2].Model != "test-model" || events[2].RawID != "raw-1" {
		t.Fatalf("expected stream metadata on text event, got %+v", events[2])
	}
	if events[2].Usage.TotalTokens != 3 {
		t.Fatalf("expected usage on text event, got %+v", events[2].Usage)
	}

	status, err := manager.Status("session-1")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.State != StateCompleted {
		t.Fatalf("expected completed, got %s", status.State)
	}
	if status.Metadata["user_id"] != "user-1" {
		t.Fatalf("expected copied metadata, got %+v", status.Metadata)
	}
	status.Metadata["user_id"] = "changed"
	secondStatus, err := manager.Status("session-1")
	if err != nil {
		t.Fatalf("status again: %v", err)
	}
	if secondStatus.Metadata["user_id"] != "user-1" {
		t.Fatalf("expected status metadata copy, got %+v", secondStatus.Metadata)
	}
}

func TestManagerInterruptCancelsRunningSession(t *testing.T) {
	started := make(chan struct{})
	stream := make(chan model.StreamEvent)
	manager, err := NewManager(fakeStreamer{stream: func(ctx context.Context, request model.Request) (<-chan model.StreamEvent, error) {
		close(started)
		go func() {
			<-ctx.Done()
			close(stream)
		}()
		return stream, nil
	}}, Config{})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	handle, err := manager.Start(context.Background(), StartRequest{
		SessionID:    "session-1",
		ModelRequest: model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	<-started

	if err := manager.Interrupt("session-1"); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	events := collectEvents(handle.Events)
	assertEventTypes(t, events, EventStarted, EventInterrupted)

	status, err := manager.Status("session-1")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.State != StateInterrupted {
		t.Fatalf("expected interrupted, got %s", status.State)
	}
	if err := manager.Interrupt("session-1"); err != nil {
		t.Fatalf("second interrupt should be idempotent: %v", err)
	}
}

func TestManagerMapsStreamEventErrorToFailed(t *testing.T) {
	stream := make(chan model.StreamEvent, 1)
	stream <- model.StreamEvent{Err: errors.New("provider failed")}
	close(stream)

	manager, err := NewManager(newStaticFakeStreamer(stream), Config{})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	handle, err := manager.Start(context.Background(), StartRequest{
		SessionID:    "session-1",
		ModelRequest: model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	events := collectEvents(handle.Events)
	assertEventTypes(t, events, EventStarted, EventFailed)

	status, err := manager.Status("session-1")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.State != StateFailed || status.Err == "" {
		t.Fatalf("expected failed status with error, got %+v", status)
	}
}

func TestManagerMapsStreamSetupErrorToFailed(t *testing.T) {
	manager, err := NewManager(fakeStreamer{stream: func(ctx context.Context, request model.Request) (<-chan model.StreamEvent, error) {
		return nil, errors.New("provider unavailable")
	}}, Config{})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	handle, err := manager.Start(context.Background(), StartRequest{
		SessionID:    "session-1",
		ModelRequest: model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	events := collectEvents(handle.Events)
	assertEventTypes(t, events, EventStarted, EventFailed)
}

func TestManagerValidationAndLimits(t *testing.T) {
	manager, err := NewManager(newBlockingFakeStreamer(), Config{MaxActiveSessions: 1})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if _, err := manager.Start(context.Background(), StartRequest{SessionID: " "}); !errors.Is(err, ErrInvalidSessionID) {
		t.Fatalf("expected ErrInvalidSessionID, got %v", err)
	}
	if _, err := manager.Status("missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
	if err := manager.Interrupt(" "); !errors.Is(err, ErrInvalidSessionID) {
		t.Fatalf("expected ErrInvalidSessionID, got %v", err)
	}

	first, err := manager.Start(context.Background(), StartRequest{
		SessionID:    "session-1",
		ModelRequest: model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}},
	})
	if err != nil {
		t.Fatalf("start first: %v", err)
	}
	if _, err := manager.Start(context.Background(), StartRequest{
		SessionID:    "session-1",
		ModelRequest: model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}},
	}); !errors.Is(err, ErrSessionExists) {
		t.Fatalf("expected ErrSessionExists, got %v", err)
	}
	if _, err := manager.Start(context.Background(), StartRequest{
		SessionID:    "session-2",
		ModelRequest: model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}},
	}); !errors.Is(err, ErrTooManySessions) {
		t.Fatalf("expected ErrTooManySessions, got %v", err)
	}

	if err := manager.Interrupt("session-1"); err != nil {
		t.Fatalf("interrupt first: %v", err)
	}
	_ = collectEvents(first.Events)
}

func TestManagerCleanupExpiredRemovesOnlyTerminalSessions(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)}
	completedStream := make(chan model.StreamEvent)
	close(completedStream)
	manager, err := NewManager(fakeStreamer{stream: func(ctx context.Context, request model.Request) (<-chan model.StreamEvent, error) {
		if request.Messages[0].Content == "running" {
			stream := make(chan model.StreamEvent)
			go func() {
				<-ctx.Done()
				close(stream)
			}()
			return stream, nil
		}
		return completedStream, nil
	}}, Config{Clock: clock, SessionTTL: time.Minute})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	completed, err := manager.Start(context.Background(), StartRequest{
		SessionID:    "completed",
		ModelRequest: model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: "done"}}},
	})
	if err != nil {
		t.Fatalf("start completed: %v", err)
	}
	_ = collectEvents(completed.Events)

	running, err := manager.Start(context.Background(), StartRequest{
		SessionID:    "running",
		ModelRequest: model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: "running"}}},
	})
	if err != nil {
		t.Fatalf("start running: %v", err)
	}
	defer func() {
		_ = manager.Interrupt("running")
		_ = collectEvents(running.Events)
	}()

	clock.Set(clock.Now().Add(2 * time.Minute))
	if removed := manager.CleanupExpired(clock.Now()); removed != 1 {
		t.Fatalf("expected one removed session, got %d", removed)
	}
	if _, err := manager.Status("completed"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected completed session removed, got %v", err)
	}
	if status, err := manager.Status("running"); err != nil || status.State != StateRunning {
		t.Fatalf("expected running session to remain, status=%+v err=%v", status, err)
	}
}

func TestManagerCloseIsIdempotentAndCancelsActiveSessions(t *testing.T) {
	started := make(chan struct{})
	manager, err := NewManager(fakeStreamer{stream: func(ctx context.Context, request model.Request) (<-chan model.StreamEvent, error) {
		close(started)
		stream := make(chan model.StreamEvent)
		go func() {
			<-ctx.Done()
			close(stream)
		}()
		return stream, nil
	}}, Config{})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	handle, err := manager.Start(context.Background(), StartRequest{
		SessionID:    "session-1",
		ModelRequest: model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	<-started

	if err := manager.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	events := collectEvents(handle.Events)
	assertEventTypes(t, events, EventStarted, EventInterrupted)

	if _, err := manager.Start(context.Background(), StartRequest{SessionID: "session-2"}); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("expected ErrManagerClosed, got %v", err)
	}
}

func newStaticFakeStreamer(stream <-chan model.StreamEvent) fakeStreamer {
	return fakeStreamer{stream: func(ctx context.Context, request model.Request) (<-chan model.StreamEvent, error) {
		return stream, nil
	}}
}

func newBlockingFakeStreamer() fakeStreamer {
	return fakeStreamer{stream: func(ctx context.Context, request model.Request) (<-chan model.StreamEvent, error) {
		stream := make(chan model.StreamEvent)
		go func() {
			<-ctx.Done()
			close(stream)
		}()
		return stream, nil
	}}
}

func collectEvents(events <-chan Event) []Event {
	var collected []Event
	for event := range events {
		collected = append(collected, event)
	}
	return collected
}

func assertEventTypes(t *testing.T, events []Event, expected ...EventType) {
	t.Helper()
	if len(events) != len(expected) {
		t.Fatalf("expected %d events, got %d: %+v", len(expected), len(events), events)
	}
	for i := range expected {
		if events[i].Type != expected[i] {
			t.Fatalf("event %d: expected %s, got %s", i, expected[i], events[i].Type)
		}
	}
}
