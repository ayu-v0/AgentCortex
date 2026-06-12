package retrieval

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ayu-v0/agent-cortex/internal/conversation"
	"github.com/ayu-v0/agent-cortex/internal/model"
)

type fakeModelClient struct {
	responses []model.Response
	errs      []error
	requests  []model.Request
}

func (f *fakeModelClient) Generate(_ context.Context, request model.Request) (model.Response, error) {
	f.requests = append(f.requests, request)
	index := len(f.requests) - 1
	if index < len(f.errs) && f.errs[index] != nil {
		return model.Response{}, f.errs[index]
	}
	if index < len(f.responses) {
		return f.responses[index], nil
	}
	return model.Response{}, nil
}

func TestModelRewriterSkipsModelWhenNoHistory(t *testing.T) {
	client := &fakeModelClient{}
	rewriter := newTestModelRewriter(t, client)

	result := rewriter.Rewrite(context.Background(), RewriteInput{
		UserID:    "user-1",
		AgentID:   "agent-1",
		SessionID: "session-1",
		Question:  "what about it?",
	})

	if result.Question != "what about it?" {
		t.Fatalf("expected original question, got %q", result.Question)
	}
	if len(client.requests) != 0 {
		t.Fatalf("expected no model calls, got %d", len(client.requests))
	}
	if result.Metadata.Stage1Status != "skipped" || result.Metadata.FinalQuerySource != "original" {
		t.Fatalf("unexpected metadata: %#v", result.Metadata)
	}
}

func TestModelRewriterUsesHighConfidenceStage1AndModelConfig(t *testing.T) {
	client := &fakeModelClient{responses: []model.Response{{
		Text: `{"resolved_question":"resolved from user history","confidence":0.91,"needs_assistant_context":false}`,
	}}}
	rewriter := newTestModelRewriter(t, client)

	result := rewriter.Rewrite(context.Background(), RewriteInput{
		UserID:    "user-1",
		AgentID:   "agent-1",
		SessionID: "session-1",
		Question:  "follow up question",
		Turns: []conversation.Turn{{
			Question: "previous user question",
			Answer:   "previous assistant answer",
		}},
	})

	if result.Question != "resolved from user history" {
		t.Fatalf("expected stage1 resolved question, got %q", result.Question)
	}
	if result.Metadata.Stage1Status != "success" || result.Metadata.Stage2Status != "not_run" || result.Metadata.FinalQuerySource != "stage1" {
		t.Fatalf("unexpected metadata: %#v", result.Metadata)
	}
	if len(client.requests) != 1 {
		t.Fatalf("expected one model call, got %d", len(client.requests))
	}
	request := client.requests[0]
	if request.Temperature == nil || *request.Temperature != 0.6 {
		t.Fatalf("expected temperature 0.6, got %#v", request.Temperature)
	}
	if request.MaxTokens == nil || *request.MaxTokens != 20000 {
		t.Fatalf("expected max tokens 20000, got %#v", request.MaxTokens)
	}
	if request.ToolChoice == nil || request.ToolChoice.Mode != model.ToolChoiceNone {
		t.Fatalf("expected tool choice none, got %#v", request.ToolChoice)
	}
	if strings.Contains(request.Messages[1].Content, "previous assistant answer") {
		t.Fatalf("expected stage1 prompt to omit assistant context, got %q", request.Messages[1].Content)
	}
}

func TestModelRewriterLowConfidenceRunsStage2AndAcceptsLowConfidenceResult(t *testing.T) {
	client := &fakeModelClient{responses: []model.Response{
		{Text: `{"resolved_question":"ambiguous","confidence":0.4,"needs_assistant_context":false}`},
		{Text: `{"resolved_question":"resolved with assistant answer","confidence":0.2,"needs_assistant_context":false}`},
	}}
	rewriter := newTestModelRewriter(t, client)

	result := rewriter.Rewrite(context.Background(), RewriteInput{
		UserID:    "user-1",
		AgentID:   "agent-1",
		SessionID: "session-1",
		Question:  "what about it?",
		Turns: []conversation.Turn{{
			Question: "which retrieval flow?",
			Answer:   "the memory retrieval coreference refactor",
		}},
	})

	if result.Question != "resolved with assistant answer" {
		t.Fatalf("expected stage2 resolved question, got %q", result.Question)
	}
	if result.Metadata.Stage1Status != "low_confidence" || result.Metadata.Stage2Status != "success" || result.Metadata.FinalQuerySource != "stage2" {
		t.Fatalf("unexpected metadata: %#v", result.Metadata)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected two model calls, got %d", len(client.requests))
	}
	if !strings.Contains(client.requests[1].Messages[1].Content, "Assistant: the memory retrieval coreference refactor") {
		t.Fatalf("expected stage2 prompt to include assistant context, got %q", client.requests[1].Messages[1].Content)
	}
}

func TestModelRewriterPronounHeuristicForcesStage2WhenUnchanged(t *testing.T) {
	client := &fakeModelClient{responses: []model.Response{
		{Text: `{"resolved_question":"what about it?","confidence":0.99,"needs_assistant_context":false}`},
		{Text: `{"resolved_question":"what about the sqlite cleanup scheduler?","confidence":0.8,"needs_assistant_context":false}`},
	}}
	rewriter := newTestModelRewriter(t, client)

	result := rewriter.Rewrite(context.Background(), RewriteInput{
		Question: "what about it?",
		Turns: []conversation.Turn{{
			Question: "should we add cleanup?",
			Answer:   "add a sqlite cleanup scheduler",
		}},
	})

	if result.Question != "what about the sqlite cleanup scheduler?" {
		t.Fatalf("expected stage2 resolved question, got %q", result.Question)
	}
	if result.Metadata.Stage1Status != "pronoun_heuristic" || result.Metadata.Stage2Status != "success" {
		t.Fatalf("unexpected metadata: %#v", result.Metadata)
	}
}

func TestModelRewriterInvalidOutputRetriesOnce(t *testing.T) {
	client := &fakeModelClient{responses: []model.Response{
		{Text: `{invalid-json}`},
		{Text: `{"resolved_question":"resolved after retry","confidence":0.9,"needs_assistant_context":false}`},
	}}
	rewriter := newTestModelRewriter(t, client)

	result := rewriter.Rewrite(context.Background(), RewriteInput{
		Question: "follow up",
		Turns:    []conversation.Turn{{Question: "previous"}},
	})

	if result.Question != "resolved after retry" {
		t.Fatalf("expected retry result, got %q", result.Question)
	}
	if len(client.requests) != 2 {
		t.Fatalf("expected one retry, got %d calls", len(client.requests))
	}
}

func TestModelRewriterProviderErrorDoesNotRetryAndFallsBack(t *testing.T) {
	client := &fakeModelClient{errs: []error{errors.New("provider down")}}
	rewriter := newTestModelRewriter(t, client)

	result := rewriter.Rewrite(context.Background(), RewriteInput{
		Question: "follow up",
		Turns:    []conversation.Turn{{Question: "previous"}},
	})

	if result.Question != "follow up" {
		t.Fatalf("expected fallback original, got %q", result.Question)
	}
	if len(client.requests) != 1 {
		t.Fatalf("expected no provider retry, got %d calls", len(client.requests))
	}
	if !result.Metadata.FallbackUsed || result.Metadata.Stage1Status != "provider_error" {
		t.Fatalf("unexpected metadata: %#v", result.Metadata)
	}
}

func TestParseRewriteOutputRepairsMalformedJSON(t *testing.T) {
	output, repaired, ok := parseRewriteOutput(`{"resolved_question":"fixed","confidence":0.9,"needs_assistant_context":false,}`)

	if !ok {
		t.Fatal("expected repairable JSON output")
	}
	if !repaired {
		t.Fatal("expected JSON repair to be reported")
	}
	if output.ResolvedQuestion != "fixed" {
		t.Fatalf("unexpected output: %#v", output)
	}
}

func TestParseRewriteOutputRejectsEmptyOrOversizedResolvedQuestion(t *testing.T) {
	if _, _, ok := parseRewriteOutput(`{"resolved_question":"","confidence":0.9,"needs_assistant_context":false}`); ok {
		t.Fatal("expected empty resolved question to be invalid")
	}

	oversized := strings.Repeat("x", maxResolvedQuestionLength+1)
	if _, _, ok := parseRewriteOutput(`{"resolved_question":"` + oversized + `","confidence":0.9,"needs_assistant_context":false}`); ok {
		t.Fatal("expected oversized resolved question to be invalid")
	}
}

func newTestModelRewriter(t *testing.T, client *fakeModelClient) *ModelRewriter {
	t.Helper()
	rewriter, err := NewModelRewriter(client)
	if err != nil {
		t.Fatalf("new rewriter: %v", err)
	}
	rewriter.SetLoggerForTest(func(string, ...any) {})
	return rewriter
}
