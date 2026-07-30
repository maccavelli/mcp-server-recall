// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcplib"

	"github.com/spf13/cobra"
)

var pruneCmd = &cobra.Command{
	Use:   "prune <namespace> [days]",
	Short: "Prunes records in the specified namespace older than [days] (default 30)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		namespace := args[0]
		days := 30
		if len(args) > 1 {
			var err error
			days, err = strconv.Atoi(args[1])
			if err != nil || days < 0 {
				return fmt.Errorf("invalid days argument: %s", args[1])
			}
		}
		return runPruneViaMCP(namespace, days)
	},
}

// runPruneViaMCP connects to the running Recall MCP server and calls the prune_records tool.
func runPruneViaMCP(namespace string, days int) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	url := config.ResolveRecallURL() + "/internal"
	fmt.Fprintf(os.Stderr, "Connecting to local recall server at %s...\n", url)

	mcpClient := mcplib.NewRecallClient(url, "recall-cli")
	defer mcpClient.Close()
	go mcpClient.Start(ctx)

	if err := mcplib.WaitForRecallServer(ctx, mcpClient, 10*time.Second); err != nil {
		return fmt.Errorf("failed to connect to recall server after 10s — is the server running? (%w)", err)
	}

	fmt.Fprintf(os.Stderr, "Connected. Pruning namespace: %s (older than %d days)\n", namespace, days)

	toolArgs := map[string]any{
		"namespace": namespace,
		"days_old":  days,
	}

	res, err := mcpClient.CallDatabaseToolE(ctx, "prune_records", toolArgs)
	if err != nil {
		return fmt.Errorf("prune_records(%s): %w", namespace, err)
	}

	if _, err := fmt.Fprintln(RealStdout, res); err != nil {
		return fmt.Errorf("write prune result: %w", err)
	}
	return nil
}

func init() {
	RootCmd.AddCommand(pruneCmd)
}
