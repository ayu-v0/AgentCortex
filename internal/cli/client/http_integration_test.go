package client_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	clientpkg "github.com/ayu-v0/agent-cortex/internal/cli/client"
	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/model"
	transporthttp "github.com/ayu-v0/agent-cortex/internal/transport/http"
)

type integrationEmbedder struct {
	err error
}

func (e *integrationEmbedder) Embed(context.Context, embedding.Input) (embedding.Vector, error) {
	if e.err != nil {
		return nil, e.err
	}
	return embedding.Vector{0, 1, 2, 3}, nil
}

type integrationStreamer struct {
	events []model.StreamEvent
	err    error
}

func (s *integrationStreamer) Stream(context.Context, model.Request) (<-chan model.StreamEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	ch := make(chan model.StreamEvent, len(s.events))
	for _, event := range s.events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

type integrationBackend struct {
	saved memory.Memory
}

func (b *integrationBackend) Close() error {
	return nil
}

func (b *integrationBackend) Save(item memory.Memory) error {
	b.saved = item
	return nil
}

func (b *integrationBackend) Search(string, string, []float32, int) ([]memory.SearchResult, error) {
	return nil, nil
}

func TestHTTPQAClientWorksAgainstRealAgentCortexServer(t *testing.T) {
	backend := &integrationBackend{}
	service, err := memory.NewService(backend)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	server := transporthttp.NewServer(service, &integrationEmbedder{}, &integrationStreamer{
		events: []model.StreamEvent{
			{TextDelta: "hello"},
			{TextDelta: " world", FinishReason: "stop"},
		},
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	client, err := clientpkg.NewHTTPQAClient(httpServer.URL, httpServer.Client(), "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	events, err := client.StreamQA(context.Background(), clientpkg.QARequest{
		AgentID: "agent-1",
		UserID:  "user-1",
		Messages: []clientpkg.Message{
			{Role: "user", Content: "question"},
		},
	})
	if err != nil {
		t.Fatalf("stream qa: %v", err)
	}

	got := collectIntegrationEvents(events)
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if got[0].Type != clientpkg.EventTextDelta || got[0].TextDelta != "hello" {
		t.Fatalf("unexpected first event: %#v", got[0])
	}
	if got[1].Type != clientpkg.EventTextDelta || got[1].TextDelta != " world" {
		t.Fatalf("unexpected second event: %#v", got[1])
	}
	if got[2].Type != clientpkg.EventFinished || got[2].Answer != "hello world" || !got[2].MemorySaved || !got[2].MarkdownSynced {
		t.Fatalf("unexpected finished event: %#v", got[2])
	}
	if backend.saved.Question != "question" || backend.saved.Answer != "hello world" {
		t.Fatalf("expected backend save, got %#v", backend.saved)
	}
}

func TestHTTPQAClientReceivesWarningFromRealAgentCortexServer(t *testing.T) {
	backend := &integrationBackend{}
	service, err := memory.NewService(backend)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	server := transporthttp.NewServer(service, &integrationEmbedder{err: errors.New("embed failed")}, &integrationStreamer{
		events: []model.StreamEvent{
			{TextDelta: "answer", FinishReason: "stop"},
		},
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	client, err := clientpkg.NewHTTPQAClient(httpServer.URL, httpServer.Client(), "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	events, err := client.StreamQA(context.Background(), clientpkg.QARequest{
		AgentID: "agent-1",
		UserID:  "user-1",
		Messages: []clientpkg.Message{
			{Role: "user", Content: "question"},
		},
	})
	if err != nil {
		t.Fatalf("stream qa: %v", err)
	}

	got := collectIntegrationEvents(events)
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if got[1].Type != clientpkg.EventWarning || !strings.Contains(got[1].Warning, "embedding failed") {
		t.Fatalf("unexpected warning event: %#v", got[1])
	}
	if got[2].Type != clientpkg.EventFinished || got[2].MemorySaved {
		t.Fatalf("unexpected finished event: %#v", got[2])
	}
	if backend.saved.ID != "" {
		t.Fatalf("expected no persisted memory, got %#v", backend.saved)
	}
}

func TestHTTPQAClientIgnoresHeartbeatFromRealAgentCortexServer(t *testing.T) {
	originalInterval := transporthttpHeartbeatIntervalForTest(t, 10*time.Millisecond)

	backend := &integrationBackend{}
	service, err := memory.NewService(backend)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	release := make(chan struct{})
	server := transporthttp.NewServer(service, &integrationEmbedder{}, &integrationBlockingStreamer{
		events: []model.StreamEvent{
			{TextDelta: "answer", FinishReason: "stop"},
		},
		release: release,
	})
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	client, err := clientpkg.NewHTTPQAClient(httpServer.URL, httpServer.Client(), "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	done := make(chan []clientpkg.QAEvent, 1)
	go func() {
		events, err := client.StreamQA(context.Background(), clientpkg.QARequest{
			AgentID: "agent-1",
			UserID:  "user-1",
			Messages: []clientpkg.Message{
				{Role: "user", Content: "question"},
			},
		})
		if err != nil {
			done <- []clientpkg.QAEvent{{Err: err}}
			return
		}
		done <- collectIntegrationEvents(events)
	}()

	time.Sleep(35 * time.Millisecond)
	close(release)
	got := <-done

	restoreTransportHTTPHeartbeatInterval(originalInterval)

	if len(got) != 2 {
		t.Fatalf("expected 2 client-visible events, got %d: %#v", len(got), got)
	}
	if got[0].Type != clientpkg.EventTextDelta || got[0].TextDelta != "answer" {
		t.Fatalf("unexpected first event: %#v", got[0])
	}
	if got[1].Type != clientpkg.EventFinished || got[1].Answer != "answer" {
		t.Fatalf("unexpected finished event: %#v", got[1])
	}
}

type integrationBlockingStreamer struct {
	events  []model.StreamEvent
	release <-chan struct{}
}

func (s *integrationBlockingStreamer) Stream(context.Context, model.Request) (<-chan model.StreamEvent, error) {
	ch := make(chan model.StreamEvent, len(s.events))
	go func() {
		defer close(ch)
		<-s.release
		for _, event := range s.events {
			ch <- event
		}
	}()
	return ch, nil
}

func transporthttpHeartbeatIntervalForTest(t *testing.T, interval time.Duration) time.Duration {
	t.Helper()
	return transporthttp.SetHeartbeatIntervalForTest(interval)
}

func restoreTransportHTTPHeartbeatInterval(previous time.Duration) {
	transporthttp.SetHeartbeatIntervalForTest(previous)
}

func collectIntegrationEvents(events <-chan clientpkg.QAEvent) []clientpkg.QAEvent {
	var collected []clientpkg.QAEvent
	for event := range events {
		collected = append(collected, event)
	}
	return collected
}
