package main

import (
	"errors"
	"net"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestIsClosedErr(t *testing.T) {
	if isClosedErr(nil) {
		t.Error("expected nil to return false")
	}

	err := errors.New("use of closed network connection")
	if !isClosedErr(err) {
		t.Error("expected 'use of closed...' to return true")
	}

	err = errors.New("other error")
	if isClosedErr(err) {
		t.Error("expected other error to return false")
	}

	netErr := net.ErrClosed
	if !isClosedErr(netErr) {
		// net.ErrClosed produces "use of closed network connection" error string
		t.Error("expected net.ErrClosed to return true")
	}
}

func TestRenderFunctions(t *testing.T) {
	m := model{}

	// Ensure these do not panic and return string
	if out := renderRPCAnalytics(m); out == "" {
		t.Error("expected non-empty output")
	}

	if out := renderNetwork(m); out == "" {
		t.Error("expected non-empty output")
	}

	if out := renderMemoryGC(m); out == "" {
		t.Error("expected non-empty output")
	}

	if out := renderOverview(m); out == "" {
		t.Error("expected non-empty output")
	}
}

func TestDashboardUpdate(t *testing.T) {
	m := model{}

	// Test basic updates that won't panic
	_, _ = m.Update("test msg")

	// Key messages
	keys := []string{"q", "ctrl+c", "up", "k", "down", "j", "enter"}
	for _, k := range keys {
		km := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		switch k {
		case "enter":
			km.Type = tea.KeyEnter
		case "up":
			km.Type = tea.KeyUp
		case "down":
			km.Type = tea.KeyDown
		case "ctrl+c":
			km.Type = tea.KeyCtrlC
		}

		_, _ = m.Update(km)
	}

	// Test other messages
	_, _ = m.Update(coldMetricsMsg{})
	_, _ = m.Update(udpMetricsMsg{})
	_, _ = m.Update(reconnectMsg{port: 1234})
}

func TestDashboardUDP(t *testing.T) {
	// Test dial on an invalid port (should fail gracefully)
	_ = udpDialAndValidate(99999)

	// Test runUDPClient which shouldn't block indefinitely on failure
	p := tea.NewProgram(model{})
	go runUDPClient(p)

	// Test sweeping
	udpSweepPorts()
}
