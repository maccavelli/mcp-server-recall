// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/maccavelli/mcplib"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcp-server-recall/internal/lifecycle"
	"github.com/maccavelli/mcp-server-recall/internal/memory"
	"github.com/maccavelli/mcp-server-recall/internal/search"
	"github.com/maccavelli/mcp-server-recall/internal/server"
	"github.com/maccavelli/mcp-server-recall/internal/telemetry"
	"github.com/maccavelli/mcp-server-recall/internal/util"
)

// ClientRegistry tracks actively connected HTTP Streamable API clients.
type ClientRegistry struct {
	mu      sync.RWMutex
	clients map[string]string // sessionID → clientName
}

// Register records a client connection by session ID and client name.
func (cr *ClientRegistry) Register(sessionID, clientName string) {
	cr.mu.Lock()
	cr.clients[sessionID] = clientName
	cr.mu.Unlock()
}

// GetClients returns a snapshot copy of the active HTTP client sessions.
func (cr *ClientRegistry) GetClients() map[string]string {
	cr.mu.RLock()
	defer cr.mu.RUnlock()
	out := make(map[string]string, len(cr.clients))
	maps.Copy(out, cr.clients)
	return out
}

// Unregister removes a client by session ID.
func (cr *ClientRegistry) Unregister(sessionID string) {
	cr.mu.Lock()
	delete(cr.clients, sessionID)
	cr.mu.Unlock()
}

var httpClients = &ClientRegistry{clients: make(map[string]string)}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Boots the JSON-RPC daemon (Proxy Sub-server)",
	RunE: func(cmd *cobra.Command, args []string) error {
		logBuffer := mcplib.NewLogBuffer()
		cleanupLogs := mcplib.SetupStandardLogging("recall", logBuffer)
		defer cleanupLogs()

		slog.Info("[BACKPLANE] SPAWN "+config.Name,
			"version", Version,
			"execution_mode", "daemon",
			"pid", os.Getpid(),
		)

		// Implement process singleton lock utilizing lifecycle package.
		lockPath := filepath.Join(os.TempDir(), config.Name+".lock")
		lock, err := lifecycle.TryLock(lockPath)
		if err != nil {
			return fmt.Errorf("another instance is running (failed to acquire lock): %w", err)
		}
		defer func() {
			if closeErr := lock.Close(); closeErr != nil {
				slog.Warn("failed to release process lock", "error", closeErr)
			}
		}()

		// 🛡️ FIX: Previously only caught os.Interrupt (SIGINT). When the orchestrator
		// sends SIGTERM via killProcessGroup(), recall exited via kernel default handler
		// — bypassing all defer chains (store.Close, bleve index flush, etc.).
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		pipeline := mcplib.NewStdioPipeline(os.Stdin, RealStdout, stop)

		if err := runServe(ctx, logBuffer, pipeline.Reader, pipeline.Writer); err != nil {
			if mcplib.IsExpectedShutdownErr(err) {
				slog.Info("server shut down gracefully", "error", err)
				if flushErr := pipeline.Flush(); flushErr != nil {
					slog.Warn("pipeline flush on shutdown failed", "error", flushErr)
				}
				return nil
			}
			return fmt.Errorf("server fatal error: %w", err)
		}

		if flushErr := pipeline.Flush(); flushErr != nil {
			slog.Warn("pipeline flush on exit failed", "error", flushErr)
		}
		return nil
	},
}

func init() {
	RootCmd.AddCommand(serveCmd)
}

func runServe(ctx context.Context, logs *mcplib.LogBuffer, reader io.ReadCloser, writer io.WriteCloser) error {
	bootStart := time.Now()
	dbPath := Cfg.GetDBPath()
	if dbPath == "" || config.UnsafeDatabasePath(dbPath) {
		return fmt.Errorf("failed to initialize storage: database path is not resolvable")
	}
	store, err := memory.NewMemoryStore(ctx, dbPath, Cfg.EncryptionKey(), Cfg.SearchLimit(), Cfg.BatchSettings())
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			slog.Warn("failed to close memory store", "error", closeErr)
		}
	}()

	go func(ctx context.Context) {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m := store.GetMetrics()
				args := []any{
					"hits", m.Hits,
					"misses", m.Misses,
					"db_hits", m.DBHits, //nolint:goconst // bypass
					"db_misses", m.DBMisses, //nolint:goconst // bypass
					"entries", m.Entries,
					"bleve_docs", m.BleveDocs,
				}
				for k, v := range m.Namespaces {
					args = append(args, k, v)
				}
				slog.Info("cache metrics", args...)
			}
		}
	}(ctx)

	var engine *search.BleveEngine
	if Cfg.SearchEnabled() {
		indexPath := filepath.Join(Cfg.GetDBPath(), "search_index")
		var engineErr error
		engine, engineErr = search.InitStorage(indexPath)
		if engineErr != nil {
			slog.Error("Failed to create or open search engine (continuing without search)", "error", engineErr)
		} else {
			if setErr := store.SetSearchEngine(ctx, engine); setErr != nil {
				slog.Error("Failed to initialize search index (continuing without search)", "error", setErr)
			}
		}
	}

	apiPort := config.ResolveAPIPort()
	source := "default (47669)"
	if os.Getenv("MCP_ENDPOINT_API_PORT") != "" {
		source = "MCP_ENDPOINT_API_PORT"
	}
	slog.Info("Resolved HTTP Stream API Port", "port", apiPort, "source", source)

	mcpServer, err := server.NewMCPRecallServer(ctx, Cfg, store, logs, slog.Default(),
		func(n int) []memory.QueryStat { return store.GetTopQueries(n) },
		networkInfoFunc(apiPort),
	)
	if err != nil {
		return fmt.Errorf("could not launch MCP server: %w", err)
	}

	defer func() {
		mcpServer.Close()
	}()

	store.StartConfigWatcher(Cfg.AppConfigDir(), mcpServer.ReloadTools)

	errChan := make(chan error, 1)

	if apiPort > 0 {
		httpServer := startStreamableHTTPAPI(ctx, mcpServer, errChan, apiPort)
		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				slog.Warn("Streamable HTTP server shutdown error", "error", err)
			}
		}()
	}

	startStdioServer(ctx, mcpServer, reader, writer, errChan)

	slog.Info("[BACKPLANE] BOOT_COMPLETE",
		"elapsed_ms", time.Since(bootStart).Milliseconds(),
		"search_enabled", Cfg.SearchEnabled(),
		"db_path", Cfg.GetDBPath(),
	)

	select {
	case <-ctx.Done():
		slog.Info("context cancelled; initiating graceful shutdown")
	case err := <-errChan:
		if mcplib.IsExpectedShutdownErr(err) {
			slog.Info("stdio transport closed gracefully", "reason", err.Error())
			return nil
		}
		return fmt.Errorf("server error: %w", err)
	}

	slog.Info("shutdown complete")
	return nil
}

func startStreamableHTTPAPI(ctx context.Context, mcpServer *server.MCPRecallServer, errChan chan<- error, port int) *http.Server {
	slog.Info("starting Streamable HTTP API (MCP 2025-03-26)", "port", port)
	streamHandler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		readOnly := mcp.NewServer(&mcp.Implementation{
			Name:    config.Name + "-readonly",
			Version: Version,
		}, &mcp.ServerOptions{Logger: slog.Default()})
		mcpServer.RegisterSafeTools(readOnly)
		return readOnly
	}, nil)

	internalStreamHandler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		internalSrv := mcp.NewServer(&mcp.Implementation{
			Name:    config.Name + "-internal",
			Version: Version,
		}, &mcp.ServerOptions{Logger: slog.Default()})
		mcpServer.RegisterSafeToolsInternal(internalSrv)
		return internalSrv
	}, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", &localhostMiddleware{next: newAuditMiddleware(streamHandler, httpClients)})
	mux.Handle("/mcp/internal", &localhostMiddleware{next: newAuditMiddleware(internalStreamHandler, nil)})

	srv := &http.Server{
		Addr:              "127.0.0.1:" + strconv.Itoa(port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Streamable HTTP server exited", "error", err)
			errChan <- err
		}
	}()

	return srv
}

type auditMiddleware struct {
	next     http.Handler
	mu       sync.RWMutex
	sessions map[string]string
	registry *ClientRegistry
}

func newAuditMiddleware(next http.Handler, registry *ClientRegistry) *auditMiddleware {
	return &auditMiddleware{
		next:     next,
		sessions: make(map[string]string),
		registry: registry,
	}
}

type localhostMiddleware struct {
	next http.Handler
}

// ServeHTTP performs the ServeHTTP operation.
func (lm *localhostMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		slog.Warn("Blocked external request to internal CLI endpoint", "remote_addr", r.RemoteAddr)
		http.Error(w, "Forbidden: internal CLI endpoint is bound to localhost", http.StatusForbidden)
		return
	}
	lm.next.ServeHTTP(w, r)
}

type initializeParams struct {
	ClientInfo struct {
		Name string `json:"name"`
	} `json:"clientInfo"`
}

type jsonRPCEnvelope struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// ServeHTTP performs the ServeHTTP operation.
//
//nolint:gocognit // HTTP audit middleware intentionally handles initialize/session re-keying in one place.
func (am *auditMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Streamable HTTP uses the Mcp-Session-Id header for session tracking, or sessionId query param.
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		sessionID = r.URL.Query().Get("sessionId")
	}

	isInitialize := false
	var initClientName string

	if r.Method == http.MethodPost && r.Body != nil {
		// Use 4MB limit for audit peeking to avoid truncating large session payloads.
		body, err := io.ReadAll(io.LimitReader(r.Body, 4*1024*1024))
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
		if err == nil && len(body) > 0 {
			var envelope jsonRPCEnvelope
			if json.Unmarshal(body, &envelope) == nil {
				switch envelope.Method {
				case "initialize":
					var params initializeParams
					if json.Unmarshal(envelope.Params, &params) == nil && params.ClientInfo.Name != "" {
						isInitialize = true
						initClientName = params.ClientInfo.Name
						if sessionID == "" {
							sessionID = "pre-init" //nolint:goconst // bypass
						}
						am.mu.Lock()
						am.sessions[sessionID] = params.ClientInfo.Name
						am.mu.Unlock()
						if am.registry != nil {
							am.registry.Register(sessionID, params.ClientInfo.Name)
						}
						slog.Debug("Streamable HTTP client identified",
							"session", sessionID,
							"client", params.ClientInfo.Name,
						)
					}
				default:
					if sessionID != "" {
						am.mu.RLock()
						clientName := am.sessions[sessionID]
						am.mu.RUnlock()
						if clientName != "" {
							ctx := util.WithClient(r.Context(), clientName)
							r = r.WithContext(ctx)
						}
					}
				}
			}
		}
	}

	if isInitialize {
		// Wrap the ResponseWriter to capture the server-assigned Mcp-Session-Id
		capture := &sessionCapture{ResponseWriter: w}
		am.next.ServeHTTP(capture, r)

		// Re-key the client from "pre-init" to the real session ID
		realSessionID := capture.Header().Get("Mcp-Session-Id")
		if realSessionID != "" && realSessionID != sessionID {
			am.mu.Lock()
			if name, ok := am.sessions[sessionID]; ok {
				am.sessions[realSessionID] = name
				delete(am.sessions, sessionID)
			}
			am.mu.Unlock()
			if am.registry != nil {
				am.registry.Unregister(sessionID)
				am.registry.Register(realSessionID, initClientName)
			}
			slog.Debug("HTTP client session re-keyed",
				"old", sessionID, "new", realSessionID, "client", initClientName)
		}
		return
	}

	am.next.ServeHTTP(w, r)
}

// sessionCapture wraps ResponseWriter to let us read the response's Mcp-Session-Id header.
type sessionCapture struct {
	http.ResponseWriter
}

func startStdioServer(ctx context.Context, mcpServer *server.MCPRecallServer, reader io.ReadCloser, writer io.WriteCloser, errChan chan<- error) {
	go func(ctx context.Context) {
		if err := mcpServer.Serve(ctx, writer, reader); err != nil {
			errChan <- err
		}
	}(ctx)
}

// networkInfoFunc returns a callback that provides live network topology data
// for the telemetry snapshot, including stdio and HTTP Streamable API clients.
func networkInfoFunc(port int) func() telemetry.NetworkStats {
	return func() telemetry.NetworkStats {
		clients := httpClients.GetClients()
		return telemetry.NetworkStats{
			StdioConnected: true,
			HTTPPort:       port,
			HTTPClients:    clients,
			TotalClients:   len(clients) + 1, // +1 for stdio
		}
	}
}
