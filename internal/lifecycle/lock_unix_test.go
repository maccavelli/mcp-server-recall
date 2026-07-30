//go:build !windows

package lifecycle

import (
	"path/filepath"
	"testing"
)

func TestTryLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	// 1. Acquire lock successfully
	l1, err := TryLock(lockPath)
	if err != nil {
		t.Fatalf("expected to acquire lock, got error: %v", err)
	}
	if l1 == nil {
		t.Fatal("expected non-nil lock closer")
	}

	// 2. Attempt to acquire same lock (should fail)
	l2, err := TryLock(lockPath)
	if err == nil {
		if l2 != nil {
			_ = l2.Close()
		}
		t.Fatal("expected error acquiring already held lock")
	}

	// 3. Close first lock
	err = l1.Close()
	if err != nil {
		t.Fatalf("expected to close lock without error, got: %v", err)
	}

	// 4. Acquire lock again after it was closed
	l3, err := TryLock(lockPath)
	if err != nil {
		t.Fatalf("expected to acquire lock after close, got error: %v", err)
	}
	if l3 != nil {
		_ = l3.Close()
	}

	// 5. Attempt invalid path
	l4, err := TryLock("/non/existent/path/for/lock/12345/test.lock")
	if err == nil {
		if l4 != nil {
			_ = l4.Close()
		}
		t.Fatal("expected error on invalid path")
	}
}
