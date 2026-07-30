package telemetry

import (
	"net"
	"os"
	"testing"
)

func TestGetTelemetryPorts(t *testing.T) {
	// Test default
	os.Setenv("MCP_TELEMETRY_UDP_PORTS", "")
	ports := GetTelemetryPorts()
	if len(ports) == 0 {
		t.Fatal("expected default ports")
	}

	// Test ranges and single values
	os.Setenv("MCP_TELEMETRY_UDP_PORTS", "4000-4002, 4010, invalid")
	ports = GetTelemetryPorts()
	if len(ports) == 0 {
		t.Fatal("expected parsed ports")
	}

	// Also test Broadcast with non-nil server but full channel
	s := &Server{
		conn: &net.UDPConn{},
		ch:   make(chan *MetricPayload, 1),
	}
	s.ch <- &MetricPayload{}
	s.Broadcast(&MetricPayload{Namespaces: map[string]int{"test": 1}})
}

func TestBroadcast(t *testing.T) {
	var s *Server
	s.Broadcast(nil)
}
