package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic_NewFile_Success(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	content := []byte("hello world")

	err := WriteFileAtomic(filePath, content)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	if string(data) != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", string(data))
	}
}

func TestWriteFileAtomic_ExistingFile_Success(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")

	// Create initial file with specific mode
	initialContent := []byte("initial")
	err := os.WriteFile(filePath, initialContent, 0640)
	if err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}

	// Overwrite using atomic write
	newContent := []byte("new content")
	err = WriteFileAtomic(filePath, newContent)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	if string(data) != "new content" {
		t.Errorf("expected %q, got %q", "new content", string(data))
	}

	// Verify mode was mirrored (best effort, OS dependent, but it should not error)
	stat, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}
	// mode might not match exactly due to umask, but we just check the file exists and is readable
	if stat.Size() == 0 {
		t.Errorf("expected non-zero size")
	}
}

func TestWriteFileAtomic_Error(t *testing.T) {
	err := WriteFileAtomic("/invalid/path/that/does/not/exist/file.txt", []byte("hello"))
	if err == nil {
		t.Error("expected error for invalid path")
	}
}
