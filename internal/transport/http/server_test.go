package http

import (
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/model"
)

type fakeEmbedder struct {
	input  string
	vector embedding.Vector
	err    error
}

func (e *fakeEmbedder) Embed(ctx context.Context, input embedding.Input) (embedding.Vector, error) {
	e.input = input.Text
	if e.err != nil {
		return nil, e.err
	}
	if e.vector != nil {
		return e.vector, nil
	}
	return embedding.Vector{0, 1, 2, 3}, nil
}

type fakeStreamer struct {
	events []model.StreamEvent
	err    error
	block  <-chan struct{}
}

func (f *fakeStreamer) Stream(context.Context, model.Request) (<-chan model.StreamEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	events := make(chan model.StreamEvent, len(f.events))
	go func() {
		defer close(events)
		for _, event := range f.events {
			if f.block != nil {
				<-f.block
			}
			events <- event
		}
	}()
	return events, nil
}

type failingBackend struct{}

func (b *failingBackend) Close() error {
	return nil
}

func (b *failingBackend) Save(memory.Memory) error {
	return errors.New("sqlite secret path")
}

func (b *failingBackend) FindByID(string) (memory.Memory, bool, error) {
	return memory.Memory{}, false, nil
}

func (b *failingBackend) Search(string, string, []float32, int) ([]memory.SearchResult, error) {
	return nil, errors.New("sqlite secret path")
}

type recordingBackend struct {
	saved           memory.Memory
	saveErr         error
	saveCalls       int
	searchAgentID   string
	searchUserID    string
	searchEmbedding []float32
	searchLimit     int
	searchResults   []memory.SearchResult
}

func (b *recordingBackend) Close() error {
	return nil
}

func (b *recordingBackend) Save(saved memory.Memory) error {
	b.saveCalls++
	if b.saveErr != nil {
		return b.saveErr
	}
	b.saved = saved
	return nil
}

func (b *recordingBackend) FindByID(id string) (memory.Memory, bool, error) {
	if b.saved.ID == id {
		return b.saved, true, nil
	}
	return memory.Memory{}, false, nil
}

func (b *recordingBackend) Search(agentID string, userID string, embedding []float32, limit int) ([]memory.SearchResult, error) {
	b.searchAgentID = agentID
	b.searchUserID = userID
	b.searchEmbedding = embedding
	b.searchLimit = limit
	if b.searchResults != nil {
		return b.searchResults, nil
	}
	return []memory.SearchResult{
		{ID: "memory-1", Distance: 0.25, RecordedAt: time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC)},
	}, nil
}

type concurrentCreateBackend struct{}

func (b *concurrentCreateBackend) Close() error {
	return nil
}

func (b *concurrentCreateBackend) Save(memory.Memory) error {
	return nil
}

func (b *concurrentCreateBackend) FindByID(string) (memory.Memory, bool, error) {
	return memory.Memory{}, false, nil
}

func (b *concurrentCreateBackend) Search(string, string, []float32, int) ([]memory.SearchResult, error) {
	return nil, nil
}

func TestHealthReturnsOK(t *testing.T) {
	server := newTestServer(t, &recordingBackend{})

	recorder := performRequest(server, "GET", "/health", "")

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if strings.TrimSpace(recorder.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
}

func TestCreateMemoryReturnsCreatedID(t *testing.T) {
	backend := &recordingBackend{}
	markdownDir := t.TempDir()
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)
	server.handlers.now = func() time.Time { return time.Date(2026, 6, 8, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60)) }

	body := `{"id":"memory-1","agent_id":"agent-1","user_id":"user-1","question":"question","answer":"answer"}`
	recorder := performRequest(server, "POST", "/api/v1/memories", body)

	if recorder.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status 201, got %d", recorder.Code)
	}
	if strings.TrimSpace(recorder.Body.String()) != `{"id":"memory-1"}` {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
	if backend.saved.ID != "memory-1" {
		t.Fatalf("expected saved memory ID memory-1, got %q", backend.saved.ID)
	}
	if backend.saved.UserID != "user-1" {
		t.Fatalf("expected saved user ID user-1, got %q", backend.saved.UserID)
	}
	if backend.saved.Question != "question" {
		t.Fatalf("expected saved question, got %q", backend.saved.Question)
	}
	if backend.saved.Answer != "answer" {
		t.Fatalf("expected saved answer, got %q", backend.saved.Answer)
	}
	if backend.saved.Content != "question\nanswer" {
		t.Fatalf("expected synthesized content, got %q", backend.saved.Content)
	}
	if len(backend.saved.Embedding) != 0 {
		t.Fatalf("expected optional embedding to be omitted, got %v", backend.saved.Embedding)
	}

	markdownPath := filepath.Join(markdownDir, "user-1", "agent-1", "2026-06-08_Memory.md")
	content, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatalf("read memory markdown: %v", err)
	}
	markdown := string(content)
	for _, expected := range []string{"UserID: user-1", "AgentID: agent-1", "MemoryID: memory-1", "RecordedAt: 2026-06-08T01:30:00Z", "## Question", "question", "## Answer", "answer"} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("expected markdown to contain %q, got %q", expected, markdown)
		}
	}
}

func TestCreateMemoryAcceptsOptionalEmbedding(t *testing.T) {
	backend := &recordingBackend{}
	server := newTestServer(t, backend)

	body := `{"id":"memory-1","agent_id":"agent-1","user_id":"user-1","question":"question","answer":"answer","embedding":[0,1,2,3]}`
	recorder := performRequest(server, "POST", "/api/v1/memories", body)

	if recorder.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status 201, got %d", recorder.Code)
	}
	if len(backend.saved.Embedding) != 4 {
		t.Fatalf("expected saved embedding, got %v", backend.saved.Embedding)
	}
}

func TestCreateMemoryMasksInternalStoreErrors(t *testing.T) {
	markdownDir := t.TempDir()
	server := newTestServerWithMarkdownDir(t, &failingBackend{}, markdownDir)
	server.handlers.now = func() time.Time { return time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC) }

	body := `{"id":"memory-1","agent_id":"agent-1","user_id":"user-1","question":"question","answer":"answer"}`
	recorder := performRequest(server, "POST", "/api/v1/memories", body)

	if recorder.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "sqlite secret path") {
		t.Fatalf("response leaked internal error: %s", recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(markdownDir, "user-1", "agent-1", "2026-06-08_Memory.md")); err != nil {
		t.Fatalf("expected markdown to remain available for retry after save failure: %v", err)
	}
}

func TestCreateMemoryMarkdownFailureDoesNotSaveDatabase(t *testing.T) {
	backend := &recordingBackend{}
	root := t.TempDir()
	markdownDir := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(markdownDir, []byte("blocked"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)
	server.handlers.now = func() time.Time { return time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC) }

	body := `{"id":"memory-1","agent_id":"agent-1","user_id":"user-1","question":"question","answer":"answer"}`
	recorder := performRequest(server, "POST", "/api/v1/memories", body)

	if recorder.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
	if backend.saveCalls != 0 {
		t.Fatalf("expected markdown failure not to save database, got %d calls", backend.saveCalls)
	}
}

func TestCreateMemoryAppendsExistingMarkdown(t *testing.T) {
	backend := &recordingBackend{}
	markdownDir := t.TempDir()
	recordedAt := time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)
	writeTestMemoryMarkdown(t, markdownDir, memory.Memory{ID: "existing-memory", UserID: "user-1", AgentID: "agent-1", Question: "existing question", Answer: "existing answer", RecordedAt: recordedAt})
	markdownPath := filepath.Join(markdownDir, "user-1", "agent-1", "2026-06-08_Memory.md")
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)
	server.handlers.now = func() time.Time { return time.Date(2026, 6, 8, 10, 45, 0, 0, time.UTC) }

	body := `{"id":"memory-1","agent_id":"agent-1","user_id":"user-1","question":"question","answer":"answer"}`
	recorder := performRequest(server, "POST", "/api/v1/memories", body)

	if recorder.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status 201, got %d", recorder.Code)
	}
	content, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatalf("read existing markdown: %v", err)
	}
	markdown := string(content)
	if !strings.Contains(markdown, "MemoryID: existing-memory") {
		t.Fatalf("expected existing markdown entry to remain, got %q", markdown)
	}
	for _, expected := range []string{"MemoryID: memory-1", "RecordedAt: 2026-06-08T10:45:00Z", "## Question", "question", "## Answer", "answer"} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("expected appended markdown to contain %q, got %q", expected, markdown)
		}
	}
}

func TestCreateMemoryTreatsConcurrentMarkdownCreateAsSuccess(t *testing.T) {
	const requestCount = 32

	markdownDir := t.TempDir()
	server := newTestServerWithMarkdownDir(t, &concurrentCreateBackend{}, markdownDir)
	server.handlers.now = func() time.Time { return time.Date(2026, 6, 8, 11, 0, 0, 0, time.UTC) }

	recorders := make([]*httptest.ResponseRecorder, requestCount)
	var waitGroup sync.WaitGroup
	waitGroup.Add(requestCount)
	for i := 0; i < requestCount; i++ {
		i := i
		go func() {
			defer waitGroup.Done()

			body := fmt.Sprintf(
				`{"id":"memory-%d","agent_id":"agent-1","user_id":"user-1","question":"question-%d","answer":"answer-%d"}`,
				i,
				i,
				i,
			)
			recorders[i] = performRequest(server, "POST", "/api/v1/memories", body)
		}()
	}

	waitGroup.Wait()

	for i, recorder := range recorders {
		if recorder.Code != stdhttp.StatusCreated {
			t.Fatalf("request %d expected status 201, got %d with body %s", i, recorder.Code, recorder.Body.String())
		}
	}

	entries, err := os.ReadDir(filepath.Join(markdownDir, "user-1", "agent-1"))
	if err != nil {
		t.Fatalf("read markdown dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one markdown file, got %d", len(entries))
	}
	if entries[0].Name() != "2026-06-08_Memory.md" {
		t.Fatalf("expected memory markdown filename, got %q", entries[0].Name())
	}

	content, err := os.ReadFile(filepath.Join(markdownDir, "user-1", "agent-1", "2026-06-08_Memory.md"))
	if err != nil {
		t.Fatalf("read memory markdown: %v", err)
	}
	markdown := string(content)
	for _, expected := range []string{"UserID: user-1", "AgentID: agent-1", "RecordedAt: 2026-06-08T11:00:00Z", "## Question", "## Answer"} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("expected markdown to contain %q, got %q", expected, markdown)
		}
	}
	for i := 0; i < requestCount; i++ {
		for _, expected := range []string{
			fmt.Sprintf("MemoryID: memory-%d", i),
			fmt.Sprintf("question-%d", i),
			fmt.Sprintf("answer-%d", i),
		} {
			if !strings.Contains(markdown, expected) {
				t.Fatalf("expected markdown to contain %q, got %q", expected, markdown)
			}
		}
	}
}

func TestCreateMemoryRejectsInvalidPathIDs(t *testing.T) {
	backend := &recordingBackend{}
	markdownDir := t.TempDir()
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)
	server.handlers.now = func() time.Time { return time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC) }

	body := `{"id":"memory-1","agent_id":"agent:1","user_id":"user one","question":"question","answer":"answer"}`
	recorder := performRequest(server, "POST", "/api/v1/memories", body)

	if recorder.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
	if backend.saveCalls != 0 {
		t.Fatalf("expected invalid IDs not to reach storage, got %d saves", backend.saveCalls)
	}
	entries, err := os.ReadDir(markdownDir)
	if err != nil {
		t.Fatalf("read markdown root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no markdown files for invalid IDs, got %d entries", len(entries))
	}
}

func TestCreateMemoryWritesRecordedAtInUTC(t *testing.T) {
	backend := &recordingBackend{}
	markdownDir := t.TempDir()
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)
	server.handlers.now = func() time.Time {
		return time.Date(2026, 6, 8, 21, 4, 5, 0, time.FixedZone("UTC+8", 8*60*60))
	}

	body := `{"id":"memory-1","agent_id":"agent-1","user_id":"user-1","question":"question","answer":"answer"}`
	recorder := performRequest(server, "POST", "/api/v1/memories", body)

	if recorder.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status 201, got %d", recorder.Code)
	}

	content, err := os.ReadFile(filepath.Join(markdownDir, "user-1", "agent-1", "2026-06-08_Memory.md"))
	if err != nil {
		t.Fatalf("read memory markdown: %v", err)
	}
	if !strings.Contains(string(content), "RecordedAt: 2026-06-08T13:04:05Z") {
		t.Fatalf("expected UTC recorded time, got %q", string(content))
	}
}

func TestCreateMemorySplitsDifferentUTCDates(t *testing.T) {
	backend := &recordingBackend{}
	markdownDir := t.TempDir()
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)
	now := time.Date(2026, 6, 8, 23, 59, 0, 0, time.UTC)
	server.handlers.now = func() time.Time { return now }

	first := performRequest(server, "POST", "/api/v1/memories", `{"id":"memory-1","agent_id":"agent-1","user_id":"user-1","question":"q1","answer":"a1"}`)
	if first.Code != stdhttp.StatusCreated {
		t.Fatalf("expected first request 201, got %d", first.Code)
	}
	now = time.Date(2026, 6, 9, 0, 1, 0, 0, time.UTC)
	second := performRequest(server, "POST", "/api/v1/memories", `{"id":"memory-2","agent_id":"agent-1","user_id":"user-1","question":"q2","answer":"a2"}`)
	if second.Code != stdhttp.StatusCreated {
		t.Fatalf("expected second request 201, got %d", second.Code)
	}

	for _, filename := range []string{"2026-06-08_Memory.md", "2026-06-09_Memory.md"} {
		if _, err := os.Stat(filepath.Join(markdownDir, "user-1", "agent-1", filename)); err != nil {
			t.Fatalf("expected daily file %s: %v", filename, err)
		}
	}
}

func TestSearchMemoryRejectsLimitAboveMaximum(t *testing.T) {
	server := newTestServer(t, &failingBackend{})

	body := `{"agent_id":"agent-1","user_id":"user-1","question":"question","limit":101}`
	recorder := performRequest(server, "POST", "/api/v1/memories/search", body)

	if recorder.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestSearchMemoryRejectsExplicitZeroLimit(t *testing.T) {
	server := newTestServer(t, &failingBackend{})

	body := `{"agent_id":"agent-1","user_id":"user-1","question":"question","limit":0}`
	recorder := performRequest(server, "POST", "/api/v1/memories/search", body)

	if recorder.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestSearchMemoryRejectsMissingQuestion(t *testing.T) {
	server := newTestServer(t, &failingBackend{})

	body := `{"agent_id":"agent-1","user_id":"user-1"}`
	recorder := performRequest(server, "POST", "/api/v1/memories/search", body)

	if recorder.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestSearchMemoryEmbedsQuestionAndForwardsRequest(t *testing.T) {
	backend := &recordingBackend{}
	embedder := &fakeEmbedder{vector: embedding.Vector{4, 5, 6, 7}}
	markdownDir := t.TempDir()
	writeTestMemoryMarkdown(t, markdownDir, memory.Memory{
		ID: "memory-1", UserID: "user-1", AgentID: "agent-1", Question: "question", Answer: "answer",
		RecordedAt: time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC),
	})
	server := newTestServerWithMarkdownDirAndEmbedder(t, backend, markdownDir, embedder)

	body := `{"agent_id":"agent-1","user_id":"user-1","question":"where is it?"}`
	recorder := performRequest(server, "POST", "/api/v1/memories/search", body)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"results":[{"id":"memory-1","content":"## Memory\n\nMemoryID: memory-1`) {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
	if embedder.input != "where is it?" {
		t.Fatalf("expected embedded question, got %q", embedder.input)
	}
	if backend.searchAgentID != "agent-1" {
		t.Fatalf("expected search agent ID agent-1, got %q", backend.searchAgentID)
	}
	if backend.searchUserID != "user-1" {
		t.Fatalf("expected search user ID user-1, got %q", backend.searchUserID)
	}
	if backend.searchLimit != 10 {
		t.Fatalf("expected search limit 10, got %d", backend.searchLimit)
	}
	if fmt.Sprint(backend.searchEmbedding) != "[4 5 6 7]" {
		t.Fatalf("expected generated embedding, got %v", backend.searchEmbedding)
	}
}

func TestSearchMemoryReplacesDatabaseContentFromMarkdownEntry(t *testing.T) {
	recordedAt := time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC)
	backend := &recordingBackend{
		searchResults: []memory.SearchResult{
			{ID: "memory-2", Distance: 0.1, RecordedAt: recordedAt},
			{ID: "memory-1", Distance: 0.2, RecordedAt: recordedAt},
		},
	}
	markdownDir := t.TempDir()
	writeTestMemoryMarkdown(t, markdownDir, memory.Memory{ID: "memory-1", UserID: "user-1", AgentID: "agent-1", Question: "first question", Answer: "first answer", RecordedAt: recordedAt})
	writeTestMemoryMarkdown(t, markdownDir, memory.Memory{ID: "memory-2", UserID: "user-1", AgentID: "agent-1", Question: "second question", Answer: "second answer", RecordedAt: recordedAt})
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)

	body := `{"agent_id":"agent-1","user_id":"user-1","question":"question","limit":2}`
	recorder := performRequest(server, "POST", "/api/v1/memories/search", body)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	bodyText := recorder.Body.String()
	for _, expected := range []string{
		`{"id":"memory-2","content":"## Memory\n\nMemoryID: memory-2`,
		`"distance":0.1}`,
		`{"id":"memory-1","content":"## Memory\n\nMemoryID: memory-1`,
		`"distance":0.2}`,
	} {
		if !strings.Contains(bodyText, expected) {
			t.Fatalf("expected response to contain %q, got %s", expected, bodyText)
		}
	}
}

func TestSearchMemoryReadsCandidatesAcrossDailyFiles(t *testing.T) {
	firstDate := time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC)
	secondDate := time.Date(2026, 6, 9, 1, 30, 0, 0, time.UTC)
	backend := &recordingBackend{searchResults: []memory.SearchResult{
		{ID: "memory-2", Distance: 0.1, RecordedAt: secondDate},
		{ID: "memory-1", Distance: 0.2, RecordedAt: firstDate},
	}}
	markdownDir := t.TempDir()
	writeTestMemoryMarkdown(t, markdownDir, memory.Memory{ID: "memory-1", UserID: "user-1", AgentID: "agent-1", Question: "first", Answer: "answer", RecordedAt: firstDate})
	writeTestMemoryMarkdown(t, markdownDir, memory.Memory{ID: "memory-2", UserID: "user-1", AgentID: "agent-1", Question: "second", Answer: "answer", RecordedAt: secondDate})
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)

	recorder := performRequest(server, "POST", "/api/v1/memories/search", `{"agent_id":"agent-1","user_id":"user-1","question":"question","limit":2}`)
	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	secondIndex := strings.Index(body, `"id":"memory-2"`)
	firstIndex := strings.Index(body, `"id":"memory-1"`)
	if secondIndex < 0 || firstIndex < 0 || secondIndex > firstIndex {
		t.Fatalf("expected vector result order to be preserved, got %s", body)
	}
}

func TestSearchMemoryReturnsEmptyResultsWithoutReadingMarkdown(t *testing.T) {
	backend := &recordingBackend{searchResults: []memory.SearchResult{}}
	server := newTestServer(t, backend)

	body := `{"agent_id":"agent-1","user_id":"user-1","question":"question"}`
	recorder := performRequest(server, "POST", "/api/v1/memories/search", body)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if strings.TrimSpace(recorder.Body.String()) != `{"results":[]}` {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
}

func TestSearchMemoryMasksMissingMarkdownFile(t *testing.T) {
	backend := &recordingBackend{}
	server := newTestServer(t, backend)

	body := `{"agent_id":"agent-1","user_id":"user-1","question":"question"}`
	recorder := performRequest(server, "POST", "/api/v1/memories/search", body)

	if recorder.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "2026-06-08_Memory.md") {
		t.Fatalf("response leaked markdown path: %s", recorder.Body.String())
	}
}

func TestSearchMemoryFailsWholeRequestWhenOneEntryIsMissing(t *testing.T) {
	recordedAt := time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC)
	backend := &recordingBackend{searchResults: []memory.SearchResult{
		{ID: "memory-1", Distance: 0.1, RecordedAt: recordedAt},
		{ID: "missing", Distance: 0.2, RecordedAt: recordedAt},
	}}
	markdownDir := t.TempDir()
	writeTestMemoryMarkdown(t, markdownDir, memory.Memory{ID: "memory-1", UserID: "user-1", AgentID: "agent-1", Question: "question", Answer: "answer", RecordedAt: recordedAt})
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)

	recorder := performRequest(server, "POST", "/api/v1/memories/search", `{"agent_id":"agent-1","user_id":"user-1","question":"question","limit":2}`)
	if recorder.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestQAStreamReturnsTextDeltasAndPersistsMemory(t *testing.T) {
	backend := &recordingBackend{}
	server := newTestServerWithMarkdownDirAndDependencies(t, backend, t.TempDir(), &fakeEmbedder{}, &fakeStreamer{
		events: []model.StreamEvent{
			{TextDelta: "hello"},
			{TextDelta: " world", FinishReason: "stop"},
		},
	})

	body := `{"agent_id":"agent-1","user_id":"user-1","messages":[{"role":"user","content":"question"}]}`
	recorder := performRequest(server, "POST", "/api/v1/qa/stream", body)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	response := recorder.Body.String()
	for _, expected := range []string{
		`event: text_delta`,
		`"text":"hello"`,
		`"text":" world"`,
		`event: finished`,
		`"answer":"hello world"`,
		`"memory_saved":true`,
		`"markdown_synced":true`,
	} {
		if !strings.Contains(response, expected) {
			t.Fatalf("expected response to contain %q, got %s", expected, response)
		}
	}
	if backend.saved.Question != "question" || backend.saved.Answer != "hello world" {
		t.Fatalf("expected saved memory question/answer, got %#v", backend.saved)
	}
}

func TestCreateMemoryRetriesSQLiteWithoutDuplicatingMarkdown(t *testing.T) {
	backend := &recordingBackend{saveErr: errors.New("sqlite unavailable")}
	markdownDir := t.TempDir()
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)
	server.handlers.now = func() time.Time { return time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC) }
	body := `{"id":"memory-1","agent_id":"agent-1","user_id":"user-1","question":"question","answer":"answer"}`

	first := performRequest(server, "POST", "/api/v1/memories", body)
	if first.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected first request status 500, got %d", first.Code)
	}
	backend.saveErr = nil
	second := performRequest(server, "POST", "/api/v1/memories", body)
	if second.Code != stdhttp.StatusCreated {
		t.Fatalf("expected retry status 201, got %d: %s", second.Code, second.Body.String())
	}

	content, err := os.ReadFile(filepath.Join(markdownDir, "user-1", "agent-1", "2026-06-08_Memory.md"))
	if err != nil {
		t.Fatalf("read markdown: %v", err)
	}
	if count := strings.Count(string(content), "<!-- MemoryEntry:BEGIN id=memory-1 "); count != 1 {
		t.Fatalf("expected one markdown entry after retry, got %d", count)
	}
	if backend.saveCalls != 2 {
		t.Fatalf("expected two database save attempts, got %d", backend.saveCalls)
	}
}

func TestCreateMemorySameContentIsIdempotent(t *testing.T) {
	backend := &recordingBackend{}
	markdownDir := t.TempDir()
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)
	server.handlers.now = func() time.Time { return time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC) }
	body := `{"id":"memory-1","agent_id":"agent-1","user_id":"user-1","question":"question","answer":"answer"}`

	for i := 0; i < 2; i++ {
		recorder := performRequest(server, "POST", "/api/v1/memories", body)
		if recorder.Code != stdhttp.StatusCreated {
			t.Fatalf("request %d expected 201, got %d: %s", i, recorder.Code, recorder.Body.String())
		}
	}
	content, err := os.ReadFile(filepath.Join(markdownDir, "user-1", "agent-1", "2026-06-08_Memory.md"))
	if err != nil {
		t.Fatalf("read markdown: %v", err)
	}
	if count := strings.Count(string(content), "<!-- MemoryEntry:BEGIN id=memory-1 "); count != 1 {
		t.Fatalf("expected one markdown entry, got %d", count)
	}
	if backend.saveCalls != 1 {
		t.Fatalf("expected one database save, got %d", backend.saveCalls)
	}
}

func TestCreateMemoryRejectsSameIDDifferentContent(t *testing.T) {
	backend := &recordingBackend{}
	markdownDir := t.TempDir()
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)
	server.handlers.now = func() time.Time { return time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC) }

	first := performRequest(server, "POST", "/api/v1/memories", `{"id":"memory-1","agent_id":"agent-1","user_id":"user-1","question":"question","answer":"answer"}`)
	if first.Code != stdhttp.StatusCreated {
		t.Fatalf("expected first request 201, got %d", first.Code)
	}
	second := performRequest(server, "POST", "/api/v1/memories", `{"id":"memory-1","agent_id":"agent-1","user_id":"user-1","question":"question","answer":"different"}`)
	if second.Code != stdhttp.StatusConflict {
		t.Fatalf("expected conflict status 409, got %d: %s", second.Code, second.Body.String())
	}
	if backend.saveCalls != 1 {
		t.Fatalf("expected conflict not to save again, got %d calls", backend.saveCalls)
	}
}

func TestQAStreamReportsMarkdownSyncedWhenSQLiteSaveFails(t *testing.T) {
	backend := &recordingBackend{saveErr: errors.New("sqlite unavailable")}
	server := newTestServerWithMarkdownDirAndDependencies(t, backend, t.TempDir(), &fakeEmbedder{}, &fakeStreamer{
		events: []model.StreamEvent{{TextDelta: "answer", FinishReason: "stop"}},
	})

	recorder := performRequest(server, "POST", "/api/v1/qa/stream", `{"agent_id":"agent-1","user_id":"user-1","messages":[{"role":"user","content":"question"}]}`)
	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	response := recorder.Body.String()
	for _, expected := range []string{`event: warning`, `"memory_saved":false`, `"markdown_synced":true`} {
		if !strings.Contains(response, expected) {
			t.Fatalf("expected response to contain %q, got %s", expected, response)
		}
	}
}

func TestQAStreamReportsMarkdownFailureWithoutSavingDatabase(t *testing.T) {
	backend := &recordingBackend{}
	root := t.TempDir()
	markdownDir := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(markdownDir, []byte("blocked"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	server := newTestServerWithMarkdownDirAndDependencies(t, backend, markdownDir, &fakeEmbedder{}, &fakeStreamer{
		events: []model.StreamEvent{{TextDelta: "answer", FinishReason: "stop"}},
	})

	recorder := performRequest(server, "POST", "/api/v1/qa/stream", `{"agent_id":"agent-1","user_id":"user-1","messages":[{"role":"user","content":"question"}]}`)
	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	response := recorder.Body.String()
	for _, expected := range []string{`event: warning`, `"memory_saved":false`, `"markdown_synced":false`} {
		if !strings.Contains(response, expected) {
			t.Fatalf("expected response to contain %q, got %s", expected, response)
		}
	}
	if backend.saveCalls != 0 {
		t.Fatalf("expected markdown failure not to call database save, got %d", backend.saveCalls)
	}
}

func TestQAStreamReturnsWarningWhenEmbeddingFails(t *testing.T) {
	backend := &recordingBackend{}
	server := newTestServerWithMarkdownDirAndDependencies(t, backend, t.TempDir(), &fakeEmbedder{err: embedding.ErrProviderUnavailable}, &fakeStreamer{
		events: []model.StreamEvent{{TextDelta: "answer", FinishReason: "stop"}},
	})

	body := `{"agent_id":"agent-1","user_id":"user-1","messages":[{"role":"user","content":"question"}]}`
	recorder := performRequest(server, "POST", "/api/v1/qa/stream", body)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	response := recorder.Body.String()
	if !strings.Contains(response, `event: warning`) {
		t.Fatalf("expected warning event, got %s", response)
	}
	if !strings.Contains(response, `"memory_saved":false`) {
		t.Fatalf("expected unsaved memory in finished event, got %s", response)
	}
	if backend.saved.ID != "" {
		t.Fatalf("expected no saved memory, got %#v", backend.saved)
	}
}

func TestQAStreamRejectsMissingLastUserMessage(t *testing.T) {
	server := newTestServerWithMarkdownDirAndDependencies(t, &recordingBackend{}, t.TempDir(), &fakeEmbedder{}, &fakeStreamer{})

	body := `{"agent_id":"agent-1","user_id":"user-1","messages":[{"role":"assistant","content":"question"}]}`
	recorder := performRequest(server, "POST", "/api/v1/qa/stream", body)

	if recorder.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestQAStreamReturnsErrorEventOnModelStreamFailure(t *testing.T) {
	server := newTestServerWithMarkdownDirAndDependencies(t, &recordingBackend{}, t.TempDir(), &fakeEmbedder{}, &fakeStreamer{
		events: []model.StreamEvent{{Err: errors.New("boom")}},
	})

	body := `{"agent_id":"agent-1","user_id":"user-1","messages":[{"role":"user","content":"question"}]}`
	recorder := performRequest(server, "POST", "/api/v1/qa/stream", body)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	response := recorder.Body.String()
	if !strings.Contains(response, `event: error`) {
		t.Fatalf("expected error event, got %s", response)
	}
	if !strings.Contains(response, `"code":"model_stream_failed"`) {
		t.Fatalf("expected model stream failed code, got %s", response)
	}
	if strings.Contains(response, `event: finished`) {
		t.Fatalf("expected no finished event on stream error, got %s", response)
	}
}

func TestQAStreamEmitsHeartbeatDuringLongRunningStream(t *testing.T) {
	originalInterval := heartbeatInterval
	heartbeatInterval = 10 * time.Millisecond
	defer func() {
		heartbeatInterval = originalInterval
	}()

	release := make(chan struct{})
	var emitted atomic.Bool
	server := newTestServerWithMarkdownDirAndDependencies(t, &recordingBackend{}, t.TempDir(), &fakeEmbedder{}, &fakeStreamer{
		events: []model.StreamEvent{
			{TextDelta: "answer", FinishReason: "stop"},
		},
		block: release,
	})

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		body := `{"agent_id":"agent-1","user_id":"user-1","messages":[{"role":"user","content":"question"}]}`
		done <- performRequest(server, "POST", "/api/v1/qa/stream", body)
	}()

	var recorder *httptest.ResponseRecorder
	select {
	case recorder = <-done:
		t.Fatal("expected stream to wait before completing")
	case <-time.After(35 * time.Millisecond):
		close(release)
		recorder = <-done
		emitted.Store(true)
	}

	if !emitted.Load() {
		t.Fatal("expected heartbeat wait path to run")
	}
	response := recorder.Body.String()
	if !strings.Contains(response, `event: heartbeat`) {
		t.Fatalf("expected heartbeat event, got %s", response)
	}
	if !strings.Contains(response, `event: finished`) {
		t.Fatalf("expected finished event, got %s", response)
	}
}

func TestStatusFromErrorMapsMemoryValidationErrors(t *testing.T) {
	if status := statusFromError(memory.ErrInvalidEmbedding); status != stdhttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", status)
	}
	if status := statusFromError(embedding.ErrEmptyInput); status != stdhttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", status)
	}
	if status := statusFromError(embedding.ErrInvalidVector); status != stdhttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", status)
	}
	if status := statusFromError(embedding.ErrProviderUnavailable); status != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", status)
	}
	if status := statusFromError(memory.ErrMemoryConflict); status != stdhttp.StatusConflict {
		t.Fatalf("expected status 409, got %d", status)
	}
	if status := statusFromError(errors.New("unknown")); status != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", status)
	}
}

func writeTestMemoryMarkdown(t *testing.T, baseDir string, item memory.Memory) {
	t.Helper()
	item.StorageVersion = memory.DailyMarkdownStorageVersion
	if err := writeMemoryMarkdown(baseDir, item); err != nil {
		t.Fatalf("write test memory markdown: %v", err)
	}
}

func newTestServer(t *testing.T, backend memory.Backend) *Server {
	t.Helper()

	return newTestServerWithMarkdownDir(t, backend, t.TempDir())
}

func newTestServerWithMarkdownDir(t *testing.T, backend memory.Backend, markdownDir string) *Server {
	t.Helper()

	return newTestServerWithMarkdownDirAndEmbedder(t, backend, markdownDir, &fakeEmbedder{})
}

func newTestServerWithEmbedder(t *testing.T, backend memory.Backend, embedder embedding.Embedder) *Server {
	t.Helper()

	return newTestServerWithMarkdownDirAndEmbedder(t, backend, t.TempDir(), embedder)
}

func newTestServerWithMarkdownDirAndEmbedder(t *testing.T, backend memory.Backend, markdownDir string, embedder embedding.Embedder) *Server {
	t.Helper()

	return newTestServerWithMarkdownDirAndDependencies(t, backend, markdownDir, embedder, &fakeStreamer{})
}

func newTestServerWithMarkdownDirAndDependencies(t *testing.T, backend memory.Backend, markdownDir string, embedder embedding.Embedder, streamer model.Streamer) *Server {
	t.Helper()

	service, err := memory.NewService(backend)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return newServer(service, embedder, streamer, markdownDir)
}

func performRequest(server *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.router.ServeHTTP(recorder, req)
	return recorder
}
