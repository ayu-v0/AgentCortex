package conversation

import (
	"strings"
	"time"
)

const (
	DefaultRecentTurnLimit = 5
	MaxRecentTurnLimit     = 20
	MaxSessionIDLength     = 128
)

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	return &Service{
		store: store,
		now:   time.Now,
	}, nil
}

func (s *Service) SetNowForTest(now func() time.Time) {
	if now == nil {
		s.now = time.Now
		return
	}
	s.now = now
}

func (s *Service) AppendCompletedTurn(userID, agentID, sessionID, question, answer string) error {
	scope, err := normalizeScope(Scope{UserID: userID, AgentID: agentID, SessionID: sessionID})
	if err != nil {
		return err
	}
	question = strings.TrimSpace(question)
	answer = strings.TrimSpace(answer)
	if question == "" || answer == "" {
		return nil
	}
	return s.store.AppendTurn(Turn{
		UserID:    scope.UserID,
		AgentID:   scope.AgentID,
		SessionID: scope.SessionID,
		Question:  question,
		Answer:    answer,
		Source:    SourceQAStream,
		CreatedAt: s.now().UTC(),
	})
}

func (s *Service) RecentTurns(userID, agentID, sessionID string, limit int) ([]Turn, error) {
	scope, err := normalizeScope(Scope{UserID: userID, AgentID: agentID, SessionID: sessionID})
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = DefaultRecentTurnLimit
	}
	if limit > MaxRecentTurnLimit {
		return nil, ErrInvalidLimit
	}
	return s.store.RecentTurns(scope, limit)
}

func (s *Service) CleanupExpired(cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		return 0, ErrInvalidConfig
	}
	return s.store.CleanupExpired(cutoff.UTC())
}

func normalizeScope(scope Scope) (Scope, error) {
	scope.UserID = strings.TrimSpace(scope.UserID)
	scope.AgentID = strings.TrimSpace(scope.AgentID)
	scope.SessionID = strings.TrimSpace(scope.SessionID)
	if err := ValidatePathID(scope.UserID); err != nil {
		return Scope{}, err
	}
	if err := ValidatePathID(scope.AgentID); err != nil {
		return Scope{}, err
	}
	if err := ValidateSessionID(scope.SessionID); err != nil {
		return Scope{}, err
	}
	return scope, nil
}

func ValidatePathID(value string) error {
	if value == "" || value != strings.TrimSpace(value) {
		return ErrInvalidID
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' {
			continue
		}
		return ErrInvalidID
	}
	return nil
}

func ValidateSessionID(value string) error {
	if err := ValidatePathID(value); err != nil {
		return err
	}
	if len([]rune(value)) > MaxSessionIDLength {
		return ErrInvalidID
	}
	return nil
}
