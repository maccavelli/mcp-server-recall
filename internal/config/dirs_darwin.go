//go:build darwin || ios

package config

import "os"

func dataDirBase() (string, error) {
	return os.UserConfigDir()
}
