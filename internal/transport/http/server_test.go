package http

import (
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
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
	server := newTestServer(t, backend)

	body := `{"id":"memory-1","agent_id":"agent-1","content":"content","embedding":[0,1,2,3]}`
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
}

func TestCreateMemoryMasksInternalStoreErrors(t *testing.T) {
	server := newTestServer(t, &failingBackend{})

	body := `{"id":"memory-1","agent_id":"agent-1","content":"content","embedding":[0,1,2,3]}`
	recorder := performRequest(server, "POST", "/api/v1/memories", body)

	if recorder.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "sqlite secret path") {
		t.Fatalf("response leaked internal error: %s", recorder.Body.String())
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

	service, err := memory.NewService(backend)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return NewServer(service)
}

func performRequest(server *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.router.ServeHTTP(recorder, req)
	return recorder
}
