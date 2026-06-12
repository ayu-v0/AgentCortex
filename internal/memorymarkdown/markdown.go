package memorymarkdown

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
	"github.com/ayu-v0/agent-cortex/internal/utils"
)

const (
	entryBeginPrefix = "<!-- MemoryEntry:BEGIN id="
	entryEndPrefix   = "<!-- MemoryEntry:END id="
	entryMarkerEnd   = " -->"
)

var ErrMarkdown = errors.New("memory markdown error")

type Entry struct {
	ID         string
	RecordedAt time.Time
	Payload    string
	Content    string
}

func DailyPath(baseDir, userID, agentID string, recordedAt time.Time) (string, error) {
	dir, err := Dir(baseDir, userID, agentID)
	if err != nil {
		return "", err
	}
	filename, err := DailyFilename(recordedAt)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filename), nil
}

func Dir(baseDir, userID, agentID string) (string, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return "", ErrMarkdown
	}
	if err := validatePathID(userID); err != nil {
		return "", err
	}
	if err := validatePathID(agentID); err != nil {
		return "", err
	}
	return filepath.Join(baseDir, userID, agentID), nil
}

func DailyFilename(recordedAt time.Time) (string, error) {
	if recordedAt.IsZero() {
		return "", ErrMarkdown
	}
	return recordedAt.UTC().Format("2006-01-02") + "_Memory.md", nil
}

func ReadEntries(path string) (map[string]Entry, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Join(ErrMarkdown, err)
	}
	entries, err := EntriesByID(string(content))
	if err != nil {
		return nil, errors.Join(ErrMarkdown, err)
	}
	return entries, nil
}

func ReadEntry(baseDir string, metadata memory.Metadata) (Entry, bool, error) {
	path, err := DailyPath(baseDir, metadata.UserID, metadata.AgentID, metadata.RecordedAt)
	if err != nil {
		return Entry{}, false, err
	}
	entries, err := ReadEntries(path)
	if err != nil {
		return Entry{}, false, err
	}
	entry, ok := entries[metadata.ID]
	return entry, ok, nil
}

func FindEntry(baseDir, userID, agentID, memoryID string) (Entry, bool, error) {
	dir, err := Dir(baseDir, userID, agentID)
	if err != nil {
		return Entry{}, false, err
	}
	dirEntries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, errors.Join(ErrMarkdown, err)
	}

	var found Entry
	foundEntry := false
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() || !isDailyFilename(dirEntry.Name()) {
			continue
		}
		entries, err := ReadEntries(filepath.Join(dir, dirEntry.Name()))
		if err != nil {
			return Entry{}, false, err
		}
		entry, ok := entries[memoryID]
		if !ok {
			continue
		}
		if foundEntry {
			return Entry{}, false, ErrMarkdown
		}
		found = entry
		foundEntry = true
	}
	return found, foundEntry, nil
}

func Write(baseDir string, item memory.Memory) error {
	dir, err := Dir(baseDir, item.UserID, item.AgentID)
	if err != nil {
		return err
	}
	filename, err := DailyFilename(item.RecordedAt)
	if err != nil {
		return err
	}
	exists, err := utils.MarkdownFileExists(dir, filename)
	if err != nil {
		return errors.Join(ErrMarkdown, err)
	}
	if exists {
		if _, err := ReadEntries(filepath.Join(dir, filename)); err != nil {
			return err
		}
		content, err := appendContent(item)
		if err != nil {
			return err
		}
		if _, err := utils.AppendMarkdownFile(dir, filename, content); err != nil {
			return errors.Join(ErrMarkdown, err)
		}
		return nil
	}

	content, err := fileContent(item)
	if err != nil {
		return err
	}
	if _, err := utils.CreateMarkdownFile(dir, filename, content); err != nil {
		if errors.Is(err, os.ErrExist) {
			entries, readErr := ReadEntries(filepath.Join(dir, filename))
			if readErr != nil {
				return readErr
			}
			if _, exists := entries[item.ID]; exists {
				return ErrMarkdown
			}
			appendContent, renderErr := appendContent(item)
			if renderErr != nil {
				return renderErr
			}
			if _, appendErr := utils.AppendMarkdownFile(dir, filename, appendContent); appendErr != nil {
				return errors.Join(ErrMarkdown, appendErr)
			}
			return nil
		}
		return errors.Join(ErrMarkdown, err)
	}
	return nil
}

func EntriesByID(content string) (map[string]Entry, error) {
	data := []byte(content)
	entries := make(map[string]Entry)
	cursor := 0
	foundMarker := false

	for cursor < len(data) {
		relativeStart := bytes.Index(data[cursor:], []byte(entryBeginPrefix))
		if relativeStart < 0 {
			break
		}
		foundMarker = true
		beginStart := cursor + relativeStart
		lineEndRelative := bytes.IndexByte(data[beginStart:], '\n')
		if lineEndRelative < 0 {
			return nil, ErrMarkdown
		}
		lineEnd := beginStart + lineEndRelative
		id, payloadLength, err := parseBeginLine(string(data[beginStart:lineEnd]))
		if err != nil {
			return nil, errors.Join(ErrMarkdown, err)
		}
		payloadStart := lineEnd + 1
		payloadEnd := payloadStart + payloadLength
		if payloadLength < 0 || payloadEnd > len(data) {
			return nil, ErrMarkdown
		}
		payload := string(data[payloadStart:payloadEnd])
		expectedEnd := entryEndPrefix + id + entryMarkerEnd
		if !bytes.HasPrefix(data[payloadEnd:], []byte(expectedEnd)) {
			return nil, ErrMarkdown
		}
		if _, exists := entries[id]; exists {
			return nil, ErrMarkdown
		}
		payloadID, ok := idFromEntry(payload)
		if !ok || payloadID != id {
			return nil, ErrMarkdown
		}
		recordedAt, ok := recordedAtFromEntry(payload)
		if !ok {
			return nil, ErrMarkdown
		}
		entries[id] = Entry{
			ID:         id,
			RecordedAt: recordedAt,
			Payload:    payload,
			Content:    strings.TrimSpace(payload),
		}
		cursor = payloadEnd + len(expectedEnd)
	}

	if !foundMarker {
		return nil, ErrMarkdown
	}
	return entries, nil
}

func EntryMatchesMemory(entry Entry, item memory.Memory) bool {
	return entry.ID == item.ID && entry.Payload == payload(item, entry.RecordedAt)
}

func FileContent(item memory.Memory) (string, error) {
	return fileContent(item)
}

func AppendContent(item memory.Memory) (string, error) {
	return appendContent(item)
}

func RenderEntry(item memory.Memory, recordedAt time.Time) (string, error) {
	return renderEntry(item, recordedAt)
}

func Payload(item memory.Memory, recordedAt time.Time) string {
	return payload(item, recordedAt)
}

func fileContent(item memory.Memory) (string, error) {
	entry, err := renderEntry(item, item.RecordedAt)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`# Memory

UserID: %s
AgentID: %s

%s`, item.UserID, item.AgentID, entry), nil
}

func appendContent(item memory.Memory) (string, error) {
	entry, err := renderEntry(item, item.RecordedAt)
	if err != nil {
		return "", err
	}
	return "\n---\n\n" + entry, nil
}

func renderEntry(item memory.Memory, recordedAt time.Time) (string, error) {
	if err := validateMarkerID(item.ID); err != nil {
		return "", err
	}
	if recordedAt.IsZero() {
		return "", ErrMarkdown
	}
	payload := payload(item, recordedAt)
	return fmt.Sprintf("%s%s bytes=%d%s\n%s%s%s%s\n",
		entryBeginPrefix,
		item.ID,
		len([]byte(payload)),
		entryMarkerEnd,
		payload,
		entryEndPrefix,
		item.ID,
		entryMarkerEnd,
	), nil
}

func payload(item memory.Memory, recordedAt time.Time) string {
	return fmt.Sprintf(`## Memory

MemoryID: %s
RecordedAt: %s

## Question

%s

## Answer

%s
`, item.ID, recordedAt.UTC().Format(time.RFC3339), item.Question, item.Answer)
}

func parseBeginLine(line string) (string, int, error) {
	if !strings.HasPrefix(line, entryBeginPrefix) || !strings.HasSuffix(line, entryMarkerEnd) {
		return "", 0, ErrMarkdown
	}
	metadata := strings.TrimSuffix(strings.TrimPrefix(line, entryBeginPrefix), entryMarkerEnd)
	separator := strings.LastIndex(metadata, " bytes=")
	if separator < 0 {
		return "", 0, ErrMarkdown
	}
	id := metadata[:separator]
	if err := validateMarkerID(id); err != nil {
		return "", 0, err
	}
	payloadLength, err := strconv.Atoi(metadata[separator+len(" bytes="):])
	if err != nil || payloadLength < 0 {
		return "", 0, ErrMarkdown
	}
	return id, payloadLength, nil
}

func recordedAtFromEntry(entry string) (time.Time, bool) {
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

func idFromEntry(entry string) (string, bool) {
	for _, line := range strings.Split(entry, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "MemoryID:") {
			id := strings.TrimSpace(strings.TrimPrefix(line, "MemoryID:"))
			return id, id != ""
		}
	}
	return "", false
}

func isDailyFilename(filename string) bool {
	const suffix = "_Memory.md"
	if !strings.HasSuffix(filename, suffix) {
		return false
	}
	datePart := strings.TrimSuffix(filename, suffix)
	_, err := time.Parse("2006-01-02", datePart)
	return err == nil
}

func validatePathID(value string) error {
	if value == "" || value != strings.TrimSpace(value) {
		return ErrMarkdown
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' {
			continue
		}
		return ErrMarkdown
	}
	return nil
}

func validateMarkerID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") || strings.Contains(value, entryMarkerEnd) {
		return ErrMarkdown
	}
	return nil
}
