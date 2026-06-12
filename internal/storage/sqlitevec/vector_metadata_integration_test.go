//go:build cgo

package sqlitevec

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/memory"
)

func TestSearchVectorIDsAndFindMetadataByIDsAreSplit(t *testing.T) {
	backend, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open backend: %v", err)
	}
	defer backend.Close()

	recordedAt := time.Date(2026, 6, 12, 1, 0, 0, 0, time.UTC)
	for _, item := range []memory.Memory{
		{
			ID: "memory-1", UserID: "user-1", AgentID: "agent-1", Question: "q1", Answer: "a1", Content: "q1\na1",
			Embedding: []float32{0, 0, 0, 0}, RecordedAt: recordedAt, StorageVersion: memory.DailyMarkdownStorageVersion,
		},
		{
			ID: "memory-2", UserID: "user-1", AgentID: "agent-1", Question: "q2", Answer: "a2", Content: "q2\na2",
			Embedding: []float32{1, 1, 1, 1}, RecordedAt: recordedAt.Add(time.Minute), StorageVersion: memory.DailyMarkdownStorageVersion,
		},
	} {
		if err := backend.Save(item); err != nil {
			t.Fatalf("save %s: %v", item.ID, err)
		}
	}

	candidates, err := backend.SearchVectorIDs("agent-1", "user-1", []float32{0, 0, 0, 0}, 2)
	if err != nil {
		t.Fatalf("search vector ids: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected two candidates, got %#v", candidates)
	}
	if candidates[0].ID != "memory-1" {
		t.Fatalf("expected nearest candidate memory-1, got %#v", candidates)
	}

	metadata, err := backend.FindMetadataByIDs([]string{"memory-2", "missing", "memory-1"})
	if err != nil {
		t.Fatalf("find metadata: %v", err)
	}
	if len(metadata) != 2 {
		t.Fatalf("expected metadata for existing IDs only, got %#v", metadata)
	}
	if metadata["memory-1"].RecordedAt.IsZero() || metadata["memory-1"].StorageVersion != memory.DailyMarkdownStorageVersion {
		t.Fatalf("expected markdown location metadata, got %#v", metadata["memory-1"])
	}
	if _, ok := metadata["missing"]; ok {
		t.Fatalf("expected missing ID to be omitted, got %#v", metadata)
	}
}
