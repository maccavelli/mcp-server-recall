// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TelemetryLog defines the TelemetryLog structure.
type TelemetryLog struct {
	Time  string `json:"time"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Pkg   string `json:"pkg"`
	Tool  string `json:"tool"`
}

// ReadDashboardSnapshot performs the ReadDashboardSnapshot operation.
func ReadDashboardSnapshot() (map[string]any, []TelemetryLog, error) {
	path := filepath.Join(Cfg.GetDBPath(), "telemetry.ring")
	b, readErr := os.ReadFile(path) //nolint:gosec // path is derived from configured DB directory
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return make(map[string]any), []TelemetryLog{{Msg: "Awaiting daemon telemetry sync..."}}, nil
		}
		return nil, nil, fmt.Errorf("read telemetry ring: %w", readErr)
	}

	var snapshot map[string]any

	var logs []TelemetryLog
	lines := strings.Split(string(b), "\n")
	if len(lines) > 0 {
		firstLine := strings.TrimSpace(lines[0])
		if firstLine != "" && strings.HasPrefix(firstLine, "{") {
			if err := json.Unmarshal([]byte(firstLine), &snapshot); err != nil {
				return nil, nil, fmt.Errorf("parse telemetry snapshot: %w", err)
			}
		}

		for i := 1; i < len(lines); i++ {
			l := strings.TrimSpace(lines[i])
			if l != "" {
				var tl TelemetryLog
				if err := json.Unmarshal([]byte(l), &tl); err == nil {
					logs = append(logs, tl)
				}
			}
		}
	}

	if snapshot == nil {
		snapshot = make(map[string]any)
	}
	return snapshot, logs, nil
}
