package sqlitevec

import (
	"time"

	"github.com/ayu-v0/agent-cortex/internal/conversation"
)

var _ conversation.Store = (*Backend)(nil)

func (b *Backend) AppendTurn(turn conversation.Turn) error {
	createdAt := turn.CreatedAt.UTC().Format(time.RFC3339Nano)
	_, err := b.db.Exec(`
		INSERT INTO conversation_turns (
			user_id, agent_id, session_id, question, answer, source, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, turn.UserID, turn.AgentID, turn.SessionID, turn.Question, turn.Answer, turn.Source, createdAt)
	return err
}

func (b *Backend) RecentTurns(scope conversation.Scope, limit int) ([]conversation.Turn, error) {
	rows, err := b.db.Query(`
		SELECT id, user_id, agent_id, session_id, question, answer, source, created_at
		FROM conversation_turns
		WHERE user_id = ?
		  AND agent_id = ?
		  AND session_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, scope.UserID, scope.AgentID, scope.SessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	newestFirst := make([]conversation.Turn, 0, limit)
	for rows.Next() {
		var (
			turn      conversation.Turn
			createdAt string
		)
		if err := rows.Scan(
			&turn.ID,
			&turn.UserID,
			&turn.AgentID,
			&turn.SessionID,
			&turn.Question,
			&turn.Answer,
			&turn.Source,
			&createdAt,
		); err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, err
		}
		turn.CreatedAt = parsed.UTC()
		newestFirst = append(newestFirst, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for left, right := 0, len(newestFirst)-1; left < right; left, right = left+1, right-1 {
		newestFirst[left], newestFirst[right] = newestFirst[right], newestFirst[left]
	}
	return newestFirst, nil
}

func (b *Backend) CleanupExpired(cutoff time.Time) (int64, error) {
	result, err := b.db.Exec(`
		DELETE FROM conversation_turns
		WHERE created_at < ?
	`, cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
