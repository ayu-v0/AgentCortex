package model

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewOpenAICompatibleClientRejectsEmptyEndpoint(t *testing.T) {
	_, err := NewOpenAICompatibleClient(Config{Model: "test-model"})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestNewOpenAICompatibleClientRejectsEmptyModel(t *testing.T) {
	_, err := NewOpenAICompatibleClient(Config{Endpoint: "http://127.0.0.1:8082"})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestOpenAICompatibleGenerateRejectsEmptyMessages(t *testing.T) {
	client := newTestOpenAICompatibleClient(t, "http://127.0.0.1:8082")
	_, err := client.Generate(context.Background(), Request{})
	if !errors.Is(err, ErrEmptyMessages) {
		t.Fatalf("expected ErrEmptyMessages, got %v", err)
	}
}

func TestOpenAICompatibleGenerateRejectsEmptyContent(t *testing.T) {
	client := newTestOpenAICompatibleClient(t, "http://127.0.0.1:8082")
	_, err := client.Generate(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: " "}}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestOpenAICompatibleGenerateRejectsUnknownRole(t *testing.T) {
	client := newTestOpenAICompatibleClient(t, "http://127.0.0.1:8082")
	_, err := client.Generate(context.Background(), Request{Messages: []Message{{Role: Role("developer"), Content: "hello"}}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestOpenAICompatibleGenerateRejectsToolMessageWithoutToolCallID(t *testing.T) {
	client := newTestOpenAICompatibleClient(t, "http://127.0.0.1:8082")
	_, err := client.Generate(context.Background(), Request{Messages: []Message{{Role: RoleTool, Content: "result"}}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestOpenAICompatibleGenerateRejectsInvalidToolSchema(t *testing.T) {
	client := newTestOpenAICompatibleClient(t, "http://127.0.0.1:8082")
	_, err := client.Generate(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
		Tools: []Tool{{
			Name:        "search_memory",
			Description: "Search stored memories.",
			Parameters:  json.RawMessage(`[]`),
		}},
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestOpenAICompatibleGenerateRejectsNamedToolChoiceWithoutName(t *testing.T) {
	client := newTestOpenAICompatibleClient(t, "http://127.0.0.1:8082")
	_, err := client.Generate(context.Background(), Request{
		Messages:   []Message{{Role: RoleUser, Content: "hello"}},
		ToolChoice: &ToolChoice{Mode: ToolChoiceTool},
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestOpenAICompatibleGenerateSendsChatCompletionRequest(t *testing.T) {
	var gotAuth string
	var gotRequest openAICompatibleChatRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("expected /v1/chat/completions, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Fatalf("expected application/json content type, got %q", contentType)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","model":"test-model","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"hello back"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	defer server.Close()

	temperature := float32(0.7)
	maxTokens := 128
	client := newTestOpenAICompatibleClientWithConfig(t, Config{
		Endpoint: server.URL + "/",
		Model:    "test-model",
		APIKey:   "secret",
	})

	response, err := client.Generate(context.Background(), Request{
		Messages: []Message{
			{Role: RoleSystem, Content: "be concise"},
			{Role: RoleUser, Content: "hello"},
		},
		Tools: []Tool{{
			Name:        "search_memory",
			Description: "Search stored memories.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
		}},
		ToolChoice:  &ToolChoice{Mode: ToolChoiceAuto},
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("expected bearer auth, got %q", gotAuth)
	}
	if gotRequest.Model != "test-model" {
		t.Fatalf("expected model test-model, got %q", gotRequest.Model)
	}
	if len(gotRequest.Messages) != 2 || gotRequest.Messages[1].Content != "hello" {
		t.Fatalf("unexpected messages: %+v", gotRequest.Messages)
	}
	if len(gotRequest.Tools) != 1 || gotRequest.Tools[0].Function.Name != "search_memory" {
		t.Fatalf("unexpected tools: %+v", gotRequest.Tools)
	}
	if gotRequest.ToolChoice != "auto" {
		t.Fatalf("expected auto tool choice, got %#v", gotRequest.ToolChoice)
	}
	if gotRequest.Temperature == nil || *gotRequest.Temperature != temperature {
		t.Fatalf("expected temperature %v, got %v", temperature, gotRequest.Temperature)
	}
	if gotRequest.MaxTokens == nil || *gotRequest.MaxTokens != maxTokens {
		t.Fatalf("expected max tokens %d, got %v", maxTokens, gotRequest.MaxTokens)
	}
	if response.Text != "hello back" {
		t.Fatalf("expected response text, got %q", response.Text)
	}
	if response.FinishReason != "stop" {
		t.Fatalf("expected finish reason stop, got %q", response.FinishReason)
	}
	if response.Model != "test-model" || response.RawID != "chatcmpl-test" {
		t.Fatalf("unexpected response metadata: %+v", response)
	}
	if response.Usage.TotalTokens != 5 {
		t.Fatalf("expected usage total 5, got %+v", response.Usage)
	}
}

func TestOpenAICompatibleGenerateParsesToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","model":"test-model","choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_001","type":"function","function":{"name":"search_memory","arguments":"{\"query\":\"hello\"}"}}]}}]}`))
	}))
	defer server.Close()

	client := newTestOpenAICompatibleClient(t, server.URL)
	response, err := client.Generate(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if response.Text != "" {
		t.Fatalf("expected empty text for tool call response, got %q", response.Text)
	}
	if response.FinishReason != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %q", response.FinishReason)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %+v", response.ToolCalls)
	}
	if response.ToolCalls[0].ID != "call_001" || response.ToolCalls[0].Name != "search_memory" {
		t.Fatalf("unexpected tool call: %+v", response.ToolCalls[0])
	}
	if string(response.ToolCalls[0].Arguments) != `{"query":"hello"}` {
		t.Fatalf("unexpected tool arguments: %s", response.ToolCalls[0].Arguments)
	}
}

func TestOpenAICompatibleGenerateOmitsAuthorizationWithoutAPIKey(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client := newTestOpenAICompatibleClient(t, server.URL)
	_, err := client.Generate(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("expected empty authorization header, got %q", gotAuth)
	}
}

func TestOpenAICompatibleGenerateMapsProviderStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestOpenAICompatibleClient(t, server.URL)
	_, err := client.Generate(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestOpenAICompatibleGenerateMapsInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	client := newTestOpenAICompatibleClient(t, server.URL)
	_, err := client.Generate(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestOpenAICompatibleGenerateMapsEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()

	client := newTestOpenAICompatibleClient(t, server.URL)
	_, err := client.Generate(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestOpenAICompatibleGenerateMapsMalformedToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"tool_calls":[{"id":"","type":"function","function":{"name":"search_memory","arguments":"{}"}}]}}]}`))
	}))
	defer server.Close()

	client := newTestOpenAICompatibleClient(t, server.URL)
	_, err := client.Generate(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestOpenAICompatibleGenerateReturnsCanceledContext(t *testing.T) {
	client := newTestOpenAICompatibleClient(t, "http://127.0.0.1:8082")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Generate(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestOpenAICompatibleStreamSendsStreamRequestAndParsesTextDeltas(t *testing.T) {
	var gotRequest openAICompatibleChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("expected /v1/chat/completions, got %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-stream\",\"model\":\"test-model\",\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-stream\",\"model\":\"test-model\",\"choices\":[{\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := newTestOpenAICompatibleClient(t, server.URL)
	events, err := client.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	collected := collectStreamEvents(t, events)

	if !gotRequest.Stream {
		t.Fatalf("expected stream request flag")
	}
	if len(collected) != 2 {
		t.Fatalf("expected two events, got %+v", collected)
	}
	if collected[0].TextDelta != "hel" || collected[1].TextDelta != "lo" {
		t.Fatalf("unexpected text deltas: %+v", collected)
	}
	if collected[1].FinishReason != "stop" {
		t.Fatalf("expected finish reason stop, got %q", collected[1].FinishReason)
	}
	if collected[1].RawID != "chatcmpl-stream" || collected[1].Model != "test-model" {
		t.Fatalf("unexpected metadata: %+v", collected[1])
	}
	if collected[1].Usage.TotalTokens != 3 {
		t.Fatalf("expected usage total 3, got %+v", collected[1].Usage)
	}
}

func TestOpenAICompatibleStreamParsesToolCallDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_001\",\"type\":\"function\",\"function\":{\"name\":\"search_memory\",\"arguments\":\"{\\\"query\\\"\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\":\\\"hello\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := newTestOpenAICompatibleClient(t, server.URL)
	events, err := client.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	collected := collectStreamEvents(t, events)

	if len(collected) != 2 {
		t.Fatalf("expected two events, got %+v", collected)
	}
	if collected[0].ToolCallDelta == nil {
		t.Fatalf("expected first tool call delta")
	}
	if collected[0].ToolCallDelta.ID != "call_001" || collected[0].ToolCallDelta.Name != "search_memory" {
		t.Fatalf("unexpected first tool delta: %+v", collected[0].ToolCallDelta)
	}
	if collected[0].ToolCallDelta.ArgumentsDelta != `{"query"` {
		t.Fatalf("unexpected first arguments delta: %q", collected[0].ToolCallDelta.ArgumentsDelta)
	}
	if collected[1].ToolCallDelta == nil || collected[1].ToolCallDelta.ArgumentsDelta != `:"hello"}` {
		t.Fatalf("unexpected second tool delta: %+v", collected[1].ToolCallDelta)
	}
	if collected[1].FinishReason != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %q", collected[1].FinishReason)
	}
}

func TestOpenAICompatibleStreamMapsProviderStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusBadGateway)
	}))
	defer server.Close()

	client := newTestOpenAICompatibleClient(t, server.URL)
	_, err := client.Stream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}
}

func collectStreamEvents(t *testing.T, events <-chan StreamEvent) []StreamEvent {
	t.Helper()
	var collected []StreamEvent
	for event := range events {
		collected = append(collected, event)
	}
	return collected
}

func newTestOpenAICompatibleClient(t *testing.T, endpoint string) *OpenAICompatibleClient {
	t.Helper()
	return newTestOpenAICompatibleClientWithConfig(t, Config{
		Endpoint: endpoint,
		Model:    "test-model",
	})
}

func newTestOpenAICompatibleClientWithConfig(t *testing.T, config Config) *OpenAICompatibleClient {
	t.Helper()
	client, err := NewOpenAICompatibleClient(config)
	if err != nil {
		t.Fatalf("new openai compatible client: %v", err)
	}
	return client
}
