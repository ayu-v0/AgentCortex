package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/bootstrap"
	"github.com/ayu-v0/agent-cortex/internal/config"
	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/model"
)

type Dependencies struct {
	ModelClient   model.Client
	ModelStreamer model.Streamer
	Embedder      embedding.Embedder
	MemoryService *memory.Service
	Now           func() time.Time
}

type App struct {
	cfg           config.Config
	modelClient   model.Client
	modelStreamer model.Streamer
	embedder      embedding.Embedder
	memoryService *memory.Service
	io            TerminalIO
	conversation  *Conversation
	now           func() time.Time
	sequence      atomic.Uint64
}

func RunWithConfigPath(configPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	runtime, err := bootstrap.New(ctx, configPath, bootstrap.Options{RequireModel: true})
	if err != nil {
		return err
	}
	defer runtime.Close()

	app, err := NewApp(runtime.Config, Dependencies{
		ModelClient:   runtime.ModelClient,
		ModelStreamer: runtime.ModelStreamer,
		Embedder:      runtime.Embedder,
		MemoryService: runtime.MemoryService,
	}, newStdio(os.Stdin, os.Stdout))
	if err != nil {
		return err
	}

	return app.Run(ctx)
}

func NewApp(cfg config.Config, dependencies Dependencies, io TerminalIO) (*App, error) {
	if io == nil {
		return nil, errors.New("terminal io is required")
	}
	if strings.TrimSpace(cfg.AgentID) == "" {
		return nil, errors.New("agent id is required")
	}
	if strings.TrimSpace(cfg.UserID) == "" {
		return nil, errors.New("user id is required")
	}
	if dependencies.MemoryService == nil {
		return nil, errors.New("memory service is required")
	}
	if dependencies.Embedder == nil {
		return nil, errors.New("embedder is required")
	}
	if cfg.CLIStream && dependencies.ModelStreamer == nil {
		return nil, errors.New("model streamer is required")
	}
	if !cfg.CLIStream && dependencies.ModelClient == nil {
		return nil, errors.New("model client is required")
	}
	if dependencies.Now == nil {
		dependencies.Now = time.Now
	}

	return &App{
		cfg:           cfg,
		modelClient:   dependencies.ModelClient,
		modelStreamer: dependencies.ModelStreamer,
		embedder:      dependencies.Embedder,
		memoryService: dependencies.MemoryService,
		io:            io,
		conversation:  NewConversation(strings.TrimSpace(cfg.SystemPrompt)),
		now:           dependencies.Now,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		if err := a.io.Write("> "); err != nil {
			return err
		}
		line, err := a.io.ReadLine(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		shouldExit, err := a.handleLine(ctx, line)
		if err != nil {
			return err
		}
		if shouldExit {
			return nil
		}
	}
}

func (a *App) handleLine(ctx context.Context, line string) (bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false, nil
	}

	switch strings.ToLower(trimmed) {
	case "exit", "quit":
		return true, nil
	case "clear":
		a.conversation.Clear(strings.TrimSpace(a.cfg.SystemPrompt))
		return false, a.io.WriteLine("[context cleared]")
	}

	answer, err := a.answer(ctx, trimmed)
	if err != nil {
		return false, a.io.WriteLine(fmt.Sprintf("[error] %v", err))
	}
	if strings.TrimSpace(answer) == "" {
		return false, a.io.WriteLine("[error] empty model response")
	}

	a.conversation.AddTurn(trimmed, answer)

	if err := a.saveTurnMemory(ctx, trimmed, answer); err != nil {
		if writeErr := a.io.WriteLine(fmt.Sprintf("[warning] memory save failed: %v", err)); writeErr != nil {
			return false, writeErr
		}
	}

	return false, nil
}

func (a *App) answer(ctx context.Context, question string) (string, error) {
	if a.cfg.CLIStream {
		return a.answerStream(ctx, question)
	}
	return a.answerGenerate(ctx, question)
}

func (a *App) answerStream(ctx context.Context, question string) (string, error) {
	request := model.Request{Messages: a.conversation.MessagesWithUser(question)}
	events, err := a.modelStreamer.Stream(ctx, request)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	for event := range events {
		if event.Err != nil {
			return "", event.Err
		}
		if event.TextDelta == "" {
			continue
		}
		builder.WriteString(event.TextDelta)
		if err := a.io.Write(event.TextDelta); err != nil {
			return "", err
		}
	}
	if err := a.io.WriteLine(""); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func (a *App) answerGenerate(ctx context.Context, question string) (string, error) {
	request := model.Request{Messages: a.conversation.MessagesWithUser(question)}
	response, err := a.modelClient.Generate(ctx, request)
	if err != nil {
		return "", err
	}
	if err := a.io.WriteLine(response.Text); err != nil {
		return "", err
	}
	return response.Text, nil
}

func (a *App) saveTurnMemory(ctx context.Context, question, answer string) error {
	vector, err := a.embedder.Embed(ctx, embedding.Input{Text: question})
	if err != nil {
		return err
	}

	memoryID := a.nextMemoryID()
	return a.memoryService.Save(memory.Memory{
		ID:        memoryID,
		AgentID:   a.cfg.AgentID,
		UserID:    a.cfg.UserID,
		Question:  question,
		Answer:    answer,
		Content:   strings.TrimSpace(question + "\n" + answer),
		Embedding: []float32(vector),
	})
}

func (a *App) nextMemoryID() string {
	sequence := a.sequence.Add(1)
	return fmt.Sprintf("memory-%d-%d", a.now().UnixNano(), sequence)
}
