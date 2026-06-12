package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPQAClientParsesSSEEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/qa/stream" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: text_delta\n")
		fmt.Fprint(w, "data: {\"text\":\"hello\"}\n\n")
		fmt.Fprint(w, "event: warning\n")
		fmt.Fprint(w, "data: {\"message\":\"memory markdown sync failed\"}\n\n")
		fmt.Fprint(w, "event: finished\n")
		fmt.Fprint(w, "data: {\"answer\":\"hello\",\"memory_id\":\"memory-1\",\"memory_saved\":true,\"markdown_synced\":false,\"finish_reason\":\"stop\"}\n\n")
	}))
	defer server.Close()

	client, err := NewHTTPQAClient(server.URL, server.Client(), "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	events, err := client.StreamQA(context.Background(), QARequest{
		AgentID:   "agent-1",
		UserID:    "user-1",
		SessionID: "session-1",
		Messages: []Message{
			{Role: "user", Content: "question"},
		},
	})
	if err != nil {
		t.Fatalf("stream qa: %v", err)
	}

	got := collectEvents(events)
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if got[0].Type != EventTextDelta || got[0].TextDelta != "hello" {
		t.Fatalf("unexpected first event: %#v", got[0])
	}
	if got[1].Type != EventWarning || got[1].Warning != "memory markdown sync failed" {
		t.Fatalf("unexpected second event: %#v", got[1])
	}
	if got[2].Type != EventFinished || got[2].Answer != "hello" || got[2].MemoryID != "memory-1" || !got[2].MemorySaved || got[2].MarkdownSynced || got[2].FinishReason != "stop" {
		t.Fatalf("unexpected finished event: %#v", got[2])
	}
}

func TestHTTPQAClientIgnoresHeartbeat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: heartbeat\n")
		fmt.Fprint(w, "data: {\"ts\":\"2026-06-07T21:00:00+08:00\"}\n\n")
		fmt.Fprint(w, "event: finished\n")
		fmt.Fprint(w, "data: {\"answer\":\"done\",\"memory_saved\":false,\"markdown_synced\":false}\n\n")
	}))
	defer server.Close()

	client, err := NewHTTPQAClient(server.URL, server.Client(), "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	events, err := client.StreamQA(context.Background(), QARequest{
		AgentID:   "agent-1",
		UserID:    "user-1",
		SessionID: "session-1",
		Messages: []Message{
			{Role: "user", Content: "question"},
		},
	})
	if err != nil {
		t.Fatalf("stream qa: %v", err)
	}

	got := collectEvents(events)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Type != EventFinished || got[0].Answer != "done" {
		t.Fatalf("unexpected event: %#v", got[0])
	}
}

func TestHTTPQAClientReturnsStatusErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	client, err := NewHTTPQAClient(server.URL, server.Client(), "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.StreamQA(context.Background(), QARequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestHTTPQAClientReturnsDecodeErrorsForInvalidEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: finished\n")
		fmt.Fprint(w, "data: {invalid-json}\n\n")
	}))
	defer server.Close()

	client, err := NewHTTPQAClient(server.URL, server.Client(), "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	events, err := client.StreamQA(context.Background(), QARequest{
		AgentID:   "agent-1",
		UserID:    "user-1",
		SessionID: "session-1",
		Messages: []Message{
			{Role: "user", Content: "question"},
		},
	})
	if err != nil {
		t.Fatalf("stream qa: %v", err)
	}

	got := collectEvents(events)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Err == nil {
		t.Fatalf("expected decode error event, got %#v", got[0])
	}
}

func TestHTTPQAClientReturnsUnknownEventErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: mystery\n")
		fmt.Fprint(w, "data: {\"value\":1}\n\n")
	}))
	defer server.Close()

	client, err := NewHTTPQAClient(server.URL, server.Client(), "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	events, err := client.StreamQA(context.Background(), QARequest{
		AgentID:   "agent-1",
		UserID:    "user-1",
		SessionID: "session-1",
		Messages: []Message{
			{Role: "user", Content: "question"},
		},
	})
	if err != nil {
		t.Fatalf("stream qa: %v", err)
	}

	got := collectEvents(events)
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Err == nil || !strings.Contains(got[0].Err.Error(), "unknown event type") {
		t.Fatalf("expected unknown event error, got %#v", got[0])
	}
}

func TestHTTPQAClientStopsOnContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: text_delta\n")
		fmt.Fprint(w, "data: {\"text\":\"hello\"}\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(w, "event: finished\n")
		fmt.Fprint(w, "data: {\"answer\":\"hello\",\"memory_saved\":false,\"markdown_synced\":false}\n\n")
	}))
	defer server.Close()

	client, err := NewHTTPQAClient(server.URL, server.Client(), "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	events, err := client.StreamQA(ctx, QARequest{
		AgentID:   "agent-1",
		UserID:    "user-1",
		SessionID: "session-1",
		Messages: []Message{
			{Role: "user", Content: "question"},
		},
	})
	if err != nil {
		t.Fatalf("stream qa: %v", err)
	}

	first := <-events
	if first.Type != EventTextDelta || first.TextDelta != "hello" {
		t.Fatalf("unexpected first event: %#v", first)
	}
	cancel()

	got := collectEvents(events)
	if len(got) != 0 {
		t.Fatalf("expected no more events after cancel, got %#v", got)
	}
}

func TestHTTPQAClientParsesMultiLineDataEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: warning\n")
		fmt.Fprint(w, "data: {\"message\":\"line1\",\n")
		fmt.Fprint(w, "data: \"extra\":\"line2\"}\n\n")
		fmt.Fprint(w, "event: finished\n")
		fmt.Fprint(w, "data: {\"answer\":\"done\",\"memory_saved\":false,\"markdown_synced\":false}\n\n")
	}))
	defer server.Close()

	client, err := NewHTTPQAClient(server.URL, server.Client(), "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	events, err := client.StreamQA(context.Background(), QARequest{
		AgentID:   "agent-1",
		UserID:    "user-1",
		SessionID: "session-1",
		Messages: []Message{
			{Role: "user", Content: "question"},
		},
	})
	if err != nil {
		t.Fatalf("stream qa: %v", err)
	}

	got := collectEvents(events)
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[0].Type != EventWarning || got[0].Warning != "line1" {
		t.Fatalf("unexpected warning event: %#v", got[0])
	}
	if got[1].Type != EventFinished {
		t.Fatalf("unexpected finished event: %#v", got[1])
	}
}

func collectEvents(events <-chan QAEvent) []QAEvent {
	var collected []QAEvent
	for event := range events {
		collected = append(collected, event)
	}
	return collected
}
