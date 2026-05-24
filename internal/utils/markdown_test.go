package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMarkdownFileExistsReturnsTrueForMarkdownFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("# Note"), 0o644); err != nil {
		t.Fatalf("write markdown file: %v", err)
	}

	exists, err := MarkdownFileExists(dir, "note.md")
	if err != nil {
		t.Fatalf("markdown file exists: %v", err)
	}
	if !exists {
		t.Fatal("expected markdown file to exist")
	}
}

func TestMarkdownFileExistsReturnsFalseForMissingFile(t *testing.T) {
	exists, err := MarkdownFileExists(t.TempDir(), "missing.md")
	if err != nil {
		t.Fatalf("markdown file exists: %v", err)
	}
	if exists {
		t.Fatal("expected missing markdown file")
	}
}

func TestCreateMarkdownFileCreatesFileWithContent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "docs")

	path, err := CreateMarkdownFile(dir, "note.md", "# Note\n")
	if err != nil {
		t.Fatalf("create markdown file: %v", err)
	}
	if path != filepath.Join(dir, "note.md") {
		t.Fatalf("expected created path under dir, got %q", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created markdown file: %v", err)
	}
	if string(content) != "# Note\n" {
		t.Fatalf("expected markdown content, got %q", string(content))
	}
}

func TestCreateMarkdownFileRejectsNonMarkdownFile(t *testing.T) {
	if _, err := CreateMarkdownFile(t.TempDir(), "note.txt", ""); err == nil {
		t.Fatal("expected non-markdown filename to be rejected")
	}
}

func TestCreateMarkdownFileDoesNotOverwriteExistingFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := CreateMarkdownFile(dir, "note.md", "first"); err != nil {
		t.Fatalf("create initial markdown file: %v", err)
	}

	if _, err := CreateMarkdownFile(dir, "note.md", "second"); err == nil {
		t.Fatal("expected existing markdown file to be rejected")
	}

	content, err := os.ReadFile(filepath.Join(dir, "note.md"))
	if err != nil {
		t.Fatalf("read markdown file: %v", err)
	}
	if string(content) != "first" {
		t.Fatalf("expected original content to remain, got %q", string(content))
	}
}
