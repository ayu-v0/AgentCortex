package conversation

import "time"

const SourceQAStream = "qa_stream"

type Turn struct {
	ID        int64
	UserID    string
	AgentID   string
	SessionID string
	Question  string
	Answer    string
	Source    string
	CreatedAt time.Time
}

type Scope struct {
	UserID    string
	AgentID   string
	SessionID string
}

type Store interface {
	AppendTurn(turn Turn) error
	RecentTurns(scope Scope, limit int) ([]Turn, error)
	CleanupExpired(cutoff time.Time) (int64, error)
}
