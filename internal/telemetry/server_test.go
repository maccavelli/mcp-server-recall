package telemetry

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestNewServer_Binds(t *testing.T) {
	// Should successfully bind to the first available port
	s := NewServer()
	if s == nil {
		t.Fatal("Expected NewServer to return a valid instance, got nil")
	}
	defer s.Close()

	if s.conn == nil {
		t.Fatal("Expected Server.conn to be non-nil")
	}

	// Local addr should match one of the TelemetryPorts
	addr := s.conn.LocalAddr().String()
	found := false
	for _, p := range GetTelemetryPorts() {
		if net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", p)) == addr {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected addr to contain one of the telemetry ports, got: %s", addr)
	}
}

func TestServer_NilSafety(t *testing.T) {
	var s *Server
	// None of these should panic
	s.Start()
	s.Broadcast(nil)
	s.Close()
}

func TestServer_Close(t *testing.T) {
	s := NewServer()
	if s == nil {
		t.Skip("Ports might be busy")
	}

	s.Close()
	// Attempting to write or read should fail on closed conn
	_, err := s.conn.WriteToUDP([]byte("test"), &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345})
	if err == nil {
		t.Error("Expected error writing to closed conn")
	}
}

func TestServer_BroadcastWithoutClient(t *testing.T) {
	s := NewServer()
	if s == nil {
		t.Skip("Ports might be busy")
	}
	defer s.Close()

	// Should not panic or block
	payload := MetricPayloadPool.Get().(*MetricPayload)
	s.Broadcast(payload)
}

func TestServer_BroadcastWithClient(t *testing.T) {
	s := NewServer()
	if s == nil {
		t.Skip("Ports might be busy")
	}
	defer s.Close()

	s.Start()

	// Simulate client binding and pinging
	clientConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()

	_, err = clientConn.WriteToUDP([]byte{0x01}, s.conn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}

	// Wait for ping to be processed
	time.Sleep(50 * time.Millisecond)

	s.dashboardAddrMu.Lock()
	if s.dashboardAddr == nil {
		t.Error("Expected dashboardAddr to be set after ping")
	}
	s.dashboardAddrMu.Unlock()

	// Broadcast
	payload := MetricPayloadPool.Get().(*MetricPayload)
	payload.CPUUsage = 42.5
	s.Broadcast(payload)

	clientConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	buf := make([]byte, 1024)
	n, _, err := clientConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("Failed to read broadcast: %v", err)
	}

	var payloadResp MetricPayload
	if err := json.Unmarshal(buf[:n], &payloadResp); err != nil {
		t.Fatalf("Failed to unmarshal payload: %v", err)
	}

	if payloadResp.CPUUsage != 42.5 {
		t.Errorf("Expected CPUUsage 42.5, got %v", payloadResp.CPUUsage)
	}
}
