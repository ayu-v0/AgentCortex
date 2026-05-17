package memory

import (
	"errors"
	"math"
	"testing"
)

func TestStoreRejectsInvalidEmbeddingValues(t *testing.T) {
	store := newStore(&fakeBackend{})

	err := store.Save(Memory{
		ID:        "memory-1",
		AgentID:   "agent-1",
		Content:   "content",
		Embedding: []float32{0, 1, 2, float32(math.Inf(1))},
	})
	if !errors.Is(err, ErrInvalidEmbeddingValue) {
		t.Fatalf("expected ErrInvalidEmbeddingValue, got %v", err)
	}
}

func TestStoreSavesValidMemory(t *testing.T) {
	backend := &fakeBackend{}
	store := newStore(backend)

	expected := Memory{
		ID:        "memory-1",
		AgentID:   "agent-1",
		Content:   "content",
		Embedding: validEmbedding(),
	}

	if err := store.Save(expected); err != nil {
		t.Fatalf("save: %v", err)
	}
	if backend.savedMemory.ID != expected.ID {
		t.Fatalf("expected saved memory ID %q, got %q", expected.ID, backend.savedMemory.ID)
	}
}
