package memory

import "testing"

func TestQueryNormalizesLimit(t *testing.T) {
	backend := &fakeBackend{}
	query := newQuery(backend)

	_, err := query.Search("agent-1", validEmbedding(), MaxSearchLimit+1)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if backend.searchLimit != MaxSearchLimit {
		t.Fatalf("expected search limit %d, got %d", MaxSearchLimit, backend.searchLimit)
	}

	_, err = query.Search("agent-1", validEmbedding(), 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if backend.searchLimit != defaultSearchLimit {
		t.Fatalf("expected default search limit %d, got %d", defaultSearchLimit, backend.searchLimit)
	}
}
