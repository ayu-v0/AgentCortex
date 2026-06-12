package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/cli/client"
	"github.com/ayu-v0/agent-cortex/internal/config"
	"github.com/ayu-v0/agent-cortex/internal/model"
)

type Dependencies struct {
	QAClient client.QAClient
}

type App struct {
	cfg          config.Config
	qaClient     client.QAClient
	io           TerminalIO
	conversation *Conversation
	sessionID    string
}

var sessionIDSequence atomic.Uint64

func RunWithConfigPath(configPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	qaClient, err := client.NewHTTPQAClient(cfg.ServerEndpoint, &http.Client{}, "")
	if err != nil {
		return err
	}

	app, err := NewApp(cfg, Dependencies{QAClient: qaClient}, newStdio(os.Stdin, os.Stdout))
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
	if strings.TrimSpace(cfg.ServerEndpoint) == "" {
		return nil, errors.New("server endpoint is required")
	}
	if dependencies.QAClient == nil {
		return nil, errors.New("qa client is required")
	}

	return &App{
		cfg:          cfg,
		qaClient:     dependencies.QAClient,
		io:           io,
		conversation: NewConversation(strings.TrimSpace(cfg.SystemPrompt)),
		sessionID:    newSessionID(),
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
		a.sessionID = newSessionID()
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
	return false, nil
}

func (a *App) answer(ctx context.Context, question string) (string, error) {
	req := client.QARequest{
		AgentID:   a.cfg.AgentID,
		UserID:    a.cfg.UserID,
		SessionID: a.sessionID,
		Messages:  conversationToClientMessages(a.conversation.MessagesWithUser(question)),
	}
	events, err := a.qaClient.StreamQA(ctx, req)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	finished := false
	for event := range events {
		if event.Err != nil {
			return "", event.Err
		}
		switch event.Type {
		case client.EventTextDelta:
			builder.WriteString(event.TextDelta)
			if err := a.io.Write(event.TextDelta); err != nil {
				return "", err
			}
		case client.EventWarning:
			if err := a.io.WriteLine("\n[warning] " + event.Warning); err != nil {
				return "", err
			}
		case client.EventFinished:
			finished = true
			if err := a.io.WriteLine(""); err != nil {
				return "", err
			}
			if strings.TrimSpace(event.Answer) != "" {
				return event.Answer, nil
			}
			return builder.String(), nil
		}
	}
	if !finished {
		return "", errors.New("qa stream ended without finished event")
	}
	return builder.String(), nil
}

func newSessionID() string {
	return fmt.Sprintf("session-%d-%d", time.Now().UnixNano(), sessionIDSequence.Add(1))
}

func conversationToClientMessages(messages []model.Message) []client.Message {
	converted := make([]client.Message, 0, len(messages))
	for _, msg := range messages {
		converted = append(converted, client.Message{
			Role:    string(msg.Role),
			Content: msg.Content,
		})
	}
	return converted
}
