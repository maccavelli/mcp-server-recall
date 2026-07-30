// Package util provides functionality for the util subsystem.
package util

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WriteFileAtomic safely writes data to a file by writing to a temporary file
// in the same directory and then renaming it. On Windows, it retries on lock errors.
func WriteFileAtomic(filename string, data []byte) error {
	dir := filepath.Dir(filename)
	base := filepath.Base(filename)

	tmpFile, err := os.CreateTemp(dir, base+".*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmpFile.Name()

	// Clean up tmp file in case of panic/early return
	defer func() {
		_ = tmpFile.Close()    //nolint:errcheck // best-effort temp cleanup
		_ = os.Remove(tmpName) //nolint:errcheck // best-effort temp cleanup
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write to temp file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	// Try to mirror permissions and ownership if the target file exists
	if stat, err := os.Stat(filename); err == nil {
		// Mirror mode
		if chmodErr := tmpFile.Chmod(stat.Mode()); chmodErr != nil {
			return fmt.Errorf("failed to mirror temp file mode: %w", chmodErr)
		}
		// Mirror ownership (POSIX only)
		mirrorOwnership(tmpName, stat)
	}

	// We must close the file before renaming on Windows!
	if closeErr := tmpFile.Close(); closeErr != nil {
		return fmt.Errorf("failed to close temp file before rename: %w", closeErr)
	}

	// Atomic rename with retry loop for Windows AV locks
	const maxRetries = 3
	for i := range maxRetries + 1 {
		err = os.Rename(tmpName, filename)
		if err == nil {
			return nil
		}
		if i < maxRetries {
			time.Sleep(time.Duration(100<<i) * time.Millisecond) // 100ms, 200ms, 400ms
		}
	}

	return fmt.Errorf("failed to atomic rename %s to %s after %d retries: %w", tmpName, filename, maxRetries, err)
}
