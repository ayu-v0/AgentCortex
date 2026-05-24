package http

import (
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ayu-v0/agent-cortex/internal/memory"
)

type failingBackend struct{}

func (b *failingBackend) Close() error {
	return nil
}

func (b *failingBackend) Save(memory.Memory) error {
	return errors.New("sqlite secret path")
}

func (b *failingBackend) Search(string, []float32, int) ([]memory.SearchResult, error) {
	return nil, errors.New("sqlite secret path")
}

type recordingBackend struct {
	saved       memory.Memory
	searchLimit int
}

func (b *recordingBackend) Close() error {
	return nil
}

func (b *recordingBackend) Save(saved memory.Memory) error {
	b.saved = saved
	return nil
}

func (b *recordingBackend) Search(agentID string, embedding []float32, limit int) ([]memory.SearchResult, error) {
	b.searchLimit = limit
	return []memory.SearchResult{
		{ID: "memory-1", Content: "content", Distance: 0.25},
	}, nil
}

type concurrentCreateBackend struct {
	ready   chan<- struct{}
	release <-chan struct{}
}

func (b *concurrentCreateBackend) Close() error {
	return nil
}

func (b *concurrentCreateBackend) Save(memory.Memory) error {
	b.ready <- struct{}{}
	<-b.release
	return nil
}

func (b *concurrentCreateBackend) Search(string, []float32, int) ([]memory.SearchResult, error) {
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

	markdownPath := filepath.Join(markdownDir, "user-1_agent-1_Memory.md")
	content, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatalf("read memory markdown: %v", err)
	}
	markdown := string(content)
	for _, expected := range []string{"UserID: user-1", "AgentID: agent-1", "MemoryID: memory-1", "## Question", "question", "## Answer", "answer"} {
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

	body := `{"id":"memory-1","agent_id":"agent-1","user_id":"user-1","question":"question","answer":"answer"}`
	recorder := performRequest(server, "POST", "/api/v1/memories", body)

	if recorder.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "sqlite secret path") {
		t.Fatalf("response leaked internal error: %s", recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(markdownDir, "user-1_agent-1_Memory.md")); !os.IsNotExist(err) {
		t.Fatalf("expected markdown not to be created after save failure, got err %v", err)
	}
}

func TestCreateMemoryDoesNotOverwriteExistingMarkdown(t *testing.T) {
	backend := &recordingBackend{}
	markdownDir := t.TempDir()
	markdownPath := filepath.Join(markdownDir, "user-1_agent-1_Memory.md")
	if err := os.WriteFile(markdownPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing markdown: %v", err)
	}
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)

	body := `{"id":"memory-1","agent_id":"agent-1","user_id":"user-1","question":"question","answer":"answer"}`
	recorder := performRequest(server, "POST", "/api/v1/memories", body)

	if recorder.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status 201, got %d", recorder.Code)
	}
	content, err := os.ReadFile(markdownPath)
	if err != nil {
		t.Fatalf("read existing markdown: %v", err)
	}
	if string(content) != "existing" {
		t.Fatalf("expected existing markdown to remain, got %q", string(content))
	}
}

func TestCreateMemoryTreatsConcurrentMarkdownCreateAsSuccess(t *testing.T) {
	const requestCount = 32

	ready := make(chan struct{}, requestCount)
	release := make(chan struct{})
	markdownDir := t.TempDir()
	server := newTestServerWithMarkdownDir(t, &concurrentCreateBackend{
		ready:   ready,
		release: release,
	}, markdownDir)

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

	for i := 0; i < requestCount; i++ {
		<-ready
	}
	close(release)
	waitGroup.Wait()

	for i, recorder := range recorders {
		if recorder.Code != stdhttp.StatusCreated {
			t.Fatalf("request %d expected status 201, got %d with body %s", i, recorder.Code, recorder.Body.String())
		}
	}

	entries, err := os.ReadDir(markdownDir)
	if err != nil {
		t.Fatalf("read markdown dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one markdown file, got %d", len(entries))
	}
	if entries[0].Name() != "user-1_agent-1_Memory.md" {
		t.Fatalf("expected memory markdown filename, got %q", entries[0].Name())
	}

	content, err := os.ReadFile(filepath.Join(markdownDir, "user-1_agent-1_Memory.md"))
	if err != nil {
		t.Fatalf("read memory markdown: %v", err)
	}
	markdown := string(content)
	for _, expected := range []string{"UserID: user-1", "AgentID: agent-1", "## Question", "## Answer"} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("expected markdown to contain %q, got %q", expected, markdown)
		}
	}
}

func TestCreateMemorySanitizesMarkdownFilename(t *testing.T) {
	backend := &recordingBackend{}
	markdownDir := t.TempDir()
	server := newTestServerWithMarkdownDir(t, backend, markdownDir)

	body := `{"id":"memory-1","agent_id":"agent:1","user_id":"user one","question":"question","answer":"answer"}`
	recorder := performRequest(server, "POST", "/api/v1/memories", body)

	if recorder.Code != stdhttp.StatusCreated {
		t.Fatalf("expected status 201, got %d", recorder.Code)
	}
	if _, err := os.Stat(filepath.Join(markdownDir, "user_one_agent_1_Memory.md")); err != nil {
		t.Fatalf("expected sanitized markdown filename: %v", err)
	}
}

func TestSearchMemoryRejectsLimitAboveMaximum(t *testing.T) {
	server := newTestServer(t, &failingBackend{})

	body := `{"agent_id":"agent-1","embedding":[0,1,2,3],"limit":101}`
	recorder := performRequest(server, "POST", "/api/v1/memories/search", body)

	if recorder.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestSearchMemoryReturnsResults(t *testing.T) {
	backend := &recordingBackend{}
	server := newTestServer(t, backend)

	body := `{"agent_id":"agent-1","embedding":[0,1,2,3],"limit":10}`
	recorder := performRequest(server, "POST", "/api/v1/memories/search", body)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"results":[{"id":"memory-1","content":"content","distance":0.25}]`) {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
	if backend.searchLimit != 10 {
		t.Fatalf("expected search limit 10, got %d", backend.searchLimit)
	}
}

func TestStatusFromErrorMapsMemoryValidationErrors(t *testing.T) {
	if status := statusFromError(memory.ErrInvalidEmbedding); status != stdhttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", status)
	}
	if status := statusFromError(errors.New("unknown")); status != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", status)
	}
}

func newTestServer(t *testing.T, backend memory.Backend) *Server {
	t.Helper()

	return newTestServerWithMarkdownDir(t, backend, t.TempDir())
}

func newTestServerWithMarkdownDir(t *testing.T, backend memory.Backend, markdownDir string) *Server {
	t.Helper()

	service, err := memory.NewService(backend)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return newServer(service, markdownDir)
}

func performRequest(server *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.router.ServeHTTP(recorder, req)
	return recorder
}
