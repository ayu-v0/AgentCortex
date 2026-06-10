package memory

import (
	"errors"
	"testing"
)

type fakeBackend struct {
	closed        bool
	savedMemory   Memory
	searchAgentID string
	searchUserID  string
	searchLimit   int
	foundMemory   Memory
	found         bool
}

func (b *fakeBackend) Close() error {
	b.closed = true
	return nil
}

func (b *fakeBackend) Save(memory Memory) error {
	b.savedMemory = memory
	return nil
}

func (b *fakeBackend) FindByID(string) (Memory, bool, error) {
	return b.foundMemory, b.found, nil
}

func (b *fakeBackend) Search(agentID string, userID string, embedding []float32, limit int) ([]SearchResult, error) {
	b.searchAgentID = agentID
	b.searchUserID = userID
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

func TestServiceSearchForwardsUserAndDefaultLimit(t *testing.T) {
	backend := &fakeBackend{}
	service, err := NewService(backend)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = service.Search("agent-1", "user-1", validEmbedding(), 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if backend.searchAgentID != "agent-1" {
		t.Fatalf("expected search agent ID agent-1, got %q", backend.searchAgentID)
	}
	if backend.searchUserID != "user-1" {
		t.Fatalf("expected search user ID user-1, got %q", backend.searchUserID)
	}
	if backend.searchLimit != 10 {
		t.Fatalf("expected default search limit 10, got %d", backend.searchLimit)
	}
}

func TestSameContentIgnoresEmbeddingAndRecordedMetadata(t *testing.T) {
	left := Memory{ID: "memory-1", AgentID: "agent-1", UserID: "user-1", Question: "question", Answer: "answer", Embedding: []float32{1}}
	right := left
	right.Embedding = []float32{2}
	right.StorageVersion = DailyMarkdownStorageVersion

	if !SameContent(left, right) {
		t.Fatal("expected memories with the same business content to match")
	}
	right.Answer = "different"
	if SameContent(left, right) {
		t.Fatal("expected different answers not to match")
	}
}

func validEmbedding() []float32 {
	return []float32{0, 1, 2, 3}
}
