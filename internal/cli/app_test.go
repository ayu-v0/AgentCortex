package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ayu-v0/agent-cortex/internal/cli/client"
	"github.com/ayu-v0/agent-cortex/internal/config"
	"github.com/ayu-v0/agent-cortex/internal/model"
)

type modelMessage = model.Message

type fakeTerminalIO struct {
	output strings.Builder
}

func (f *fakeTerminalIO) ReadLine(context.Context) (string, error) {
	return "", io.EOF
}

func (f *fakeTerminalIO) Write(text string) error {
	_, err := f.output.WriteString(text)
	return err
}

func (f *fakeTerminalIO) WriteLine(text string) error {
	return f.Write(text + "\n")
}

type fakeQAClient struct {
	events []client.QAEvent
	err    error
}

func (f *fakeQAClient) StreamQA(ctx context.Context, req client.QARequest) (<-chan client.QAEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan client.QAEvent, len(f.events))
	for _, event := range f.events {
		select {
		case <-ctx.Done():
			close(ch)
			return ch, nil
		default:
			ch <- event
		}
	}
	close(ch)
	return ch, nil
}

func TestHandleLineSuccessfulTurnAddsConversationAfterFinished(t *testing.T) {
	io := &fakeTerminalIO{}
	app, err := NewApp(config.Config{
		AgentID:        "agent-1",
		UserID:         "user-1",
		ServerEndpoint: "http://127.0.0.1:8080",
		SystemPrompt:   "be concise",
	}, Dependencies{
		QAClient: &fakeQAClient{events: []client.QAEvent{
			{Type: client.EventTextDelta, TextDelta: "hello"},
			{Type: client.EventTextDelta, TextDelta: " world"},
			{Type: client.EventFinished, Answer: "hello world"},
		}},
	}, io)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	shouldExit, err := app.handleLine(context.Background(), "question")
	if err != nil {
		t.Fatalf("handle line: %v", err)
	}
	if shouldExit {
		t.Fatal("expected to continue")
	}
	if got := io.output.String(); !strings.Contains(got, "hello world") {
		t.Fatalf("expected streamed answer, got %q", got)
	}
	if len(app.conversation.Messages()) != 3 {
		t.Fatalf("expected system + turn messages, got %d", len(app.conversation.Messages()))
	}
}

func TestHandleLineServerFailureDoesNotMutateConversation(t *testing.T) {
	io := &fakeTerminalIO{}
	app, err := NewApp(config.Config{
		AgentID:        "agent-1",
		UserID:         "user-1",
		ServerEndpoint: "http://127.0.0.1:8080",
	}, Dependencies{
		QAClient: &fakeQAClient{err: errors.New("boom")},
	}, io)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	shouldExit, err := app.handleLine(context.Background(), "question")
	if err != nil {
		t.Fatalf("handle line: %v", err)
	}
	if shouldExit {
		t.Fatal("expected to continue")
	}
	if got := io.output.String(); !strings.Contains(got, "[error] boom") {
		t.Fatalf("expected error output, got %q", got)
	}
	if len(app.conversation.Messages()) != 0 {
		t.Fatalf("expected no conversation mutation, got %d messages", len(app.conversation.Messages()))
	}
}

func TestHandleLineWarningKeepsConversationOnFinished(t *testing.T) {
	io := &fakeTerminalIO{}
	app, err := NewApp(config.Config{
		AgentID:        "agent-1",
		UserID:         "user-1",
		ServerEndpoint: "http://127.0.0.1:8080",
	}, Dependencies{
		QAClient: &fakeQAClient{events: []client.QAEvent{
			{Type: client.EventTextDelta, TextDelta: "answer"},
			{Type: client.EventWarning, Warning: "memory markdown sync failed"},
			{Type: client.EventFinished, Answer: "answer"},
		}},
	}, io)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	shouldExit, err := app.handleLine(context.Background(), "question")
	if err != nil {
		t.Fatalf("handle line: %v", err)
	}
	if shouldExit {
		t.Fatal("expected to continue")
	}
	if got := io.output.String(); !strings.Contains(got, "[warning] memory markdown sync failed") {
		t.Fatalf("expected warning output, got %q", got)
	}
	if len(app.conversation.Messages()) != 2 {
		t.Fatalf("expected successful turn kept, got %d messages", len(app.conversation.Messages()))
	}
}

func TestHandleLineWithoutFinishedDoesNotMutateConversation(t *testing.T) {
	io := &fakeTerminalIO{}
	app, err := NewApp(config.Config{
		AgentID:        "agent-1",
		UserID:         "user-1",
		ServerEndpoint: "http://127.0.0.1:8080",
	}, Dependencies{
		QAClient: &fakeQAClient{events: []client.QAEvent{
			{Type: client.EventTextDelta, TextDelta: "partial"},
		}},
	}, io)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	shouldExit, err := app.handleLine(context.Background(), "question")
	if err != nil {
		t.Fatalf("handle line: %v", err)
	}
	if shouldExit {
		t.Fatal("expected to continue")
	}
	if got := io.output.String(); !strings.Contains(got, "qa stream ended without finished event") {
		t.Fatalf("expected finished error, got %q", got)
	}
	if len(app.conversation.Messages()) != 0 {
		t.Fatalf("expected no conversation mutation, got %d messages", len(app.conversation.Messages()))
	}
}

func TestHandleLineClearResetsConversation(t *testing.T) {
	io := &fakeTerminalIO{}
	app, err := NewApp(config.Config{
		AgentID:        "agent-1",
		UserID:         "user-1",
		ServerEndpoint: "http://127.0.0.1:8080",
		SystemPrompt:   "be concise",
	}, Dependencies{
		QAClient: &fakeQAClient{events: []client.QAEvent{{Type: client.EventFinished, Answer: "answer"}}},
	}, io)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	if _, err := app.handleLine(context.Background(), "question"); err != nil {
		t.Fatalf("handle question: %v", err)
	}
	if len(app.conversation.Messages()) != 3 {
		t.Fatalf("expected system + turn before clear, got %d messages", len(app.conversation.Messages()))
	}

	shouldExit, err := app.handleLine(context.Background(), "clear")
	if err != nil {
		t.Fatalf("handle clear: %v", err)
	}
	if shouldExit {
		t.Fatal("expected to continue")
	}
	if len(app.conversation.Messages()) != 1 {
		t.Fatalf("expected only system prompt after clear, got %d messages", len(app.conversation.Messages()))
	}
}
