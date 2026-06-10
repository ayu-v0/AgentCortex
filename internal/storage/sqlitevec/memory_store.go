package sqlitevec

import (
	"database/sql"
	"errors"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/memory"
)

type MemoryStore struct {
	db *sql.DB
}

func newMemoryStore(db *sql.DB) *MemoryStore {
	return &MemoryStore{db: db}
}

func (s *MemoryStore) Save(item memory.Memory) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	recordedAt := ""
	if !item.RecordedAt.IsZero() {
		recordedAt = item.RecordedAt.UTC().Format(time.RFC3339)
	}

	_, err = tx.Exec(`
		INSERT INTO memories (
			id, agent_id, user_id, question, answer, content, recorded_at, storage_version
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.AgentID, item.UserID, item.Question, item.Answer, item.Content, recordedAt, item.StorageVersion)
	if err != nil {
		return err
	}

	if _, err = tx.Exec(`
		DELETE FROM memory_vectors
		WHERE memory_id = ?
	`, item.ID); err != nil {
		return err
	}

	if len(item.Embedding) == 0 {
		err = tx.Commit()
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO memory_vectors (memory_id, agent_id, user_id, storage_version, embedding)
		VALUES (?, ?, ?, ?, ?)
	`, item.ID, item.AgentID, item.UserID, item.StorageVersion, float32VectorToBytes(item.Embedding))
	if err != nil {
		return err
	}

	err = tx.Commit()
	return err
}

func (s *MemoryStore) FindByID(id string) (memory.Memory, bool, error) {
	var (
		item       memory.Memory
		recordedAt string
	)
	err := s.db.QueryRow(`
		SELECT id, agent_id, user_id, question, answer, content, recorded_at, storage_version
		FROM memories
		WHERE id = ?
	`, id).Scan(
		&item.ID,
		&item.AgentID,
		&item.UserID,
		&item.Question,
		&item.Answer,
		&item.Content,
		&recordedAt,
		&item.StorageVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return memory.Memory{}, false, nil
	}
	if err != nil {
		return memory.Memory{}, false, err
	}
	if recordedAt != "" {
		parsed, err := time.Parse(time.RFC3339, recordedAt)
		if err != nil {
			return memory.Memory{}, false, err
		}
		item.RecordedAt = parsed.UTC()
	}
	return item, true, nil
}
