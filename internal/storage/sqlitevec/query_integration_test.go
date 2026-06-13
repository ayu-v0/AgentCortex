//go:build cgo

package sqlitevec

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

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
			ID:             "requested-user-memory",
			AgentID:        "agent-1",
			UserID:         "user-1",
			Content:        "requested user content",
			Embedding:      []float32{1, 1, 1, 1},
			RecordedAt:     time.Date(2026, 6, 8, 1, 0, 0, 0, time.UTC),
			StorageVersion: memory.DailyMarkdownStorageVersion,
		},
		{
			ID:             "other-user-memory",
			AgentID:        "agent-1",
			UserID:         "user-2",
			Content:        "other user content",
			Embedding:      []float32{0, 0, 0, 0},
			RecordedAt:     time.Date(2026, 6, 8, 2, 0, 0, 0, time.UTC),
			StorageVersion: memory.DailyMarkdownStorageVersion,
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
	if results[0].Content != "" {
		t.Fatalf("expected search not to read DB content, got %q", results[0].Content)
	}
	if results[0].RecordedAt.IsZero() {
		t.Fatal("expected search result recorded time")
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
	if len(results) != 0 {
		t.Fatalf("expected old storage version vectors to be excluded, got %#v", results)
	}
	stored, found, err := backend.FindByID("memory-1")
	if err != nil {
		t.Fatalf("find migrated memory: %v", err)
	}
	if !found {
		t.Fatal("expected migrated memory row to remain stored")
	}
	if stored.StorageVersion != 0 {
		t.Fatalf("expected migrated memory to remain storage version 0, got %d", stored.StorageVersion)
	}
}

func TestSaveDoesNotReplaceDuplicateMemoryID(t *testing.T) {
	backend, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open backend: %v", err)
	}
	defer backend.Close()

	recordedAt := time.Date(2026, 6, 8, 1, 0, 0, 0, time.UTC)
	first := memory.Memory{ID: "memory-1", AgentID: "agent-1", UserID: "user-1", Question: "question", Answer: "answer", Content: "question\nanswer", RecordedAt: recordedAt, StorageVersion: memory.DailyMarkdownStorageVersion}
	if err := backend.Save(first); err != nil {
		t.Fatalf("save first memory: %v", err)
	}
	second := first
	second.Answer = "different"
	second.Content = "question\ndifferent"
	if err := backend.Save(second); err == nil {
		t.Fatal("expected duplicate insert to fail")
	}
	stored, found, err := backend.FindByID(first.ID)
	if err != nil {
		t.Fatalf("find memory: %v", err)
	}
	if !found || stored.Answer != first.Answer {
		t.Fatalf("expected original memory to remain, got %#v", stored)
	}
}

func TestSearchFiltersStorageVersionBeforeKNNLimit(t *testing.T) {
	backend, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open backend: %v", err)
	}
	defer backend.Close()

	if _, err := backend.db.Exec(`
		INSERT INTO memories (id, agent_id, user_id, question, answer, content, storage_version)
		VALUES ('old-memory', 'agent-1', 'user-1', 'old', 'old', 'old', 0)
	`); err != nil {
		t.Fatalf("insert old memory: %v", err)
	}
	if _, err := backend.db.Exec(`
		INSERT INTO memory_vectors (memory_id, agent_id, user_id, storage_version, embedding)
		VALUES (?, ?, ?, ?, ?)
	`, "old-memory", "agent-1", "user-1", 0, float32VectorToBytes([]float32{0, 0, 0, 0})); err != nil {
		t.Fatalf("insert old vector: %v", err)
	}
	newMemory := memory.Memory{
		ID: "new-memory", AgentID: "agent-1", UserID: "user-1", Question: "new", Answer: "new", Content: "new",
		Embedding: []float32{1, 1, 1, 1}, RecordedAt: time.Date(2026, 6, 8, 1, 0, 0, 0, time.UTC), StorageVersion: memory.DailyMarkdownStorageVersion,
	}
	if err := backend.Save(newMemory); err != nil {
		t.Fatalf("save new memory: %v", err)
	}

	results, err := backend.Search("agent-1", "user-1", []float32{0, 0, 0, 0}, 1)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].ID != newMemory.ID {
		t.Fatalf("expected new storage version result despite closer old vector, got %#v", results)
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

	if _, err := db.Exec(`
		CREATE TABLE memories (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			user_id TEXT NOT NULL DEFAULT '',
			question TEXT NOT NULL DEFAULT '',
			answer TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
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
