package http

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/model"
	"github.com/ayu-v0/agent-cortex/internal/utils"
)

const (
	memoryEntryBeginPrefix = "<!-- MemoryEntry:BEGIN id="
	memoryEntryEndPrefix   = "<!-- MemoryEntry:END id="
	memoryEntryMarkerEnd   = " -->"
)

type parsedMemoryMarkdownEntry struct {
	ID         string
	RecordedAt time.Time
	Payload    string
	Content    string
}

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
	if value == "" || strings.ContainsAny(value, "\r\n") || strings.Contains(value, memoryEntryMarkerEnd) {
		return model.ErrInvalidRequest
	}
	return nil
}

func memoryMarkdownDir(baseDir, userID, agentID string) (string, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return "", ErrMemoryMarkdown
	}
	if err := validateMemoryPathID(userID); err != nil {
		return "", err
	}
	if err := validateMemoryPathID(agentID); err != nil {
		return "", err
	}
	return filepath.Join(baseDir, userID, agentID), nil
}

func memoryMarkdownDailyFilename(recordedAt time.Time) (string, error) {
	if recordedAt.IsZero() {
		return "", ErrMemoryMarkdown
	}
	return recordedAt.UTC().Format("2006-01-02") + "_Memory.md", nil
}

func memoryMarkdownDailyPath(baseDir, userID, agentID string, recordedAt time.Time) (string, string, error) {
	dir, err := memoryMarkdownDir(baseDir, userID, agentID)
	if err != nil {
		return "", "", err
	}
	filename, err := memoryMarkdownDailyFilename(recordedAt)
	if err != nil {
		return "", "", err
	}
	return dir, filename, nil
}

func memoryMarkdownContent(item memory.Memory) (string, error) {
	entry, err := memoryMarkdownEntry(item, item.RecordedAt)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`# Memory

UserID: %s
AgentID: %s

%s`, item.UserID, item.AgentID, entry), nil
}

func memoryMarkdownAppendContent(item memory.Memory) (string, error) {
	entry, err := memoryMarkdownEntry(item, item.RecordedAt)
	if err != nil {
		return "", err
	}
	return "\n---\n\n" + entry, nil
}

func memoryMarkdownEntry(item memory.Memory, recordedAt time.Time) (string, error) {
	if err := validateMemoryMarkerID(item.ID); err != nil {
		return "", err
	}
	if recordedAt.IsZero() {
		return "", ErrMemoryMarkdown
	}
	payload := memoryMarkdownPayload(item, recordedAt)
	return fmt.Sprintf("%s%s bytes=%d%s\n%s%s%s%s\n",
		memoryEntryBeginPrefix,
		item.ID,
		len([]byte(payload)),
		memoryEntryMarkerEnd,
		payload,
		memoryEntryEndPrefix,
		item.ID,
		memoryEntryMarkerEnd,
	), nil
}

func memoryMarkdownPayload(item memory.Memory, recordedAt time.Time) string {
	return fmt.Sprintf(`## Memory

MemoryID: %s
RecordedAt: %s

## Question

%s

## Answer

%s
`, item.ID, recordedAt.UTC().Format(time.RFC3339), item.Question, item.Answer)
}

func memoryMarkdownEntriesByID(content string) (map[string]parsedMemoryMarkdownEntry, error) {
	data := []byte(content)
	entries := make(map[string]parsedMemoryMarkdownEntry)
	cursor := 0
	foundMarker := false

	for cursor < len(data) {
		relativeStart := bytes.Index(data[cursor:], []byte(memoryEntryBeginPrefix))
		if relativeStart < 0 {
			break
		}
		foundMarker = true
		beginStart := cursor + relativeStart
		lineEndRelative := bytes.IndexByte(data[beginStart:], '\n')
		if lineEndRelative < 0 {
			return nil, ErrMemoryMarkdown
		}
		lineEnd := beginStart + lineEndRelative
		id, payloadLength, err := parseMemoryEntryBeginLine(string(data[beginStart:lineEnd]))
		if err != nil {
			return nil, errors.Join(ErrMemoryMarkdown, err)
		}
		payloadStart := lineEnd + 1
		payloadEnd := payloadStart + payloadLength
		if payloadLength < 0 || payloadEnd > len(data) {
			return nil, ErrMemoryMarkdown
		}
		payload := string(data[payloadStart:payloadEnd])
		expectedEnd := memoryEntryEndPrefix + id + memoryEntryMarkerEnd
		if !bytes.HasPrefix(data[payloadEnd:], []byte(expectedEnd)) {
			return nil, ErrMemoryMarkdown
		}
		if _, exists := entries[id]; exists {
			return nil, ErrMemoryMarkdown
		}
		payloadID, ok := memoryIDFromMarkdownEntry(payload)
		if !ok || payloadID != id {
			return nil, ErrMemoryMarkdown
		}
		recordedAt, ok := recordedAtFromMarkdownEntry(payload)
		if !ok {
			return nil, ErrMemoryMarkdown
		}
		entries[id] = parsedMemoryMarkdownEntry{
			ID:         id,
			RecordedAt: recordedAt,
			Payload:    payload,
			Content:    strings.TrimSpace(payload),
		}
		cursor = payloadEnd + len(expectedEnd)
	}

	if !foundMarker {
		return nil, ErrMemoryMarkdown
	}
	return entries, nil
}

func parseMemoryEntryBeginLine(line string) (string, int, error) {
	if !strings.HasPrefix(line, memoryEntryBeginPrefix) || !strings.HasSuffix(line, memoryEntryMarkerEnd) {
		return "", 0, ErrMemoryMarkdown
	}
	metadata := strings.TrimSuffix(strings.TrimPrefix(line, memoryEntryBeginPrefix), memoryEntryMarkerEnd)
	separator := strings.LastIndex(metadata, " bytes=")
	if separator < 0 {
		return "", 0, ErrMemoryMarkdown
	}
	id := metadata[:separator]
	if err := validateMemoryMarkerID(id); err != nil {
		return "", 0, err
	}
	payloadLength, err := strconv.Atoi(metadata[separator+len(" bytes="):])
	if err != nil || payloadLength < 0 {
		return "", 0, ErrMemoryMarkdown
	}
	return id, payloadLength, nil
}

func recordedAtFromMarkdownEntry(entry string) (time.Time, bool) {
	for _, line := range strings.Split(entry, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "RecordedAt:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "RecordedAt:"))
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return time.Time{}, false
		}
		return parsed.UTC(), true
	}
	return time.Time{}, false
}

func readMemoryMarkdownEntries(path string) (map[string]parsedMemoryMarkdownEntry, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Join(ErrMemoryMarkdown, err)
	}
	entries, err := memoryMarkdownEntriesByID(string(content))
	if err != nil {
		return nil, errors.Join(ErrMemoryMarkdown, err)
	}
	return entries, nil
}

func findMemoryMarkdownEntry(baseDir, userID, agentID, memoryID string) (parsedMemoryMarkdownEntry, bool, error) {
	dir, err := memoryMarkdownDir(baseDir, userID, agentID)
	if err != nil {
		return parsedMemoryMarkdownEntry{}, false, err
	}
	dirEntries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return parsedMemoryMarkdownEntry{}, false, nil
	}
	if err != nil {
		return parsedMemoryMarkdownEntry{}, false, errors.Join(ErrMemoryMarkdown, err)
	}

	var found parsedMemoryMarkdownEntry
	foundEntry := false
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() || !isDailyMemoryMarkdownFilename(dirEntry.Name()) {
			continue
		}
		entries, err := readMemoryMarkdownEntries(filepath.Join(dir, dirEntry.Name()))
		if err != nil {
			return parsedMemoryMarkdownEntry{}, false, err
		}
		entry, ok := entries[memoryID]
		if !ok {
			continue
		}
		if foundEntry {
			return parsedMemoryMarkdownEntry{}, false, ErrMemoryMarkdown
		}
		found = entry
		foundEntry = true
	}
	return found, foundEntry, nil
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
	return entry.ID == item.ID && entry.Payload == memoryMarkdownPayload(item, entry.RecordedAt)
}

func writeMemoryMarkdown(baseDir string, item memory.Memory) error {
	dir, filename, err := memoryMarkdownDailyPath(baseDir, item.UserID, item.AgentID, item.RecordedAt)
	if err != nil {
		return err
	}
	exists, err := utils.MarkdownFileExists(dir, filename)
	if err != nil {
		return errors.Join(ErrMemoryMarkdown, err)
	}
	if exists {
		if _, err := readMemoryMarkdownEntries(filepath.Join(dir, filename)); err != nil {
			return err
		}
		content, err := memoryMarkdownAppendContent(item)
		if err != nil {
			return err
		}
		if _, err := utils.AppendMarkdownFile(dir, filename, content); err != nil {
			return errors.Join(ErrMemoryMarkdown, err)
		}
		return nil
	}

	content, err := memoryMarkdownContent(item)
	if err != nil {
		return err
	}
	if _, err := utils.CreateMarkdownFile(dir, filename, content); err != nil {
		if errors.Is(err, os.ErrExist) {
			entries, readErr := readMemoryMarkdownEntries(filepath.Join(dir, filename))
			if readErr != nil {
				return readErr
			}
			if _, exists := entries[item.ID]; exists {
				return ErrMemoryMarkdown
			}
			appendContent, renderErr := memoryMarkdownAppendContent(item)
			if renderErr != nil {
				return renderErr
			}
			if _, appendErr := utils.AppendMarkdownFile(dir, filename, appendContent); appendErr != nil {
				return errors.Join(ErrMemoryMarkdown, appendErr)
			}
			return nil
		}
		return errors.Join(ErrMemoryMarkdown, err)
	}
	return nil
}
