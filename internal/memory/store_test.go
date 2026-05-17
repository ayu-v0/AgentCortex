package memory

import (
	"errors"
	"math"
	"testing"
)

type fakeBackend struct {
	searchLimit int
}

func (b *fakeBackend) Close() error {
	return nil
}

func (b *fakeBackend) Save(memory Memory) error {
	return nil
}

func (b *fakeBackend) Search(agentID string, embedding []float32, limit int) ([]SearchResult, error) {
	b.searchLimit = limit
	return nil, nil
}

func TestNewStoreRejectsNilBackend(t *testing.T) {
	_, err := NewStore(nil)
	if !errors.Is(err, ErrNilBackend) {
		t.Fatalf("expected ErrNilBackend, got %v", err)
	}
}

func TestSearchNormalizesLimit(t *testing.T) {
	backend := &fakeBackend{}
	store, err := NewStore(backend)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	_, err = store.Search("agent-1", validEmbedding(), MaxSearchLimit+1)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if backend.searchLimit != MaxSearchLimit {
		t.Fatalf("expected search limit %d, got %d", MaxSearchLimit, backend.searchLimit)
	}

	_, err = store.Search("agent-1", validEmbedding(), 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if backend.searchLimit != defaultSearchLimit {
		t.Fatalf("expected default search limit %d, got %d", defaultSearchLimit, backend.searchLimit)
	}
}

func TestSaveRejectsInvalidEmbeddingValues(t *testing.T) {
	store, err := NewStore(&fakeBackend{})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	err = store.Save(Memory{
		ID:        "memory-1",
		AgentID:   "agent-1",
		Content:   "content",
		Embedding: []float32{0, 1, 2, float32(math.Inf(1))},
	})
	if !errors.Is(err, ErrInvalidEmbeddingValue) {
		t.Fatalf("expected ErrInvalidEmbeddingValue, got %v", err)
	}
}

func validEmbedding() []float32 {
	return []float32{0, 1, 2, 3}
}
