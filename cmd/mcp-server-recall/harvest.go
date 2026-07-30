// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcplib"

	"github.com/spf13/cobra"
)

var harvestCmd = &cobra.Command{
	Use:   "harvest", //nolint:goconst // bypass
	Short: "Harvest structural intelligence from Go packages into Recall namespaces",
	Example: `  mcp-server-recall harvest standards github.com/ollama/ollama/api
  mcp-server-recall harvest projects /path/to/local/project`,
}

var harvestStandardsCmd = &cobra.Command{
	Use:     "standards [package-path]",
	Short:   "Harvests a Go package into the standards namespace (external libraries)",
	Example: `  mcp-server-recall harvest standards github.com/ollama/ollama/api`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHarvestViaMCP("standards", args[0])
	},
}

var harvestProjectsCmd = &cobra.Command{
	Use:   "projects [package-path]",
	Short: "Harvests a local Go project into the projects namespace (project intelligence)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHarvestViaMCP("projects", args[0])
	},
}

func runHarvestViaMCP(namespace, pkgPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	targetPath := pkgPath
	if strings.HasPrefix(targetPath, ".") || strings.HasPrefix(targetPath, "/") || strings.HasPrefix(targetPath, "..") {
		var err error
		targetPath, err = filepath.Abs(targetPath)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute path: %w", err)
		}
	}

	url := config.ResolveRecallURL() + "/internal"
	fmt.Fprintf(os.Stderr, "Connecting to local recall server at %s...\n", url)

	mcpClient := mcplib.NewRecallClient(url, "recall-cli")
	defer mcpClient.Close()
	go mcpClient.Start(ctx)

	// Blocking wait until the 'initialize' lifecycle is fully established
	if err := mcplib.WaitForRecallServer(ctx, mcpClient, 10*time.Second); err != nil {
		return fmt.Errorf("failed to connect to recall server after 10s — is the server running? (%w)", err)
	}

	fmt.Fprintf(os.Stderr, "Connected to running Recall server. Firing %s -> %s\n", namespace, targetPath)

	toolArgs := map[string]any{
		"namespace":   namespace,
		"target_path": targetPath,
	}

	toolName := "harvest"
	res, err := mcpClient.CallDatabaseToolE(ctx, toolName, toolArgs)
	if err != nil {
		return fmt.Errorf("%s: %w", namespace, err)
	}

	// Push the raw structured JSON payload out correctly
	if _, err := fmt.Fprintln(RealStdout, res); err != nil {
		return fmt.Errorf("write harvest result: %w", err)
	}
	return nil
}

func init() {
	harvestCmd.AddCommand(harvestStandardsCmd, harvestProjectsCmd)
	RootCmd.AddCommand(harvestCmd)
}
