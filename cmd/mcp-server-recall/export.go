// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcplib"

	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:     "export [filepath]",
	Short:   "Exports the entire recall database to a JSONL file for backup",
	Long:    "Streams all records (memories, standards, sessions) from the running Recall server to a JSONL file. The output file must not already exist (O_EXCL safety).",
	Example: `  mcp-server-recall export ./backup.jsonl`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		absPath, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("failed to resolve absolute path: %w", err)
		}

		url := config.ResolveRecallURL() + "/internal"
		fmt.Fprintf(os.Stderr, "Connecting to local recall server at %s...\n", url)

		mcpClient := mcplib.NewRecallClient(url, "recall-cli")
		defer mcpClient.Close()
		go mcpClient.Start(ctx)

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		for !mcpClient.RecallEnabled() {

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return fmt.Errorf("failed to connect to recall server after 10s — is the server running?")
			case <-ticker.C:
			}
		}

		fmt.Fprintf(os.Stderr, "Connected. Exporting database to %s\n", absPath)

		toolArgs := map[string]any{
			"filename": absPath,
		}

		res, err := mcpClient.CallDatabaseToolE(ctx, "export_records", toolArgs)
		if err != nil {
			return fmt.Errorf("export_records: %w", err)
		}

		if _, err := fmt.Fprintln(RealStdout, res); err != nil {
			return fmt.Errorf("write export result: %w", err)
		}
		return nil
	},
}

func init() {
	RootCmd.AddCommand(exportCmd)
}
