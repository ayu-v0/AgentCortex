package retrieval

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/conversation"
	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/memorymarkdown"
)

type fakeConversationReader struct {
	turns     []conversation.Turn
	userID    string
	agentID   string
	sessionID string
	limit     int
	calls     int
	returnErr error
}

func (f *fakeConversationReader) RecentTurns(userID, agentID, sessionID string, limit int) ([]conversation.Turn, error) {
	f.calls++
	f.userID = userID
	f.agentID = agentID
	f.sessionID = sessionID
	f.limit = limit
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	return f.turns, nil
}

type fakeRewriter struct {
	result RewriteResult
	input  RewriteInput
	calls  int
}

func (f *fakeRewriter) Rewrite(_ context.Context, input RewriteInput) RewriteResult {
	f.calls++
	f.input = input
	return f.result
}

type fakeRetrievalEmbedder struct {
	text   string
	vector embedding.Vector
	err    error
}

func (f *fakeRetrievalEmbedder) Embed(_ context.Context, input embedding.Input) (embedding.Vector, error) {
	f.text = input.Text
	if f.err != nil {
		return nil, f.err
	}
	if f.vector != nil {
		return f.vector, nil
	}
	return embedding.Vector{1, 2, 3}, nil
}

type fakeVectorSearcher struct {
	agentID   string
	userID    string
	embedding []float32
	limit     int
	results   []memory.VectorSearchResult
	err       error
}

func (f *fakeVectorSearcher) SearchVectorIDs(agentID string, userID string, embedding []float32, limit int) ([]memory.VectorSearchResult, error) {
	f.agentID = agentID
	f.userID = userID
	f.embedding = embedding
	f.limit = limit
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

type fakeMetadataFinder struct {
	ids      []string
	metadata map[string]memory.Metadata
	err      error
}

func (f *fakeMetadataFinder) FindMetadataByIDs(ids []string) (map[string]memory.Metadata, error) {
	f.ids = append([]string(nil), ids...)
	if f.err != nil {
		return nil, f.err
	}
	return f.metadata, nil
}

func TestRetrieveEmbedsResolvedQuestionAndHydratesInVectorOrder(t *testing.T) {
	recordedAt := time.Date(2026, 6, 12, 1, 2, 3, 0, time.UTC)
	markdownDir := t.TempDir()
	writeRetrievalMemory(t, markdownDir, "memory-1", recordedAt, "first question", "first answer")
	writeRetrievalMemory(t, markdownDir, "memory-2", recordedAt, "second question", "second answer")

	conversations := &fakeConversationReader{turns: []conversation.Turn{{
		UserID: "user-1", AgentID: "agent-1", SessionID: "session-1", Question: "previous", Answer: "previous answer",
	}}}
	rewriter := &fakeRewriter{result: RewriteResult{
		Question: "resolved question",
		Metadata: RewriteMetadata{
			Stage1Status:     "success",
			FinalQuerySource: "stage1",
		},
	}}
	embedder := &fakeRetrievalEmbedder{vector: embedding.Vector{4, 5, 6}}
	vectorSearcher := &fakeVectorSearcher{results: []memory.VectorSearchResult{
		{ID: "memory-2", Distance: 0.1},
		{ID: "memory-1", Distance: 0.2},
	}}
	metadataFinder := &fakeMetadataFinder{metadata: map[string]memory.Metadata{
		"memory-1": retrievalMetadata("memory-1", recordedAt),
		"memory-2": retrievalMetadata("memory-2", recordedAt),
	}}
	service := newRetrievalTestService(t, conversations, rewriter, embedder, vectorSearcher, metadataFinder, markdownDir)
	var logs []string
	service.SetLoggerForTest(func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	})

	results, err := service.Retrieve(context.Background(), Request{
		UserID:    "user-1",
		AgentID:   "agent-1",
		SessionID: "session-1",
		Question:  "what about it?",
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	if conversations.userID != "user-1" || conversations.agentID != "agent-1" || conversations.sessionID != "session-1" {
		t.Fatalf("expected same-scope conversation lookup, got %#v", conversations)
	}
	if conversations.limit != conversation.DefaultRecentTurnLimit {
		t.Fatalf("expected default recent turn limit, got %d", conversations.limit)
	}
	if rewriter.calls != 1 || len(rewriter.input.Turns) != 1 {
		t.Fatalf("expected one rewrite with history, got calls=%d turns=%d", rewriter.calls, len(rewriter.input.Turns))
	}
	if embedder.text != "resolved question" {
		t.Fatalf("expected resolved question embedding, got %q", embedder.text)
	}
	if vectorSearcher.limit != 6 {
		t.Fatalf("expected over-fetch limit 6, got %d", vectorSearcher.limit)
	}
	if fmt.Sprint(vectorSearcher.embedding) != "[4 5 6]" {
		t.Fatalf("expected vector embedding, got %v", vectorSearcher.embedding)
	}
	if fmt.Sprint(metadataFinder.ids) != "[memory-2 memory-1]" {
		t.Fatalf("expected metadata lookup in vector order IDs, got %v", metadataFinder.ids)
	}
	if len(results) != 2 || results[0].ID != "memory-2" || results[1].ID != "memory-1" {
		t.Fatalf("expected vector order results, got %#v", results)
	}
	if !strings.Contains(results[0].Content, "second answer") || !strings.Contains(results[1].Content, "first answer") {
		t.Fatalf("expected markdown-backed content, got %#v", results)
	}
	if len(logs) == 0 || !strings.Contains(logs[0], `original_question="what about it?"`) || !strings.Contains(logs[0], `resolved_question="resolved question"`) {
		t.Fatalf("expected rewrite log with original and resolved question, got %#v", logs)
	}
}

func TestRetrieveSkipsInvalidCandidatesAndReturnsEmptyWhenAllSkipped(t *testing.T) {
	recordedAt := time.Date(2026, 6, 12, 1, 2, 3, 0, time.UTC)
	conversations := &fakeConversationReader{}
	rewriter := &fakeRewriter{result: RewriteResult{Question: "question"}}
	embedder := &fakeRetrievalEmbedder{}
	vectorSearcher := &fakeVectorSearcher{results: []memory.VectorSearchResult{
		{ID: "missing-metadata", Distance: 0.1},
		{ID: "missing-markdown", Distance: 0.2},
		{ID: "invalid-metadata", Distance: 0.3},
	}}
	metadataFinder := &fakeMetadataFinder{metadata: map[string]memory.Metadata{
		"missing-markdown": retrievalMetadata("missing-markdown", recordedAt),
		"invalid-metadata": {
			ID:             "invalid-metadata",
			UserID:         "user-1",
			AgentID:        "agent-1",
			RecordedAt:     recordedAt,
			StorageVersion: 999,
		},
	}}
	service := newRetrievalTestService(t, conversations, rewriter, embedder, vectorSearcher, metadataFinder, t.TempDir())

	results, err := service.Retrieve(context.Background(), Request{
		UserID:    "user-1",
		AgentID:   "agent-1",
		SessionID: "session-1",
		Question:  "question",
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty results when all candidates are skipped, got %#v", results)
	}
}

func newRetrievalTestService(
	t *testing.T,
	conversations ConversationReader,
	rewriter Rewriter,
	embedder embedding.Embedder,
	vectorSearcher VectorSearcher,
	metadataFinder MetadataFinder,
	markdownDir string,
) *Service {
	t.Helper()
	service, err := NewService(conversations, rewriter, embedder, vectorSearcher, metadataFinder, Config{
		MemoryMarkdownDir: markdownDir,
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func writeRetrievalMemory(t *testing.T, markdownDir, id string, recordedAt time.Time, question, answer string) {
	t.Helper()
	if err := memorymarkdown.Write(markdownDir, memory.Memory{
		ID:             id,
		UserID:         "user-1",
		AgentID:        "agent-1",
		Question:       question,
		Answer:         answer,
		RecordedAt:     recordedAt,
		StorageVersion: memory.DailyMarkdownStorageVersion,
	}); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
}

func retrievalMetadata(id string, recordedAt time.Time) memory.Metadata {
	return memory.Metadata{
		ID:             id,
		UserID:         "user-1",
		AgentID:        "agent-1",
		RecordedAt:     recordedAt,
		StorageVersion: memory.DailyMarkdownStorageVersion,
	}
}
