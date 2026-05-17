package memory

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

const EmbeddingDimensions = 4

var ErrInvalidEmbedding = errors.New("embedding must contain exactly 4 values")

type Store struct {
	db *sql.DB
}

type Memory struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding,omitempty"`
}

type SearchResult struct {
	ID       string  `json:"id"`
	Content  string  `json:"content"`
	Distance float64 `json:"distance"`
}

func Open(path string) (*Store, error) {
	sqlitevec.Auto()

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Save(memory Memory) error {
	if err := validateEmbedding(memory.Embedding); err != nil {
		return err
	}

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

func (s *Store) Search(agentID string, embedding []float32, limit int) ([]SearchResult, error) {
	if err := validateEmbedding(embedding); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 5
	}

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

	results := make([]SearchResult, 0)
	for rows.Next() {
		var result SearchResult
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

func (s *Store) initSchema() error {
	_, err := s.db.Exec(`
		PRAGMA journal_mode = WAL;
		PRAGMA foreign_keys = ON;

		CREATE TABLE IF NOT EXISTS memories (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE VIRTUAL TABLE IF NOT EXISTS memory_vectors USING vec0(
			memory_id TEXT PRIMARY KEY,
			embedding FLOAT[4]
		);
	`)
	return err
}

func validateEmbedding(embedding []float32) error {
	if len(embedding) != EmbeddingDimensions {
		return fmt.Errorf("%w: got %d", ErrInvalidEmbedding, len(embedding))
	}
	return nil
}

func float32VectorToBytes(vector []float32) []byte {
	buf := make([]byte, len(vector)*4)
	for i, v := range vector {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}
