package memory

import "testing"

func TestQueryNormalizesLimitAndForwardsUser(t *testing.T) {
	backend := &fakeBackend{}
	query := newQuery(backend)

	_, err := query.Search("agent-1", "user-1", validEmbedding(), MaxSearchLimit+1)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if backend.searchAgentID != "agent-1" {
		t.Fatalf("expected search agent ID agent-1, got %q", backend.searchAgentID)
	}
	if backend.searchUserID != "user-1" {
		t.Fatalf("expected search user ID user-1, got %q", backend.searchUserID)
	}
	if backend.searchLimit != MaxSearchLimit {
		t.Fatalf("expected search limit %d, got %d", MaxSearchLimit, backend.searchLimit)
	}

	_, err = query.Search("agent-1", "user-1", validEmbedding(), 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if backend.searchLimit != 10 {
		t.Fatalf("expected default search limit 10, got %d", backend.searchLimit)
	}
}
