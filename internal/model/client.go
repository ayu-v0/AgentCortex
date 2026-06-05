package model

import (
	"context"
	"encoding/json"
)

type Client interface {
	Generate(ctx context.Context, request Request) (Response, error)
}

type Streamer interface {
	Stream(ctx context.Context, request Request) (<-chan StreamEvent, error)
}

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role
	Content    string
	ToolCallID string
	ToolCalls  []ToolCall
}

type Request struct {
	Messages    []Message
	Tools       []Tool
	ToolChoice  *ToolChoice
	Temperature *float32
	MaxTokens   *int
}

type Response struct {
	Text         string
	ToolCalls    []ToolCall
	FinishReason string
	Model        string
	Usage        Usage
	RawID        string
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

type ToolChoiceMode string

const (
	ToolChoiceAuto     ToolChoiceMode = "auto"
	ToolChoiceNone     ToolChoiceMode = "none"
	ToolChoiceRequired ToolChoiceMode = "required"
	ToolChoiceTool     ToolChoiceMode = "tool"
)

type ToolChoice struct {
	Mode ToolChoiceMode
	Name string
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type StreamEvent struct {
	TextDelta     string
	ToolCallDelta *ToolCallDelta
	FinishReason  string
	Model         string
	Usage         Usage
	RawID         string
	Err           error
}

type ToolCallDelta struct {
	Index          int
	ID             string
	Name           string
	ArgumentsDelta string
}
