//go:build cgo

package sqlitevec

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ayu-v0/agent-cortex/internal/memory"
	_ "github.com/mattn/go-sqlite3"
)

func TestSearchUsesUserFilterBeforeKNNLimit(t *testing.T) {
	backend, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open backend: %v", err)
	}
	defer backend.Close()

	for _, item := range []memory.Memory{
		{
			ID:        "requested-user-memory",
			AgentID:   "agent-1",
			UserID:    "user-1",
			Content:   "requested user content",
			Embedding: []float32{1, 1, 1, 1},
		},
		{
			ID:        "other-user-memory",
			AgentID:   "agent-1",
			UserID:    "user-2",
			Content:   "other user content",
			Embedding: []float32{0, 0, 0, 0},
		},
	} {
		if err := backend.Save(item); err != nil {
			t.Fatalf("save %s: %v", item.ID, err)
		}
	}

	results, err := backend.Search("agent-1", "user-1", []float32{0, 0, 0, 0}, 1)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one result for requested user, got %d: %#v", len(results), results)
	}
	if results[0].ID != "requested-user-memory" {
		t.Fatalf("expected requested user memory, got %q", results[0].ID)
	}
}

func TestMigratePreservesOldMemoryVectors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	if err := createOldVectorSchemaDatabase(path); err != nil {
		t.Fatalf("create old vector schema database: %v", err)
	}

	backend, err := Open(path)
	if err != nil {
		t.Fatalf("open migrated backend: %v", err)
	}
	defer backend.Close()

	results, err := backend.Search("agent-1", "user-1", []float32{0, 1, 2, 3}, 1)
	if err != nil {
		t.Fatalf("search migrated vectors: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected migrated vector to remain searchable, got %d results: %#v", len(results), results)
	}
	if results[0].ID != "memory-1" {
		t.Fatalf("expected memory-1, got %q", results[0].ID)
	}
}

func createOldVectorSchemaDatabase(path string) error {
	if err := registerSQLiteVec(); err != nil {
		return err
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.Exec(schemaSQL); err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE VIRTUAL TABLE memory_vectors USING vec0(
			memory_id TEXT PRIMARY KEY,
			embedding FLOAT[4]
		);
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		INSERT INTO memories (id, agent_id, user_id, question, answer, content)
		VALUES ('memory-1', 'agent-1', 'user-1', 'question', 'answer', 'content')
	`); err != nil {
		return err
	}
	_, err = db.Exec(`
		INSERT INTO memory_vectors (memory_id, embedding)
		VALUES (?, ?)
	`, "memory-1", float32VectorToBytes([]float32{0, 1, 2, 3}))
	return err
}
