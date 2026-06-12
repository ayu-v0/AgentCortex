package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type QAEventType string

const (
	EventTextDelta QAEventType = "text_delta"
	EventWarning   QAEventType = "warning"
	EventFinished  QAEventType = "finished"
	EventError     QAEventType = "error"
	EventHeartbeat QAEventType = "heartbeat"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type QARequest struct {
	AgentID   string    `json:"agent_id"`
	UserID    string    `json:"user_id"`
	SessionID string    `json:"session_id"`
	Messages  []Message `json:"messages"`
}

type QAEvent struct {
	Type           QAEventType
	TextDelta      string
	Warning        string
	Answer         string
	MemoryID       string
	MemorySaved    bool
	MarkdownSynced bool
	FinishReason   string
	Err            error
}

type QAClient interface {
	StreamQA(ctx context.Context, req QARequest) (<-chan QAEvent, error)
}

type HTTPQAClient struct {
	baseURL       string
	httpClient    *http.Client
	authorization string
}

func NewHTTPQAClient(baseURL string, httpClient *http.Client, authorization string) (*HTTPQAClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, ErrServerEndpointRequired
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 0}
	}
	return &HTTPQAClient{
		baseURL:       baseURL,
		httpClient:    httpClient,
		authorization: strings.TrimSpace(authorization),
	}, nil
}

func (c *HTTPQAClient) StreamQA(ctx context.Context, reqBody QARequest) (<-chan QAEvent, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/qa/stream", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.authorization != "" {
		req.Header.Set("Authorization", c.authorization)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return nil, qaStreamStatusError{statusCode: resp.StatusCode, body: string(payload)}
	}

	events := make(chan QAEvent)
	go func() {
		defer close(events)
		defer resp.Body.Close()
		c.streamEvents(ctx, resp.Body, events)
	}()
	return events, nil
}

func (c *HTTPQAClient) streamEvents(ctx context.Context, body io.Reader, events chan<- QAEvent) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventName string
	var dataLines []string
	emit := func() bool {
		if strings.TrimSpace(eventName) == "" {
			eventName = ""
			dataLines = dataLines[:0]
			return true
		}
		event, err := decodeEvent(eventName, strings.Join(dataLines, "\n"))
		if err != nil {
			select {
			case events <- QAEvent{Err: err}:
			case <-ctx.Done():
			}
			return false
		}
		if event.Type != EventHeartbeat {
			select {
			case events <- event:
			case <-ctx.Done():
				return false
			}
		}
		eventName = ""
		dataLines = dataLines[:0]
		return true
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := scanner.Text()
		if line == "" {
			if !emit() {
				return
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return
		}
		select {
		case events <- QAEvent{Err: err}:
		case <-ctx.Done():
		}
		return
	}
	_ = emit()
}

func decodeEvent(name, data string) (QAEvent, error) {
	switch QAEventType(name) {
	case EventTextDelta:
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return QAEvent{}, err
		}
		return QAEvent{Type: EventTextDelta, TextDelta: payload.Text}, nil
	case EventWarning:
		var payload struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return QAEvent{}, err
		}
		return QAEvent{Type: EventWarning, Warning: payload.Message}, nil
	case EventFinished:
		var payload struct {
			Answer         string `json:"answer"`
			MemoryID       string `json:"memory_id"`
			MemorySaved    bool   `json:"memory_saved"`
			MarkdownSynced bool   `json:"markdown_synced"`
			FinishReason   string `json:"finish_reason"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return QAEvent{}, err
		}
		return QAEvent{
			Type:           EventFinished,
			Answer:         payload.Answer,
			MemoryID:       payload.MemoryID,
			MemorySaved:    payload.MemorySaved,
			MarkdownSynced: payload.MarkdownSynced,
			FinishReason:   payload.FinishReason,
		}, nil
	case EventError:
		var payload struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return QAEvent{}, err
		}
		return QAEvent{Type: EventError, Err: qaStreamEventError{code: payload.Code, message: payload.Message}}, nil
	case EventHeartbeat:
		return QAEvent{Type: EventHeartbeat}, nil
	default:
		return QAEvent{}, unknownEventTypeError(name)
	}
}
