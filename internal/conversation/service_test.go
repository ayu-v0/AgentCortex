package conversation

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeStore struct {
	appended      []Turn
	recentScope   Scope
	recentLimit   int
	recentTurns   []Turn
	cleanupCutoff time.Time
}

func (f *fakeStore) AppendTurn(turn Turn) error {
	f.appended = append(f.appended, turn)
	return nil
}

func (f *fakeStore) RecentTurns(scope Scope, limit int) ([]Turn, error) {
	f.recentScope = scope
	f.recentLimit = limit
	return f.recentTurns, nil
}

func (f *fakeStore) CleanupExpired(cutoff time.Time) (int64, error) {
	f.cleanupCutoff = cutoff
	return 3, nil
}

func TestAppendCompletedTurnTrimsAndPersistsFinalQATurn(t *testing.T) {
	store := &fakeStore{}
	service, err := NewService(store)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	now := time.Date(2026, 6, 12, 10, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	service.SetNowForTest(func() time.Time { return now })

	if err := service.AppendCompletedTurn(" user-1 ", " agent-1 ", "session-1", " question ", " answer "); err != nil {
		t.Fatalf("append turn: %v", err)
	}

	if len(store.appended) != 1 {
		t.Fatalf("expected one appended turn, got %d", len(store.appended))
	}
	turn := store.appended[0]
	if turn.UserID != "user-1" || turn.AgentID != "agent-1" || turn.SessionID != "session-1" {
		t.Fatalf("unexpected scope: %#v", turn)
	}
	if turn.Question != "question" || turn.Answer != "answer" {
		t.Fatalf("expected trimmed question and answer, got %#v", turn)
	}
	if turn.Source != SourceQAStream {
		t.Fatalf("expected qa stream source, got %q", turn.Source)
	}
	if !turn.CreatedAt.Equal(now.UTC()) || turn.CreatedAt.Location() != time.UTC {
		t.Fatalf("expected UTC created time, got %s", turn.CreatedAt)
	}
}

func TestAppendCompletedTurnSkipsEmptyQuestionOrAnswer(t *testing.T) {
	store := &fakeStore{}
	service, err := NewService(store)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := service.AppendCompletedTurn("user-1", "agent-1", "session-1", " ", "answer"); err != nil {
		t.Fatalf("append empty question: %v", err)
	}
	if err := service.AppendCompletedTurn("user-1", "agent-1", "session-1", "question", " "); err != nil {
		t.Fatalf("append empty answer: %v", err)
	}

	if len(store.appended) != 0 {
		t.Fatalf("expected no appended turns, got %#v", store.appended)
	}
}

func TestRecentTurnsUsesDefaultAndRejectsTooLargeLimit(t *testing.T) {
	store := &fakeStore{}
	service, err := NewService(store)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if _, err := service.RecentTurns("user-1", "agent-1", "session-1", 0); err != nil {
		t.Fatalf("recent turns: %v", err)
	}
	if store.recentLimit != DefaultRecentTurnLimit {
		t.Fatalf("expected default limit %d, got %d", DefaultRecentTurnLimit, store.recentLimit)
	}
	if store.recentScope != (Scope{UserID: "user-1", AgentID: "agent-1", SessionID: "session-1"}) {
		t.Fatalf("unexpected scope: %#v", store.recentScope)
	}
	if _, err := service.RecentTurns("user-1", "agent-1", "session-1", MaxRecentTurnLimit+1); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("expected invalid limit, got %v", err)
	}
}

func TestValidateSessionID(t *testing.T) {
	valid := []string{"session-1", "SESSION_2", "abc123"}
	for _, value := range valid {
		if err := ValidateSessionID(value); err != nil {
			t.Fatalf("expected valid session ID %q: %v", value, err)
		}
	}

	invalid := []string{"", " session-1", "session 1", "session.1"}
	for _, value := range invalid {
		if err := ValidateSessionID(value); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("expected invalid session ID %q, got %v", value, err)
		}
	}
	if err := ValidateSessionID(strings.Repeat("a", MaxSessionIDLength+1)); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("expected overlong session ID to be invalid, got %v", err)
	}
}
