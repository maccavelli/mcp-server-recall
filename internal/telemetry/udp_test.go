package telemetry

import (
	"testing"
)

func TestUDPServer(t *testing.T) {
	srv := NewServer()

	// Test GetTelemetryPorts
	ports := GetTelemetryPorts()
	if ports == nil {
		t.Error("expected non-nil ports")
	}

	payload := &MetricPayload{}
	// Test Broadcast when closed
	srv.Broadcast(payload)

	// Test Start/Close
	go srv.Start()

	// Broadcast while running
	srv.Broadcast(payload)

	srv.Close()
}
