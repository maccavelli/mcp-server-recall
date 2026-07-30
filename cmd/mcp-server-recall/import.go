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

var importCmd = &cobra.Command{
	Use:     "import [filepath]",
	Short:   "Imports a JSONL backup file into the running recall database",
	Long:    "Reads a JSONL file previously created by 'export' and restores all records (memories, standards, sessions) into the running Recall server. Preserves original timestamps and metadata.",
	Example: `  mcp-server-recall import ./backup.jsonl`,
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

		if err := mcplib.WaitForRecallServer(ctx, mcpClient, 10*time.Second); err != nil {
			return fmt.Errorf("failed to connect to recall server after 10s — is the server running? (%w)", err)
		}

		fmt.Fprintf(os.Stderr, "Connected. Importing database from %s\n", absPath)

		toolArgs := map[string]any{
			"filename": absPath,
		}

		res, err := mcpClient.CallDatabaseToolE(ctx, "import_records", toolArgs)
		if err != nil {
			return fmt.Errorf("import_records: %w", err)
		}

		if _, err := fmt.Fprintln(RealStdout, res); err != nil {
			return fmt.Errorf("write import result: %w", err)
		}
		return nil
	},
}

func init() {
	RootCmd.AddCommand(importCmd)
}
