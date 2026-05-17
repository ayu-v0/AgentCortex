package memory

import (
	"errors"
	"testing"
)

type fakeBackend struct {
	closed      bool
	savedMemory Memory
	searchLimit int
}

func (b *fakeBackend) Close() error {
	b.closed = true
	return nil
}

func (b *fakeBackend) Save(memory Memory) error {
	b.savedMemory = memory
	return nil
}

func (b *fakeBackend) Search(agentID string, embedding []float32, limit int) ([]SearchResult, error) {
	b.searchLimit = limit
	return nil, nil
}

func TestNewServiceRejectsNilBackend(t *testing.T) {
	_, err := NewService(nil)
	if !errors.Is(err, ErrNilBackend) {
		t.Fatalf("expected ErrNilBackend, got %v", err)
	}
}

func TestServiceCloseClosesBackend(t *testing.T) {
	backend := &fakeBackend{}
	service, err := NewService(backend)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := service.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !backend.closed {
		t.Fatal("expected backend to be closed")
	}
}

func validEmbedding() []float32 {
	return []float32{0, 1, 2, 3}
}
