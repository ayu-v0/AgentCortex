package http

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/memorymarkdown"
	"github.com/ayu-v0/agent-cortex/internal/model"
)

type parsedMemoryMarkdownEntry = memorymarkdown.Entry

func validateMemoryPathID(value string) error {
	if value == "" || value != strings.TrimSpace(value) {
		return model.ErrInvalidRequest
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' {
			continue
		}
		return model.ErrInvalidRequest
	}
	return nil
}

func validateMemoryMarkerID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") || strings.Contains(value, " -->") {
		return model.ErrInvalidRequest
	}
	return nil
}

func memoryMarkdownDir(baseDir, userID, agentID string) (string, error) {
	if err := validateMemoryPathID(userID); err != nil {
		return "", err
	}
	if err := validateMemoryPathID(agentID); err != nil {
		return "", err
	}
	dir, err := memorymarkdown.Dir(baseDir, userID, agentID)
	return dir, wrapMemoryMarkdownError(err)
}

func memoryMarkdownDailyFilename(recordedAt time.Time) (string, error) {
	filename, err := memorymarkdown.DailyFilename(recordedAt)
	return filename, wrapMemoryMarkdownError(err)
}

func memoryMarkdownDailyPath(baseDir, userID, agentID string, recordedAt time.Time) (string, string, error) {
	path, err := memorymarkdown.DailyPath(baseDir, userID, agentID, recordedAt)
	if err != nil {
		return "", "", wrapMemoryMarkdownError(err)
	}
	return filepath.Dir(path), filepath.Base(path), nil
}

func memoryMarkdownContent(item memory.Memory) (string, error) {
	content, err := memorymarkdown.FileContent(item)
	return content, wrapMemoryMarkdownError(err)
}

func memoryMarkdownAppendContent(item memory.Memory) (string, error) {
	content, err := memorymarkdown.AppendContent(item)
	return content, wrapMemoryMarkdownError(err)
}

func memoryMarkdownEntry(item memory.Memory, recordedAt time.Time) (string, error) {
	entry, err := memorymarkdown.RenderEntry(item, recordedAt)
	return entry, wrapMemoryMarkdownError(err)
}

func memoryMarkdownPayload(item memory.Memory, recordedAt time.Time) string {
	return memorymarkdown.Payload(item, recordedAt)
}

func memoryMarkdownEntriesByID(content string) (map[string]parsedMemoryMarkdownEntry, error) {
	entries, err := memorymarkdown.EntriesByID(content)
	return entries, wrapMemoryMarkdownError(err)
}

func readMemoryMarkdownEntries(path string) (map[string]parsedMemoryMarkdownEntry, error) {
	entries, err := memorymarkdown.ReadEntries(path)
	return entries, wrapMemoryMarkdownError(err)
}

func findMemoryMarkdownEntry(baseDir, userID, agentID, memoryID string) (parsedMemoryMarkdownEntry, bool, error) {
	entry, ok, err := memorymarkdown.FindEntry(baseDir, userID, agentID, memoryID)
	return entry, ok, wrapMemoryMarkdownError(err)
}

func isDailyMemoryMarkdownFilename(filename string) bool {
	const suffix = "_Memory.md"
	if !strings.HasSuffix(filename, suffix) {
		return false
	}
	datePart := strings.TrimSuffix(filename, suffix)
	_, err := time.Parse("2006-01-02", datePart)
	return err == nil
}

func parsedEntryMatchesMemory(entry parsedMemoryMarkdownEntry, item memory.Memory) bool {
	return memorymarkdown.EntryMatchesMemory(entry, item)
}

func writeMemoryMarkdown(baseDir string, item memory.Memory) error {
	return wrapMemoryMarkdownError(memorymarkdown.Write(baseDir, item))
}

func wrapMemoryMarkdownError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, memorymarkdown.ErrMarkdown) {
		return errors.Join(ErrMemoryMarkdown, err)
	}
	return err
}
