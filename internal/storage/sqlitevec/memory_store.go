package sqlitevec

import (
	"database/sql"

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

	_, err = tx.Exec(`
		INSERT OR REPLACE INTO memories (id, agent_id, content)
		VALUES (?, ?, ?)
	`, item.ID, item.AgentID, item.Content)
	if err != nil {
		return err
	}

	if _, err = tx.Exec(`
		DELETE FROM memory_vectors
		WHERE memory_id = ?
	`, item.ID); err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO memory_vectors (memory_id, embedding)
		VALUES (?, ?)
	`, item.ID, float32VectorToBytes(item.Embedding))
	if err != nil {
		return err
	}

	err = tx.Commit()
	return err
}
