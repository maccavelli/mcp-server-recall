// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/maccavelli/mcp-server-recall/internal/config"
)

var forceFlag bool

var purgeCmd = &cobra.Command{
	Use:   "purge", //nolint:goconst // bypass
	Short: "Destructively clears the underlying datastore",
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := Cfg.GetDBPath()
		if config.UnsafeDatabasePath(dbPath) || config.IsCWDOrParent(dbPath) {
			return fmt.Errorf("refusing to purge: empty dbpath is not a purge target (path %q)", dbPath)
		}
		if _, err := os.Stat(filepath.Join(dbPath, "MANIFEST")); err != nil {
			return fmt.Errorf("refusing to purge: %s is not a Badger store (missing MANIFEST): %w", dbPath, err)
		}

		// Safety guard: require explicit confirmation to prevent accidental data loss.
		if !forceFlag {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("purge aborted: non-interactive terminal detected — use --force to confirm")
			}
			result, err := pterm.DefaultInteractiveConfirm.
				WithDefaultValue(false).
				Show(fmt.Sprintf("⚠️  This will permanently delete the database at: %s\nAre you sure?", dbPath))
			if err != nil {
				return fmt.Errorf("prompt error: %w", err)
			}
			if !result {
				fmt.Fprintln(os.Stderr, "Purge cancelled.")
				return nil
			}
		}

		slog.Warn("purge requested: deleting existing database", "path", dbPath)
		if err := os.RemoveAll(dbPath); err != nil {
			return fmt.Errorf("failed to clear database during purge: %w", err)
		}
		slog.Info("database reset successfully. Exiting.")
		return nil
	},
}

func init() {
	purgeCmd.Flags().BoolVar(&forceFlag, "force", false, "Skip confirmation prompt (required for non-interactive use)")
	RootCmd.AddCommand(purgeCmd)
}
