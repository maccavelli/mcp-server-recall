//go:build windows

package config

import "os"

func dataDirBase() (string, error) {
	return os.UserCacheDir()
}
