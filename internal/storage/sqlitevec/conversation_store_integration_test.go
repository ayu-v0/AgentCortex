//go:build cgo

package sqlitevec

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/conversation"
)

func TestConversationRecentTurnsAreScopedAndChronological(t *testing.T) {
	backend, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open backend: %v", err)
	}
	defer backend.Close()

	base := time.Date(2026, 6, 12, 1, 0, 0, 0, time.UTC)
	turns := []conversation.Turn{
		testTurn("user-1", "agent-1", "session-1", "q1", base.Add(1*time.Minute)),
		testTurn("user-1", "agent-1", "session-1", "q2", base.Add(2*time.Minute)),
		testTurn("user-1", "agent-1", "session-1", "q3", base.Add(3*time.Minute)),
		testTurn("user-1", "agent-1", "other-session", "other session", base.Add(4*time.Minute)),
		testTurn("user-2", "agent-1", "session-1", "other user", base.Add(5*time.Minute)),
		testTurn("user-1", "agent-2", "session-1", "other agent", base.Add(6*time.Minute)),
	}
	for _, turn := range turns {
		if err := backend.AppendTurn(turn); err != nil {
			t.Fatalf("append turn %q: %v", turn.Question, err)
		}
	}

	got, err := backend.RecentTurns(conversation.Scope{
		UserID:    "user-1",
		AgentID:   "agent-1",
		SessionID: "session-1",
	}, 2)
	if err != nil {
		t.Fatalf("recent turns: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected two turns, got %#v", got)
	}
	if got[0].Question != "q2" || got[1].Question != "q3" {
		t.Fatalf("expected newest two turns returned chronologically, got %#v", got)
	}
}

func TestConversationCleanupExpiredDeletesGlobally(t *testing.T) {
	backend, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open backend: %v", err)
	}
	defer backend.Close()

	base := time.Date(2026, 6, 12, 1, 0, 0, 0, time.UTC)
	for _, turn := range []conversation.Turn{
		testTurn("user-1", "agent-1", "session-1", "old", base.Add(-48*time.Hour)),
		testTurn("user-2", "agent-2", "session-2", "also old", base.Add(-47*time.Hour)),
		testTurn("user-1", "agent-1", "session-1", "new", base),
	} {
		if err := backend.AppendTurn(turn); err != nil {
			t.Fatalf("append turn %q: %v", turn.Question, err)
		}
	}

	deleted, err := backend.CleanupExpired(base.Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("cleanup expired: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected two deleted rows, got %d", deleted)
	}

	got, err := backend.RecentTurns(conversation.Scope{UserID: "user-1", AgentID: "agent-1", SessionID: "session-1"}, 10)
	if err != nil {
		t.Fatalf("recent turns: %v", err)
	}
	if len(got) != 1 || got[0].Question != "new" {
		t.Fatalf("expected only new turn to remain, got %#v", got)
	}
}

func testTurn(userID, agentID, sessionID, question string, createdAt time.Time) conversation.Turn {
	return conversation.Turn{
		UserID:    userID,
		AgentID:   agentID,
		SessionID: sessionID,
		Question:  question,
		Answer:    "answer",
		Source:    conversation.SourceQAStream,
		CreatedAt: createdAt,
	}
}
