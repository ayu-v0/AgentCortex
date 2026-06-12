package sqlitevec

import (
	"database/sql"
	"errors"
	"strings"
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

func (s *MemoryStore) FindMetadataByIDs(ids []string) (map[string]memory.Metadata, error) {
	if len(ids) == 0 {
		return map[string]memory.Metadata{}, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	rows, err := s.db.Query(`
		SELECT id, user_id, agent_id, recorded_at, storage_version
		FROM memories
		WHERE id IN (`+strings.Join(placeholders, ",")+`)
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	metadata := make(map[string]memory.Metadata, len(ids))
	for rows.Next() {
		var (
			item       memory.Metadata
			recordedAt string
		)
		if err := rows.Scan(&item.ID, &item.UserID, &item.AgentID, &recordedAt, &item.StorageVersion); err != nil {
			return nil, err
		}
		if recordedAt != "" {
			parsed, err := time.Parse(time.RFC3339, recordedAt)
			if err != nil {
				return nil, err
			}
			item.RecordedAt = parsed.UTC()
		}
		metadata[item.ID] = item
	}
	return metadata, rows.Err()
}
