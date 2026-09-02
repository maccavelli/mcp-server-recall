// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"fmt"
	"os"

	"github.com/maccavelli/mcp-server-recall/internal/config"

	"github.com/spf13/cobra"
)

var (
	// Version is injected during runtime

	// Cfg provides thread-safe access to Viper values
	Cfg *config.Config

	// RealStdout carefully preserves standard out for JSON-RPC transport constraints
	RealStdout *os.File
)

// RootCmd intercepts all runtime behaviors cleanly
var RootCmd = &cobra.Command{
	Use:   config.Name,
	Short: "Recall Engine",
	Long:  "A Model Context Protocol orchestration engine for codebase extraction and vector recall.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return serveCmd.RunE(cmd, args)
	},
}

// Execute protects the native standard streams before cascading into
// subcommands and returns the command error. It no longer calls os.Exit: exit
// mapping belongs to main so `update --check` can report an available update
// as exit 10 rather than a generic failure (MADR 0005).
func Execute() error {
	// CRITICAL constraint: Steal os.Stdout to forbid Cobra usage-printing corruption
	RealStdout = os.Stdout
	os.Stdout = os.Stderr
	RootCmd.SetOut(os.Stderr)
	RootCmd.SetErr(os.Stderr)

	// Enable --version flag, outputting to stderr to match stdout protection pattern
	RootCmd.Version = Version
	RootCmd.SetVersionTemplate(fmt.Sprintf("%s version %s\n", config.Name, Version))

	return RootCmd.Execute()
}

func init() {
	// cobra.OnInitialize ran initConfig for EVERY command, which would
	// initialize Viper and start the fsnotify watcher just to check for an
	// update. A pre-run hook can be scoped instead: a command annotated
	// selfupdate.skip-config opts out. A child's own PersistentPreRunE would
	// override this one, so no child defines one.
	RootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Annotations[skipConfigAnnotation] == skipConfigValue {
			return nil
		}
		initConfig()
		return nil
	}
	RootCmd.AddCommand(newUpdateCmd())
}

func initConfig() {
	// Initialize Viper mappings and spawn the fsnotify file-watcher loops securely
	Cfg = config.New(Version)
}
