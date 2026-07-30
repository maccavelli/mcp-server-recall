// Package telemetry provides functionality for the telemetry subsystem.
package telemetry

import (
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// TelemetryPorts are the UDP ports used for dashboard telemetry (serve listens, dashboard connects).
	DefaultTelemetryPorts = []int{49156, 49157, 49158, 49159, 49160}
	// EmissionInterval controls how frequently the serve process pushes metrics to the dashboard.
	EmissionInterval = 500 * time.Millisecond
)

// MetricPayload contains the Hot State telemetry data sent over UDP.
type MetricPayload struct {
	// System Metrics
	MemoryMB   uint64  `json:"memory_mb"`
	Goroutines int     `json:"goroutines"`
	UptimeSec  int64   `json:"uptime_sec"`
	NumGC      uint32  `json:"num_gc"`
	CPUUsage   float64 `json:"cpu_usage"`

	// RPC Analytics
	CacheHits       int64   `json:"cache_hits"`
	CacheMisses     int64   `json:"cache_misses"`
	DBHits          int64   `json:"db_hits"`
	DBMisses        int64   `json:"db_misses"`
	RPCPayloadBytes int64   `json:"rpc_payload_bytes"`
	AvgRPCLatencyMs float64 `json:"avg_rpc_latency_ms"` // Changed to match standard but dashboard receives it fine

	// Network Topology
	StdioConnected bool `json:"stdio_connected"`
	HTTPPort       int  `json:"http_port"`
	TotalClients   int  `json:"total_clients"`

	// Namespace Metrics
	BleveDocs  uint64         `json:"bleve_docs"`
	Namespaces map[string]int `json:"namespaces"`

	// Extended Analytics
	SearchQueries      uint64 `json:"search_queries"`
	CreateOps          uint64 `json:"create_ops"`
	UpdateOps          uint64 `json:"update_ops"`
	MergeOps           uint64 `json:"merge_ops"`
	GCSweeps           uint64 `json:"gc_sweeps"`
	BoundaryViolations uint64 `json:"boundary_violations"`
}

var MetricPayloadPool = sync.Pool{
	New: func() any {
		return &MetricPayload{
			Namespaces: make(map[string]int),
		}
	},
}

// Server handles the UDP broadcast of telemetry data to the dashboard.
type Server struct {
	conn            *net.UDPConn
	dashboardAddr   *net.UDPAddr
	dashboardAddrMu sync.Mutex
	ch              chan *MetricPayload
	done            chan struct{}
}

// NewServer initializes the UDP listener on the first available port.
func NewServer() *Server {
	var conn *net.UDPConn
	for _, port := range GetTelemetryPorts() {
		addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}
		c, err := net.ListenUDP("udp", addr)
		if err == nil {
			conn = c
			slog.Info("telemetry udp listener bound", "port", port)
			break
		}
		slog.Warn("telemetry port unavailable", "port", port, "error", err)
	}

	if conn == nil {
		slog.Warn("all telemetry ports exhausted; starting without dashboard emission")
		return nil
	}

	return &Server{
		conn: conn,
		ch:   make(chan *MetricPayload, 100),
		done: make(chan struct{}),
	}
}

// Start begins listening for dashboard pings to register the client address.
func (s *Server) Start() {
	if s == nil || s.conn == nil {
		return
	}

	// Goroutine to receive pings from the dashboard
	go func() {
		buf := make([]byte, 64)
		for {
			_, remoteAddr, err := s.conn.ReadFromUDP(buf)
			if err != nil {
				if strings.Contains(err.Error(), "use of closed") {
					return
				}
				continue
			}
			s.dashboardAddrMu.Lock()
			s.dashboardAddr = remoteAddr
			s.dashboardAddrMu.Unlock()
		}
	}()

	// Outbound broadcast processor loop
	go func() {
		for {
			select {
			case <-s.done:
				return
			case payload, ok := <-s.ch:
				if !ok {
					return
				}
				s.dashboardAddrMu.Lock()
				target := s.dashboardAddr
				s.dashboardAddrMu.Unlock()

				if target != nil {
					data, err := json.Marshal(payload)
					if err == nil {
						if _, writeErr := s.conn.WriteToUDP(data, target); writeErr != nil {
							slog.Debug("failed to write telemetry UDP payload", "error", writeErr)
						}
					}
				}
				// Clear namespaces map to reuse
				for k := range payload.Namespaces {
					delete(payload.Namespaces, k)
				}
				MetricPayloadPool.Put(payload)
			}
		}
	}()
}

// Broadcast sends the MetricPayload to the connected dashboard if debouncing allows it.
// It is non-blocking.
func (s *Server) Broadcast(payload *MetricPayload) {
	if s == nil || s.conn == nil || s.ch == nil {
		return
	}

	select {
	case s.ch <- payload:
	default:
		if payload != nil {
			for k := range payload.Namespaces {
				delete(payload.Namespaces, k)
			}
			MetricPayloadPool.Put(payload)
		}
	}
}

// Close gracefully shuts down the UDP listener.
func (s *Server) Close() {
	if s == nil {
		return
	}
	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			slog.Debug("failed to close telemetry UDP socket", "error", err)
		}
	}
	if s.done != nil {
		close(s.done)
	}
}

// GetTelemetryPorts parses the environment variable or falls back to the server's specific default ports.
func GetTelemetryPorts() []int {
	env := os.Getenv("MCP_TELEMETRY_UDP_PORTS")
	if env == "" {
		return DefaultTelemetryPorts
	}

	var ports []int
	for part := range strings.SplitSeq(env, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			bounds := strings.Split(part, "-")
			if len(bounds) == 2 {
				start, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
				end, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
				if err1 == nil && err2 == nil && start <= end {
					for i := start; i <= end; i++ {
						ports = append(ports, i)
					}
				}
			}
		} else {
			if port, err := strconv.Atoi(part); err == nil {
				ports = append(ports, port)
			}
		}
	}

	if len(ports) == 0 {
		return DefaultTelemetryPorts // Fallback on malformed environment variable
	}
	return ports
}
