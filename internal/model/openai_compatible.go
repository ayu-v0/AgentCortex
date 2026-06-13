package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type OpenAICompatibleClient struct {
	endpoint string
	model    string
	apiKey   string
	client   *http.Client
}

var _ Client = (*OpenAICompatibleClient)(nil)
var _ Streamer = (*OpenAICompatibleClient)(nil)

func NewOpenAICompatibleClient(config Config) (*OpenAICompatibleClient, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	if endpoint == "" {
		return nil, ErrInvalidConfig
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		return nil, ErrInvalidConfig
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &OpenAICompatibleClient{
		endpoint: endpoint,
		model:    model,
		apiKey:   strings.TrimSpace(config.APIKey),
		client:   &http.Client{Timeout: timeout},
	}, nil
}

func (c *OpenAICompatibleClient) Generate(ctx context.Context, request Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}

	body, err := c.chatRequestBody(request, false)
	if err != nil {
		return Response{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, errors.Join(ErrProviderUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Response{}, ctxErr
		}
		return Response{}, errors.Join(ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Response{}, ErrProviderUnavailable
	}

	var decoded openAICompatibleChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Response{}, errors.Join(ErrInvalidResponse, err)
	}
	return c.responseFromChatResponse(decoded)
}

func (c *OpenAICompatibleClient) Stream(ctx context.Context, request Request) (<-chan StreamEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	body, err := c.chatRequestBody(request, true)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, errors.Join(ErrProviderUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, errors.Join(ErrProviderUnavailable, err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		resp.Body.Close()
		return nil, ErrProviderUnavailable
	}

	events := make(chan StreamEvent)
	go func() {
		defer close(events)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				return
			}
			event, err := c.streamEventFromData(data)
			if err != nil {
				events <- StreamEvent{Err: err}
				return
			}
			if streamEventEmpty(event) {
				continue
			}
			select {
			case events <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return events, nil
}

func (c *OpenAICompatibleClient) chatRequestBody(request Request, stream bool) ([]byte, error) {
	messages, err := openAICompatibleMessages(request.Messages)
	if err != nil {
		return nil, err
	}
	tools, err := openAICompatibleTools(request.Tools)
	if err != nil {
		return nil, err
	}
	toolChoice, err := openAICompatibleToolChoice(request.ToolChoice)
	if err != nil {
		return nil, err
	}
	if request.Temperature != nil && (*request.Temperature < 0 || *request.Temperature > 2) {
		return nil, ErrInvalidRequest
	}
	if request.MaxTokens != nil && *request.MaxTokens <= 0 {
		return nil, ErrInvalidRequest
	}

	body, err := json.Marshal(openAICompatibleChatRequest{
		Model:       c.model,
		Messages:    messages,
		Tools:       tools,
		ToolChoice:  toolChoice,
		Stream:      stream,
		Temperature: request.Temperature,
		MaxTokens:   request.MaxTokens,
	})
	if err != nil {
		return nil, errors.Join(ErrProviderUnavailable, err)
	}
	return body, nil
}

func (c *OpenAICompatibleClient) streamEventFromData(data string) (StreamEvent, error) {
	var decoded openAICompatibleChatStreamResponse
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		return StreamEvent{}, errors.Join(ErrInvalidResponse, err)
	}
	if len(decoded.Choices) == 0 {
		return StreamEvent{}, ErrInvalidResponse
	}
	choice := decoded.Choices[0]
	event := StreamEvent{
		TextDelta:    choice.Delta.Content,
		FinishReason: choice.FinishReason,
		Model:        decoded.Model,
		RawID:        decoded.ID,
		Usage: Usage{
			PromptTokens:     decoded.Usage.PromptTokens,
			CompletionTokens: decoded.Usage.CompletionTokens,
			TotalTokens:      decoded.Usage.TotalTokens,
		},
	}
	if strings.TrimSpace(event.Model) == "" {
		event.Model = c.model
	}
	if len(choice.Delta.ToolCalls) > 0 {
		toolCall := choice.Delta.ToolCalls[0]
		event.ToolCallDelta = &ToolCallDelta{
			Index:          toolCall.Index,
			ID:             strings.TrimSpace(toolCall.ID),
			Name:           strings.TrimSpace(toolCall.Function.Name),
			ArgumentsDelta: toolCall.Function.Arguments,
		}
	}
	return event, nil
}

func streamEventEmpty(event StreamEvent) bool {
	return event.TextDelta == "" &&
		event.ToolCallDelta == nil &&
		event.FinishReason == "" &&
		event.Usage == (Usage{}) &&
		event.Err == nil
}

func openAICompatibleMessages(messages []Message) ([]openAICompatibleChatMessage, error) {
	if len(messages) == 0 {
		return nil, ErrEmptyMessages
	}
	converted := make([]openAICompatibleChatMessage, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		convertedMessage := openAICompatibleChatMessage{
			Role:       string(message.Role),
			Content:    message.Content,
			ToolCallID: strings.TrimSpace(message.ToolCallID),
		}

		switch message.Role {
		case RoleSystem, RoleUser:
			if content == "" {
				return nil, ErrInvalidRequest
			}
		case RoleAssistant:
			toolCalls, err := openAICompatibleToolCalls(message.ToolCalls)
			if err != nil {
				return nil, err
			}
			if content == "" && len(toolCalls) == 0 {
				return nil, ErrInvalidRequest
			}
			convertedMessage.ToolCalls = toolCalls
		case RoleTool:
			if content == "" {
				return nil, ErrInvalidRequest
			}
			if convertedMessage.ToolCallID == "" {
				return nil, ErrInvalidRequest
			}
		default:
			return nil, ErrInvalidRequest
		}
		converted = append(converted, convertedMessage)
	}
	return converted, nil
}

func openAICompatibleTools(tools []Tool) ([]openAICompatibleTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	converted := make([]openAICompatibleTool, 0, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		description := strings.TrimSpace(tool.Description)
		if name == "" {
			return nil, ErrInvalidRequest
		}
		if description == "" {
			return nil, ErrInvalidRequest
		}
		if !isJSONObject(tool.Parameters) {
			return nil, ErrInvalidRequest
		}
		converted = append(converted, openAICompatibleTool{
			Type: "function",
			Function: openAICompatibleToolFunction{
				Name:        name,
				Description: description,
				Parameters:  tool.Parameters,
			},
		})
	}
	return converted, nil
}

func openAICompatibleToolChoice(choice *ToolChoice) (any, error) {
	if choice == nil {
		return nil, nil
	}
	switch choice.Mode {
	case ToolChoiceAuto:
		return "auto", nil
	case ToolChoiceNone:
		return "none", nil
	case ToolChoiceRequired:
		return "required", nil
	case ToolChoiceTool:
		name := strings.TrimSpace(choice.Name)
		if name == "" {
			return nil, ErrInvalidRequest
		}
		return openAICompatibleNamedToolChoice{
			Type: "function",
			Function: openAICompatibleNamedToolChoiceFunction{
				Name: name,
			},
		}, nil
	default:
		return nil, ErrInvalidRequest
	}
}

func openAICompatibleToolCalls(toolCalls []ToolCall) ([]openAICompatibleToolCall, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}
	converted := make([]openAICompatibleToolCall, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		id := strings.TrimSpace(toolCall.ID)
		name := strings.TrimSpace(toolCall.Name)
		if id == "" {
			return nil, ErrInvalidRequest
		}
		if name == "" {
			return nil, ErrInvalidRequest
		}
		if !json.Valid(toolCall.Arguments) {
			return nil, ErrInvalidRequest
		}
		converted = append(converted, openAICompatibleToolCall{
			ID:   id,
			Type: "function",
			Function: openAICompatibleToolFunctionCall{
				Name:      name,
				Arguments: string(toolCall.Arguments),
			},
		})
	}
	return converted, nil
}

func (c *OpenAICompatibleClient) responseFromChatResponse(decoded openAICompatibleChatResponse) (Response, error) {
	if len(decoded.Choices) == 0 {
		return Response{}, ErrInvalidResponse
	}
	choice := decoded.Choices[0]
	toolCalls, err := toolCallsFromOpenAICompatible(choice.Message.ToolCalls)
	if err != nil {
		return Response{}, err
	}
	if strings.TrimSpace(choice.Message.Content) == "" && len(toolCalls) == 0 {
		return Response{}, ErrInvalidResponse
	}
	model := strings.TrimSpace(decoded.Model)
	if model == "" {
		model = c.model
	}
	return Response{
		Text:         choice.Message.Content,
		ToolCalls:    toolCalls,
		FinishReason: choice.FinishReason,
		Model:        model,
		RawID:        decoded.ID,
		Usage: Usage{
			PromptTokens:     decoded.Usage.PromptTokens,
			CompletionTokens: decoded.Usage.CompletionTokens,
			TotalTokens:      decoded.Usage.TotalTokens,
		},
	}, nil
}

func toolCallsFromOpenAICompatible(toolCalls []openAICompatibleToolCall) ([]ToolCall, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}
	converted := make([]ToolCall, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		id := strings.TrimSpace(toolCall.ID)
		name := strings.TrimSpace(toolCall.Function.Name)
		if id == "" {
			return nil, ErrInvalidResponse
		}
		if name == "" {
			return nil, ErrInvalidResponse
		}
		arguments := json.RawMessage(toolCall.Function.Arguments)
		if !json.Valid(arguments) {
			return nil, ErrInvalidResponse
		}
		converted = append(converted, ToolCall{
			ID:        id,
			Name:      name,
			Arguments: arguments,
		})
	}
	return converted, nil
}

func isJSONObject(raw json.RawMessage) bool {
	if !json.Valid(raw) {
		return false
	}
	trimmed := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")
}

type openAICompatibleChatRequest struct {
	Model       string                        `json:"model"`
	Messages    []openAICompatibleChatMessage `json:"messages"`
	Tools       []openAICompatibleTool        `json:"tools,omitempty"`
	ToolChoice  any                           `json:"tool_choice,omitempty"`
	Stream      bool                          `json:"stream,omitempty"`
	Temperature *float32                      `json:"temperature,omitempty"`
	MaxTokens   *int                          `json:"max_tokens,omitempty"`
}

type openAICompatibleChatMessage struct {
	Role       string                     `json:"role"`
	Content    string                     `json:"content,omitempty"`
	ToolCallID string                     `json:"tool_call_id,omitempty"`
	ToolCalls  []openAICompatibleToolCall `json:"tool_calls,omitempty"`
}

type openAICompatibleTool struct {
	Type     string                       `json:"type"`
	Function openAICompatibleToolFunction `json:"function"`
}

type openAICompatibleToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openAICompatibleToolCall struct {
	ID       string                           `json:"id"`
	Index    int                              `json:"index,omitempty"`
	Type     string                           `json:"type"`
	Function openAICompatibleToolFunctionCall `json:"function"`
}

type openAICompatibleToolFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAICompatibleNamedToolChoice struct {
	Type     string                                  `json:"type"`
	Function openAICompatibleNamedToolChoiceFunction `json:"function"`
}

type openAICompatibleNamedToolChoiceFunction struct {
	Name string `json:"name"`
}

type openAICompatibleChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string                      `json:"finish_reason"`
		Message      openAICompatibleChatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAICompatibleChatStreamResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string                      `json:"finish_reason"`
		Delta        openAICompatibleChatMessage `json:"delta"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}
