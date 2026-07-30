// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/maccavelli/mcp-server-recall/internal/telemetry"
	"github.com/maccavelli/mcp-server-recall/internal/ui"
)

var dashCmd = &cobra.Command{
	Use:   "dash",
	Short: "Launch the observability dashboard",
	Run: func(cmd *cobra.Command, args []string) {
		if err := ui.EnableVirtualTerminalProcessing(); err != nil {
			slog.Warn("failed to enable virtual terminal processing", "error", err)
		}
		runInteractiveDashboard()
	},
}

func init() {
	RootCmd.AddCommand(dashCmd)
}

// coldMetricsMsg carries the latest BuntDB snapshot.
type coldMetricsMsg struct {
	Snapshot map[string]any
	Logs     []TelemetryLog
}

// udpMetricsMsg carries the latest UDP telemetry.
type udpMetricsMsg telemetry.MetricPayload

type model struct {
	activeTab     int
	coldMetrics   coldMetricsMsg
	hotState      udpMetricsMsg
	boundPort     int
	hotConnected  bool
	hotLastUpdate time.Time
}

const (
	tabOverview = iota
	tabMemoryGC
	tabSearchEngine
	tabTaxonomyAST
	tabRPCAnalytics
	tabNetwork
	tabSecurity
	tabConfig
	tabQuit
)

var navItems = []string{
	"Summary",
	"Memory Consolidation & GC",
	"Semantic Search Engine",
	"Taxonomy & AST Pipeline",
	"RPC & Gateway Analytics",
	"Network Topology",
	"Security & Cryptography",
	"Config & Environment",
	"Quit",
}

// --- Lipgloss Styles ---

var (
	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 2).
			Width(34)

	navItemStyle = lipgloss.NewStyle().
			Padding(0, 1)

	activeNavItemStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("62")).
				Foreground(lipgloss.Color("230")).
				Padding(0, 1).
				Bold(true)

	windowStyle = lipgloss.NewStyle().
			Padding(1, 4)

	dashTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("62")).
			Bold(true).
			MarginBottom(1)
)

func runInteractiveDashboard() {
	m := model{}
	p := tea.NewProgram(m, tea.WithAltScreen())

	// Start BuntDB polling goroutine
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		var dbMu sync.Mutex

		// Initial load
		snapshot, logs, snapErr := ReadDashboardSnapshot()
		if snapErr != nil {
			slog.Warn("failed to read dashboard snapshot", "error", snapErr)
		}
		p.Send(coldMetricsMsg{Snapshot: snapshot, Logs: logs})

		for range ticker.C {
			if dbMu.TryLock() {
				go func() {
					defer dbMu.Unlock()
					snapshot, logs, snapErr := ReadDashboardSnapshot()
					if snapErr != nil {
						slog.Warn("failed to read dashboard snapshot", "error", snapErr)
					}
					p.Send(coldMetricsMsg{Snapshot: snapshot, Logs: logs})
				}()
			}
		}
	}()

	// Start UDP Client / Watchdog
	go runUDPClient(p)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running dashboard: %v\n", err)
		os.Exit(1)
	}
}

// isClosedErr checks if the error indicates a closed socket.
func isClosedErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "use of closed")
}

// reconnectMsg notifies the BubbleTea program of a port change.
type reconnectMsg struct {
	port int
}

// udpDialAndValidate connects to a port and verifies the server responds.
func udpDialAndValidate(port int) *net.UDPConn {
	addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}
	c, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil
	}
	if _, writeErr := c.Write([]byte{0x01}); writeErr != nil {
		_ = c.Close() //nolint:errcheck // probe cleanup after failed write
		return nil
	}
	if err := c.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
		_ = c.Close() //nolint:errcheck // probe cleanup after deadline failure
		return nil
	}
	buf := make([]byte, 4096)
	_, err = c.Read(buf)
	if err != nil {
		_ = c.Close() //nolint:errcheck // probe cleanup after failed read
		return nil
	}
	return c
}

// udpSweepPorts attempts to connect to the first responding telemetry port.
func udpSweepPorts() (*net.UDPConn, int) {
	for _, port := range telemetry.GetTelemetryPorts() {
		if c := udpDialAndValidate(port); c != nil {
			return c, port
		}
	}
	return nil, 0
}

func runUDPClient(p *tea.Program) {
	conn, boundPort := udpSweepPorts()
	if conn == nil {
		slog.Warn("could not connect to any telemetry port; will retry")
	} else {
		p.Send(reconnectMsg{port: boundPort})
	}

	buf := make([]byte, 4096)
	pingTicker := time.NewTicker(telemetry.EmissionInterval)
	defer pingTicker.Stop()

	const maxConsecutiveFailures = 6
	consecutiveFailures := 0
	backoff := 2 * time.Second
	const maxBackoff = 10 * time.Second

	for range pingTicker.C {
		if conn == nil {
			time.Sleep(backoff)
			conn, boundPort = udpSweepPorts()
			if conn != nil {
				consecutiveFailures = 0
				backoff = 2 * time.Second
				p.Send(reconnectMsg{port: boundPort})
			} else {
				backoff = min(backoff*2, maxBackoff)
			}
			continue
		}

		_, err := conn.Write([]byte{0x01})
		if err != nil {
			if isClosedErr(err) {
				return
			}
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveFailures {
				_ = conn.Close() //nolint:errcheck // best-effort reconnect after repeated write failures
				conn = nil
			}
			continue
		}

		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			consecutiveFailures++
			continue
		}
		n, err := conn.Read(buf)
		if err != nil {
			if isClosedErr(err) {
				return
			}
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveFailures {
				_ = conn.Close() //nolint:errcheck // best-effort reconnect after repeated read failures
				conn = nil
			}
			continue
		}

		consecutiveFailures = 0
		var payload telemetry.MetricPayload
		if json.Unmarshal(buf[:n], &payload) == nil {
			p.Send(udpMetricsMsg(payload))
		}
	}
}

// Init performs the Init operation.
func (m model) Init() tea.Cmd {
	return nil
}

// Update performs the Update operation.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q": //nolint:goconst // bypass
			return m, tea.Quit
		case "up", "k":
			m.activeTab--
			if m.activeTab < 0 {
				m.activeTab = len(navItems) - 1
			}
		case "down", "j": //nolint:goconst // bypass
			m.activeTab++
			if m.activeTab >= len(navItems) {
				m.activeTab = 0
			}
		case "enter": //nolint:goconst // bypass
			if m.activeTab == tabQuit {
				return m, tea.Quit
			}
		}
	case coldMetricsMsg:
		m.coldMetrics = msg
	case udpMetricsMsg:
		m.hotState = msg
		m.hotConnected = true
		m.hotLastUpdate = time.Now()
	case reconnectMsg:
		m.boundPort = msg.port
	}
	return m, nil
}

// View performs the View operation.
func (m model) View() string {
	var navLines []string
	navLines = append(navLines, dashTitleStyle.Render("Recall Dashboard"), "")

	for i, item := range navItems {
		if i == m.activeTab {
			navLines = append(navLines, activeNavItemStyle.Render("> "+item))
		} else {
			navLines = append(navLines, navItemStyle.Render("  "+item))
		}
	}

	sidebar := sidebarStyle.Render(strings.Join(navLines, "\n"))

	var content string
	switch m.activeTab {
	case tabOverview:
		content = renderOverview(m)
	case tabMemoryGC:
		content = renderMemoryGC(m)
	case tabSearchEngine:
		content = renderSearchEngine(m)
	case tabTaxonomyAST:
		content = renderTaxonomyAST(m)
	case tabRPCAnalytics:
		content = renderRPCAnalytics(m)
	case tabNetwork:
		content = renderNetwork(m)
	case tabSecurity:
		content = renderSecurity(m)
	case tabConfig:
		content = renderConfigTab(m)
	case tabQuit:
		content = dashTitleStyle.Render("Quit") + "\n\nPress Enter to exit the dashboard."
	}

	mainView := windowStyle.Render(content)

	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, mainView)
}
