package http

import (
	"errors"
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

func TestCreateMemoryMasksInternalStoreErrors(t *testing.T) {
	store, err := memory.NewStore(&failingBackend{})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	server := NewServer(store)

	body := `{"id":"memory-1","agent_id":"agent-1","content":"content","embedding":[0,1,2,3]}`
	req := httptest.NewRequest("POST", "/api/v1/memories", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.router.ServeHTTP(recorder, req)

	if recorder.Code != 500 {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "sqlite secret path") {
		t.Fatalf("response leaked internal error: %s", recorder.Body.String())
	}
}

func TestSearchMemoryRejectsLimitAboveMaximum(t *testing.T) {
	store, err := memory.NewStore(&failingBackend{})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	server := NewServer(store)

	body := `{"agent_id":"agent-1","embedding":[0,1,2,3],"limit":101}`
	req := httptest.NewRequest("POST", "/api/v1/memories/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	server.router.ServeHTTP(recorder, req)

	if recorder.Code != 400 {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}
