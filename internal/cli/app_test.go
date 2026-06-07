package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/config"
	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/model"
)

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

type fakeStreamer struct {
	events []model.StreamEvent
	err    error
}

func (f *fakeStreamer) Stream(ctx context.Context, request model.Request) (<-chan model.StreamEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	events := make(chan model.StreamEvent, len(f.events))
	for _, event := range f.events {
		select {
		case <-ctx.Done():
			close(events)
			return events, nil
		default:
			events <- event
		}
	}
	close(events)
	return events, nil
}

type fakeClient struct {
	response model.Response
	err      error
}

func (f *fakeClient) Generate(context.Context, model.Request) (model.Response, error) {
	if f.err != nil {
		return model.Response{}, f.err
	}
	return f.response, nil
}

type fakeEmbedder struct {
	vector embedding.Vector
	err    error
}

func (f *fakeEmbedder) Embed(context.Context, embedding.Input) (embedding.Vector, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.vector, nil
}

type fakeBackend struct {
	savedMemory memory.Memory
	saveErr     error
}

func (f *fakeBackend) Close() error {
	return nil
}

func (f *fakeBackend) Save(saved memory.Memory) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.savedMemory = saved
	return nil
}

func (f *fakeBackend) Search(string, string, []float32, int) ([]memory.SearchResult, error) {
	return nil, nil
}

func TestHandleLineSavesMemoryAfterSuccessfulAnswer(t *testing.T) {
	io := &fakeTerminalIO{}
	backend := &fakeBackend{}
	service, err := memory.NewService(backend)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	app, err := NewApp(config.Config{
		AgentID:      "agent-1",
		UserID:       "user-1",
		SystemPrompt: "be concise",
		CLIStream:    true,
	}, Dependencies{
		ModelStreamer: &fakeStreamer{events: []model.StreamEvent{
			{TextDelta: "hello"},
			{TextDelta: " world"},
		}},
		ModelClient:   &fakeClient{},
		Embedder:      &fakeEmbedder{vector: embedding.Vector{0, 1, 2, 3}},
		MemoryService: service,
		Now: func() time.Time {
			return time.Unix(100, 0)
		},
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
	if backend.savedMemory.Question != "question" {
		t.Fatalf("expected saved question, got %q", backend.savedMemory.Question)
	}
	if backend.savedMemory.Answer != "hello world" {
		t.Fatalf("expected saved answer, got %q", backend.savedMemory.Answer)
	}
	if backend.savedMemory.Content != "question\nhello world" {
		t.Fatalf("expected combined content, got %q", backend.savedMemory.Content)
	}
	if len(app.conversation.Messages()) != 3 {
		t.Fatalf("expected system + 2 turn messages, got %d", len(app.conversation.Messages()))
	}
}

func TestHandleLineModelFailureDoesNotSaveMemory(t *testing.T) {
	io := &fakeTerminalIO{}
	backend := &fakeBackend{}
	service, err := memory.NewService(backend)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	app, err := NewApp(config.Config{
		AgentID:   "agent-1",
		UserID:    "user-1",
		CLIStream: true,
	}, Dependencies{
		ModelStreamer: &fakeStreamer{err: errors.New("boom")},
		ModelClient:   &fakeClient{},
		Embedder:      &fakeEmbedder{vector: embedding.Vector{0, 1, 2, 3}},
		MemoryService: service,
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
	if backend.savedMemory.ID != "" {
		t.Fatalf("expected no saved memory, got %q", backend.savedMemory.ID)
	}
	if got := io.output.String(); !strings.Contains(got, "[error] boom") {
		t.Fatalf("expected error output, got %q", got)
	}
	if len(app.conversation.Messages()) != 0 {
		t.Fatalf("expected no conversation mutation, got %d messages", len(app.conversation.Messages()))
	}
}

func TestHandleLineMemoryFailureWarnsAndKeepsConversation(t *testing.T) {
	io := &fakeTerminalIO{}
	backend := &fakeBackend{saveErr: errors.New("save failed")}
	service, err := memory.NewService(backend)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	app, err := NewApp(config.Config{
		AgentID:   "agent-1",
		UserID:    "user-1",
		CLIStream: true,
	}, Dependencies{
		ModelStreamer: &fakeStreamer{events: []model.StreamEvent{{TextDelta: "answer"}}},
		ModelClient:   &fakeClient{},
		Embedder:      &fakeEmbedder{vector: embedding.Vector{0, 1, 2, 3}},
		MemoryService: service,
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
	if got := io.output.String(); !strings.Contains(got, "[warning] memory save failed: save failed") {
		t.Fatalf("expected warning output, got %q", got)
	}
	if len(app.conversation.Messages()) != 2 {
		t.Fatalf("expected successful conversation turn kept, got %d messages", len(app.conversation.Messages()))
	}
}

func TestHandleLineClearResetsConversation(t *testing.T) {
	io := &fakeTerminalIO{}
	backend := &fakeBackend{}
	service, err := memory.NewService(backend)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	app, err := NewApp(config.Config{
		AgentID:      "agent-1",
		UserID:       "user-1",
		SystemPrompt: "be concise",
		CLIStream:    true,
	}, Dependencies{
		ModelStreamer: &fakeStreamer{events: []model.StreamEvent{{TextDelta: "answer"}}},
		ModelClient:   &fakeClient{},
		Embedder:      &fakeEmbedder{vector: embedding.Vector{0, 1, 2, 3}},
		MemoryService: service,
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
