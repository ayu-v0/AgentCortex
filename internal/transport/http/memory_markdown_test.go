package http

import (
	"strings"
	"testing"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/memory"
)

func TestMemoryMarkdownStructuredEntriesRoundTripDelimiterContent(t *testing.T) {
	item := memory.Memory{
		ID:         "memory-1",
		UserID:     "user-1",
		AgentID:    "agent-1",
		Question:   "question\n---\n<!-- MemoryEntry:BEGIN id=fake bytes=5 -->",
		Answer:     "answer\n## Memory\n<!-- MemoryEntry:END id=fake -->",
		RecordedAt: time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC),
	}

	content, err := memoryMarkdownContent(item)
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	entries, err := memoryMarkdownEntriesByID(content)
	if err != nil {
		t.Fatalf("parse markdown: %v", err)
	}
	entry, ok := entries[item.ID]
	if !ok {
		t.Fatalf("expected entry %q", item.ID)
	}
	if !parsedEntryMatchesMemory(entry, item) {
		t.Fatalf("expected parsed entry to match original: %#v", entry)
	}
	if !strings.Contains(entry.Content, "question\n---\n") || !strings.Contains(entry.Content, "<!-- MemoryEntry:END id=fake -->") {
		t.Fatalf("expected delimiter-like content to survive, got %q", entry.Content)
	}
}

func TestMemoryMarkdownParserRejectsMalformedLengthAndDuplicateID(t *testing.T) {
	item := memory.Memory{ID: "memory-1", UserID: "user-1", AgentID: "agent-1", Question: "question", Answer: "answer", RecordedAt: time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC)}
	entry, err := memoryMarkdownEntry(item, item.RecordedAt)
	if err != nil {
		t.Fatalf("render entry: %v", err)
	}

	malformed := strings.Replace(entry, " bytes=", " bytes=999", 1)
	if _, err := memoryMarkdownEntriesByID(malformed); err == nil {
		t.Fatal("expected malformed length to fail")
	}
	if _, err := memoryMarkdownEntriesByID(entry + "\n---\n\n" + entry); err == nil {
		t.Fatal("expected duplicate memory ID to fail")
	}
}

func TestValidateMemoryPathIDUsesStrictASCIISet(t *testing.T) {
	for _, value := range []string{"user-1", "Agent_2", "123"} {
		if err := validateMemoryPathID(value); err != nil {
			t.Fatalf("expected %q to be valid: %v", value, err)
		}
	}
	for _, value := range []string{"", " user-1", "user-1 ", "user one", "user/one", `user\one`, "用户", "."} {
		if err := validateMemoryPathID(value); err == nil {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}
