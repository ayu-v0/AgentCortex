package sqlitevec

import (
	"database/sql"

	sqlitevecbinding "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

var _ memory.Backend = (*Store)(nil)

func Open(path string) (*Store, error) {
	sqlitevecbinding.Auto()

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Save(memory memory.Memory) error {
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
	`, memory.ID, memory.AgentID, memory.Content)
	if err != nil {
		return err
	}

	if _, err = tx.Exec(`
		DELETE FROM memory_vectors
		WHERE memory_id = ?
	`, memory.ID); err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO memory_vectors (memory_id, embedding)
		VALUES (?, ?)
	`, memory.ID, float32VectorToBytes(memory.Embedding))
	if err != nil {
		return err
	}

	err = tx.Commit()
	return err
}

func (s *Store) Search(agentID string, embedding []float32, limit int) ([]memory.SearchResult, error) {
	rows, err := s.db.Query(`
		SELECT
			m.id,
			m.content,
			v.distance
		FROM memory_vectors v
		JOIN memories m ON m.id = v.memory_id
		WHERE v.embedding MATCH ?
		  AND k = ?
		  AND m.agent_id = ?
		ORDER BY v.distance
	`, float32VectorToBytes(embedding), limit, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]memory.SearchResult, 0)
	for rows.Next() {
		var result memory.SearchResult
		if err := rows.Scan(&result.ID, &result.Content, &result.Distance); err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
