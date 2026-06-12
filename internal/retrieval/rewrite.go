package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"

	jsonrepair "github.com/kaptinlin/jsonrepair"

	"github.com/ayu-v0/agent-cortex/internal/conversation"
	"github.com/ayu-v0/agent-cortex/internal/model"
)

const (
	rewriteInputBudget        = 20000
	maxResolvedQuestionLength = 100000
	stage1ConfidenceThreshold = 0.75
)

type ModelRewriter struct {
	client model.Client
	logger func(string, ...any)
}

func NewModelRewriter(client model.Client) (*ModelRewriter, error) {
	if client == nil {
		return nil, model.ErrInvalidConfig
	}
	return &ModelRewriter{client: client, logger: log.Printf}, nil
}

func (r *ModelRewriter) SetLoggerForTest(logger func(string, ...any)) {
	if logger == nil {
		r.logger = log.Printf
		return
	}
	r.logger = logger
}

func (r *ModelRewriter) Rewrite(ctx context.Context, input RewriteInput) RewriteResult {
	started := time.Now()
	question := strings.TrimSpace(input.Question)
	meta := RewriteMetadata{
		Stage1Status:          "skipped",
		Stage2Status:          "not_run",
		FinalQuerySource:      "original",
		HistoryTurnsUsed:      len(input.Turns),
		OriginalQuestionChars: len([]rune(question)),
		ResolvedQuestionChars: len([]rune(question)),
	}
	if len(input.Turns) == 0 {
		return RewriteResult{Question: question, Metadata: meta}
	}
	if len([]rune(question)) > rewriteInputBudget {
		meta.Stage1Status = "skipped_current_question_too_long"
		return RewriteResult{Question: question, Metadata: meta}
	}

	stage1, repaired, err := r.runStageWithRetry(ctx, stage1Prompt(question, input.Turns), "stage1")
	meta.JSONRepaired = meta.JSONRepaired || repaired
	if err != nil {
		meta.Stage1Status = "provider_error"
		meta.FallbackUsed = true
		meta.FinalQuerySource = "fallback_original"
		meta.ResolvedQuestionChars = len([]rune(question))
		r.logLatency(input, meta, started)
		return RewriteResult{Question: question, Metadata: meta}
	}
	if !stage1.valid {
		meta.Stage1Status = "invalid_output"
		return r.runStage2(ctx, input, question, meta, started)
	}

	meta.Stage1Confidence = stage1.output.Confidence
	if stage1.output.NeedsAssistantContext {
		meta.Stage1Status = "needs_assistant_context"
		return r.runStage2(ctx, input, question, meta, started)
	}
	if stage1.output.Confidence < stage1ConfidenceThreshold {
		meta.Stage1Status = "low_confidence"
		return r.runStage2(ctx, input, question, meta, started)
	}
	resolved := strings.TrimSpace(stage1.output.ResolvedQuestion)
	if hasCoreferenceMarker(question) && nearlySame(question, resolved) {
		meta.Stage1Status = "pronoun_heuristic"
		return r.runStage2(ctx, input, question, meta, started)
	}

	meta.Stage1Status = "success"
	meta.FinalQuerySource = "stage1"
	meta.ResolvedQuestionChars = len([]rune(resolved))
	r.logLatency(input, meta, started)
	return RewriteResult{Question: resolved, Metadata: meta}
}

func (r *ModelRewriter) runStage2(ctx context.Context, input RewriteInput, original string, meta RewriteMetadata, started time.Time) RewriteResult {
	prompt, assistantChars := stage2Prompt(original, input.Turns)
	meta.AssistantContextChars = assistantChars
	stage2, repaired, err := r.runStageWithRetry(ctx, prompt, "stage2")
	meta.JSONRepaired = meta.JSONRepaired || repaired
	if err != nil {
		meta.Stage2Status = "provider_error"
		meta.FallbackUsed = true
		meta.FinalQuerySource = "fallback_original"
		meta.ResolvedQuestionChars = len([]rune(original))
		r.logLatency(input, meta, started)
		return RewriteResult{Question: original, Metadata: meta}
	}
	if !stage2.valid {
		meta.Stage2Status = "invalid_output"
		meta.FallbackUsed = true
		meta.FinalQuerySource = "fallback_original"
		meta.ResolvedQuestionChars = len([]rune(original))
		r.logLatency(input, meta, started)
		return RewriteResult{Question: original, Metadata: meta}
	}

	resolved := strings.TrimSpace(stage2.output.ResolvedQuestion)
	meta.Stage2Status = "success"
	meta.FinalQuerySource = "stage2"
	meta.ResolvedQuestionChars = len([]rune(resolved))
	r.logLatency(input, meta, started)
	return RewriteResult{Question: resolved, Metadata: meta}
}

type stageResult struct {
	output rewriteOutput
	valid  bool
}

type rewriteOutput struct {
	ResolvedQuestion      string  `json:"resolved_question"`
	Confidence            float64 `json:"confidence"`
	NeedsAssistantContext bool    `json:"needs_assistant_context"`
}

func (r *ModelRewriter) runStageWithRetry(ctx context.Context, prompt string, stage string) (stageResult, bool, error) {
	repairedAny := false
	var last stageResult
	for attempt := 0; attempt < 2; attempt++ {
		response, err := r.generate(ctx, prompt)
		if err != nil {
			return stageResult{}, repairedAny, err
		}
		output, repaired, ok := parseRewriteOutput(response)
		repairedAny = repairedAny || repaired
		if ok {
			return stageResult{output: output, valid: true}, repairedAny, nil
		}
		last = stageResult{valid: false}
		r.logger("query rewrite invalid output stage=%q attempt=%d", stage, attempt+1)
	}
	return last, repairedAny, nil
}

func (r *ModelRewriter) generate(ctx context.Context, prompt string) (string, error) {
	temperature := float32(0.6)
	maxTokens := 20000
	toolChoice := &model.ToolChoice{Mode: model.ToolChoiceNone}
	response, err := r.client.Generate(ctx, model.Request{
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "Rewrite the user's latest question for semantic memory retrieval. Return only JSON with resolved_question, confidence, and needs_assistant_context."},
			{Role: model.RoleUser, Content: prompt},
		},
		ToolChoice:  toolChoice,
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
	})
	if err != nil {
		return "", err
	}
	if len(response.ToolCalls) > 0 || strings.TrimSpace(response.Text) == "" {
		return "", nil
	}
	return response.Text, nil
}

func parseRewriteOutput(raw string) (rewriteOutput, bool, bool) {
	cleaned := stripCodeFence(strings.TrimSpace(raw))
	var output rewriteOutput
	if err := json.Unmarshal([]byte(cleaned), &output); err == nil && validRewriteOutput(output) {
		return output, false, true
	}
	repaired, err := jsonrepair.Repair(cleaned)
	if err != nil {
		return rewriteOutput{}, false, false
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(repaired)), &output); err != nil {
		return rewriteOutput{}, true, false
	}
	if !validRewriteOutput(output) {
		return rewriteOutput{}, true, false
	}
	return output, true, true
}

func validRewriteOutput(output rewriteOutput) bool {
	resolved := strings.TrimSpace(output.ResolvedQuestion)
	return resolved != "" && len([]rune(resolved)) <= maxResolvedQuestionLength
}

func stripCodeFence(value string) string {
	if !strings.HasPrefix(value, "```") || !strings.HasSuffix(value, "```") {
		return value
	}
	lines := strings.Split(value, "\n")
	if len(lines) < 2 {
		return value
	}
	first := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(first, "```") {
		return value
	}
	lines = lines[1:]
	if strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func stage1Prompt(question string, turns []conversation.Turn) string {
	var entries []string
	remaining := rewriteInputBudget - len([]rune(question))
	for i := len(turns) - 1; i >= 0 && remaining > 0; i-- {
		text := strings.TrimSpace(turns[i].Question)
		if text == "" {
			continue
		}
		entry := fmt.Sprintf("User question: %s", cropRunes(text, remaining))
		remaining -= len([]rune(entry))
		entries = append([]string{entry}, entries...)
	}
	return fmt.Sprintf("Recent user questions:\n%s\n\nCurrent question:\n%s", strings.Join(entries, "\n"), question)
}

func stage2Prompt(question string, turns []conversation.Turn) (string, int) {
	remaining := rewriteInputBudget - len([]rune(question))
	var entries []string
	assistantChars := 0
	for i := len(turns) - 1; i >= 0 && remaining > 0; i-- {
		user := strings.TrimSpace(turns[i].Question)
		answer := strings.TrimSpace(turns[i].Answer)
		var entry strings.Builder
		if user != "" {
			userLine := fmt.Sprintf("User: %s\n", cropRunes(user, remaining))
			entry.WriteString(userLine)
			remaining -= len([]rune(userLine))
		}
		if answer != "" && remaining > 0 {
			cropped := cropMiddle(answer, remaining)
			assistantChars += len([]rune(cropped))
			answerLine := fmt.Sprintf("Assistant: %s", cropped)
			entry.WriteString(answerLine)
			remaining -= len([]rune(answerLine))
		}
		text := strings.TrimSpace(entry.String())
		if text != "" {
			entries = append([]string{text}, entries...)
		}
	}
	return fmt.Sprintf("Recent conversation:\n%s\n\nCurrent question:\n%s", strings.Join(entries, "\n\n"), question), assistantChars
}

func cropRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func cropMiddle(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= len("...[omitted]...") {
		return string(runes[:limit])
	}
	marker := []rune("...[omitted]...")
	remaining := limit - len(marker)
	head := remaining / 2
	tail := remaining - head
	return string(runes[:head]) + string(marker) + string(runes[len(runes)-tail:])
}

func hasCoreferenceMarker(question string) bool {
	lower := strings.ToLower(question)
	phraseMarkers := []string{
		"the previous",
		"\u5b83",
		"\u8fd9\u4e2a",
		"\u90a3\u4e2a",
		"\u4e0a\u8ff0",
		"\u524d\u9762",
		"\u521a\u624d",
	}
	for _, marker := range phraseMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	wordMarkers := map[string]struct{}{
		"this": {},
		"that": {},
		"it":   {},
		"they": {},
	}
	for _, token := range strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if _, ok := wordMarkers[token]; ok {
			return true
		}
	}
	return false
}

func nearlySame(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func (r *ModelRewriter) logLatency(input RewriteInput, meta RewriteMetadata, started time.Time) {
	r.logger("query rewrite completed user_id=%q agent_id=%q session_id=%q latency_ms=%d final_query_source=%q fallback_used=%t", input.UserID, input.AgentID, input.SessionID, time.Since(started).Milliseconds(), meta.FinalQuerySource, meta.FallbackUsed)
}
