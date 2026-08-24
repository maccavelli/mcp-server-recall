// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	"github.com/maccavelli/mcp-server-recall/internal/config"
	"github.com/maccavelli/mcp-server-recall/internal/util"
)

// encryptionKeyField is the YAML mapping key holding the database encryption key.
const encryptionKeyField = "encryptionkey"

var (
	forceInit        bool
	allowUnencrypted bool
)

var configureCmd = &cobra.Command{
	Use:     "configure",
	Aliases: []string{"init"},
	Short:   "Interactive setup to securely generate or update the encryption key",
	RunE: func(cmd *cobra.Command, args []string) error {
		pterm.DefaultHeader.WithFullWidth().Println("Recall Configuration Wizard")

		configPath := configFilePath()
		if err := ensureInitialized(forceInit); err != nil {
			return err
		}

		// Read existing configuration
		configData, err := os.ReadFile(configPath) //nolint:gosec // configPath is the wizard-managed recall.yaml location
		if err != nil {
			pterm.Error.Printf("Failed to read configuration file: %v\n", err)
			return err
		}

		// AST Parsing
		var rootNode yaml.Node
		if err := yaml.Unmarshal(configData, &rootNode); err != nil {
			// Malformed YAML recovery
			bakPath := configPath + ".bak"
			if err := os.Rename(configPath, bakPath); err != nil {
				return fmt.Errorf("failed to backup malformed configuration: %w", err)
			}
			pterm.Warning.Printf("Existing configuration was malformed. Backed up to %s\n", bakPath)

			// Start fresh with empty template node
			if err := yaml.Unmarshal(fmt.Appendf(nil, FullConfigTemplate, ""), &rootNode); err != nil {
				return fmt.Errorf("failed to parse default configuration template: %w", err)
			}
		}

		existingKey := Cfg.EncryptionKey()

		// Determine the new key
		var input string
		envKey := os.Getenv("RECALL_ENCRYPTION_KEY")
		if envKey != "" {
			input = envKey
			pterm.Success.Println("Encryption key securely loaded from RECALL_ENCRYPTION_KEY environment variable.")
		} else if !term.IsTerminal(int(os.Stdin.Fd())) {
			// Read from the parsed document, not Cfg: a config whose key carries a legacy
			// !!null tag fails typed decode, so Cfg.EncryptionKey() is empty and would let a
			// real key be silently discarded here.
			if existingKeyFromNode(&rootNode) != "" && !allowUnencrypted {
				return fmt.Errorf("refusing to overwrite an existing encryption key non-interactively; " +
					"re-run attached to a terminal, set RECALL_ENCRYPTION_KEY, " +
					"or pass --allow-unencrypted to intentionally disable encryption")
			}
			pterm.Warning.Println("Non-interactive terminal detected. Proceeding without encryption.")
			input = ""
		} else {
			if len(existingKey) >= 32 {
				pterm.Success.Println("Valid encryption key already mapped in configuration.")
			}

			// Check DB dir
			dbDir := filepath.Join(configDirPath(), config.DefaultDBName)
			entries, dirErr := os.ReadDir(dbDir)
			if dirErr != nil {
				return fmt.Errorf("read database directory: %w", dirErr)
			}
			if len(entries) > 0 && existingKey != "" {
				pterm.Warning.Println("Changing the encryption key will render existing database contents irrecoverable!")
				confirm, confirmErr := pterm.DefaultInteractiveConfirm.Show("Are you sure you want to proceed?")
				if confirmErr != nil {
					return fmt.Errorf("interactive confirm: %w", confirmErr)
				}
				if !confirm {
					pterm.Info.Println("Aborted.")
					return nil
				}
			}

			encChoice, encErr := pterm.DefaultInteractiveConfirm.
				WithDefaultValue(true).
				Show("Do you want to enable AES-256 encryption-at-rest?")
			if encErr != nil {
				return fmt.Errorf("interactive confirm: %w", encErr)
			}

			if !encChoice {
				input = ""
			} else {
				options := []string{
					"1. Auto-generate AES-256 Key (Recommended)",
					"2. Paste existing 32-character Hex Key",
				}
				sel, selErr := pterm.DefaultInteractiveSelect.WithOptions(options).Show("Select key source")
				if selErr != nil {
					return fmt.Errorf("interactive select: %w", selErr)
				}

				if strings.HasPrefix(sel, "1") {
					keyBytes := make([]byte, 32)
					if _, err := rand.Read(keyBytes); err != nil {
						return fmt.Errorf("error generating key: %w", err)
					}
					input = hex.EncodeToString(keyBytes)
					pterm.Success.Println("Key generated securely.")
				} else {
					result, inputErr := pterm.DefaultInteractiveTextInput.WithMask("*").Show("Paste 64-character Hex Key")
					if inputErr != nil {
						return fmt.Errorf("interactive text input: %w", inputErr)
					}
					input = strings.TrimSpace(result)
				}
			}
		}

		if input != "" {
			if len(input) != 64 {
				return fmt.Errorf("provided key must be exactly 64 characters in length (got %d)", len(input))
			}
			if _, err := hex.DecodeString(input); err != nil {
				return fmt.Errorf("provided key is not a valid hex string")
			}
		}

		// AST Update
		if len(rootNode.Content) > 0 {
			mappingNode := rootNode.Content[0]
			keyFound := false
			for i := 0; i < len(mappingNode.Content)-1; i += 2 {
				keyNode := mappingNode.Content[i]
				if keyNode.Value == encryptionKeyField {
					// Replace the node rather than mutating its Value: the template emits an
					// empty encryptionkey, which parses as a !!null-tagged scalar, and that tag
					// would otherwise survive and be re-emitted alongside the string.
					mappingNode.Content[i+1] = newKeyScalar(input)
					keyFound = true
					break
				}
			}
			if !keyFound {
				// Add it if not present
				mappingNode.Content = append(mappingNode.Content,
					&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: encryptionKeyField},
					newKeyScalar(input),
				)
			}
		}

		forceBlockStyle(&rootNode)
		updatedConfig, err := yaml.Marshal(&rootNode)
		if err != nil {
			return fmt.Errorf("failed to marshal updated configuration: %w", err)
		}

		// Atomic write
		if err := util.WriteFileAtomic(configPath, updatedConfig); err != nil {
			return fmt.Errorf("failed to write config output: %w", err)
		}

		pterm.Println()
		pterm.Success.Println("Configuration Successful!")
		pterm.Info.Printf("Saved locally to: %s\n", configPath)
		if input != "" {
			pterm.Success.Println("Your new database encryption key has been safely vaulted stringently offline.")
		} else {
			pterm.Info.Println("Database configured securely for unencrypted operations.")
		}
		return nil
	},
}

func ensureInitialized(force bool) error {
	configPath := configFilePath()
	dirPath := configDirPath()

	if _, err := os.Stat(configPath); err == nil && !force {
		// Configuration already exists, no need to initialize
		return nil
	}

	if force {
		fd := int(os.Stdin.Fd())
		if term.IsTerminal(fd) {
			confirm, confirmErr := pterm.DefaultInteractiveConfirm.Show("Configuration already exists. Overwrite? (resets to defaults)")
			if confirmErr != nil {
				return fmt.Errorf("interactive confirm: %w", confirmErr)
			}
			if !confirm {
				pterm.Info.Println("Aborted initialization.")
				return nil
			}
		}
	}

	if err := os.MkdirAll(dirPath, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create DB directory inside config directory
	dbDir := filepath.Join(dirPath, config.DefaultDBName)
	if mkErr := os.MkdirAll(dbDir, 0700); mkErr != nil {
		pterm.Warning.Printf("failed to create db directory %s: %v\n", dbDir, mkErr)
	}

	fullConfig := fmt.Sprintf(FullConfigTemplate, "")
	if err := os.WriteFile(configPath, []byte(fullConfig), 0600); err != nil {
		return fmt.Errorf("failed to write configuration: %w", err)
	}

	pterm.Success.Printf("Configuration initialized at: %s\n", configPath)
	return nil
}

func init() {
	configureCmd.Flags().BoolVarP(&forceInit, "force", "f", false, "Overwrite existing configuration file")
	configureCmd.Flags().BoolVar(&allowUnencrypted, "allow-unencrypted", false,
		"Permit a non-interactive run to disable encryption on a config that already has a key")
	RootCmd.AddCommand(configureCmd)
}

// existingKeyFromNode reads encryptionkey straight from the parsed document. It deliberately
// bypasses Cfg so the guard still sees a key that typed decoding rejected.
func existingKeyFromNode(root *yaml.Node) string {
	if root == nil || len(root.Content) == 0 {
		return ""
	}
	mapping := root.Content[0]
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == encryptionKeyField {
			return strings.TrimSpace(mapping.Content[i+1].Value)
		}
	}
	return ""
}

// newKeyScalar returns a scalar node pinned to !!str for the encryption key. The tag and
// quoted style are explicit rather than inferred: a 64-character hex key may be all decimal
// digits, which YAML's implicit resolver types as a number. See
// docs/0001-MADR-encryptionkey-yaml-tag-round-trip.md.
func newKeyScalar(v string) *yaml.Node {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Style: yaml.DoubleQuotedStyle,
		Value: v,
	}
}

// forceBlockStyle recursively traverses the AST and clears the yaml.FlowStyle
// bitmask from all SequenceNode and MappingNode objects to enforce pure
// block-style formatting in the output configuration file.
func forceBlockStyle(node *yaml.Node) {
	if node == nil {
		return
	}
	if node.Kind == yaml.SequenceNode || node.Kind == yaml.MappingNode {
		node.Style &= ^yaml.FlowStyle
	}
	for _, child := range node.Content {
		forceBlockStyle(child)
	}
}
