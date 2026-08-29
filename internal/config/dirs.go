package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigDir returns the application-scoped user configuration directory.
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("user config directory is not absolute: %q", base)
	}
	return filepath.Join(base, Name), nil
}

// CacheDir returns the application-scoped user cache directory.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("user cache directory is not absolute: %q", base)
	}
	return filepath.Join(base, Name), nil
}

// DataDir returns the application-scoped user data directory.
func DataDir() (string, error) {
	base, err := dataDirBase()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("user data directory is not absolute: %q", base)
	}
	return filepath.Join(base, Name), nil
}

// DefaultDBPath returns the greenfield default Badger directory.
func DefaultDBPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DefaultDBName), nil
}

// UnsafeDatabasePath reports whether p must not be used as a datastore path.
// Empty, volume roots, and the process working directory are unusable.
func UnsafeDatabasePath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" || p == "." {
		return true
	}
	cleaned := filepath.Clean(p)
	if isVolumeRoot(cleaned) {
		return true
	}
	wd, err := os.Getwd()
	if err != nil {
		return false
	}
	return pathEquiv(cleaned, wd)
}

func isVolumeRoot(p string) bool {
	cleaned := filepath.Clean(p)
	sep := string(os.PathSeparator)
	if cleaned == sep || cleaned == "/" || cleaned == "\\" {
		return true
	}
	vol := filepath.VolumeName(cleaned)
	if vol == "" {
		return false
	}
	return cleaned == vol || cleaned == vol+sep
}

func pathEquiv(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ea, err1 := filepath.EvalSymlinks(a)
	eb, err2 := filepath.EvalSymlinks(b)
	if err1 == nil && err2 == nil {
		return ea == eb
	}
	return false
}

// IsCWDOrParent reports whether p is the working directory or a parent of it.
func IsCWDOrParent(p string) bool {
	wd, err := os.Getwd()
	if err != nil {
		return false
	}
	absP, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	absWD, err := filepath.Abs(wd)
	if err != nil {
		return false
	}
	if pathEquiv(absP, absWD) {
		return true
	}
	rel, err := filepath.Rel(absP, absWD)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, "..")
}
