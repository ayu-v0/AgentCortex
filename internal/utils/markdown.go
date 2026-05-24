package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MarkdownFileExists reports whether a markdown file exists directly under dir.
func MarkdownFileExists(dir, filename string) (bool, error) {
	path, err := markdownFilePath(dir, filename)
	if err != nil {
		return false, err
	}

	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// CreateMarkdownFile creates a markdown file directly under dir and writes content.
func CreateMarkdownFile(dir, filename, content string) (string, error) {
	path, err := markdownFilePath(dir, filename)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		return "", err
	}
	return path, nil
}

// AppendMarkdownFile appends content to an existing markdown file directly under dir.
func AppendMarkdownFile(dir, filename, content string) (string, error) {
	path, err := markdownFilePath(dir, filename)
	if err != nil {
		return "", err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := file.WriteString(content); err != nil {
		return "", err
	}
	return path, nil
}

func markdownFilePath(dir, filename string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("markdown directory is required")
	}

	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "", fmt.Errorf("markdown filename is required")
	}
	if filepath.Base(filename) != filename {
		return "", fmt.Errorf("markdown filename must not include path separators")
	}

	extension := strings.ToLower(filepath.Ext(filename))
	if extension != ".md" && extension != ".markdown" {
		return "", fmt.Errorf("markdown filename must end with .md or .markdown")
	}

	return filepath.Join(dir, filename), nil
}
