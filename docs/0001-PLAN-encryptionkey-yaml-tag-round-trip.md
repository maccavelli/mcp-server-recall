# Implement Persist the recall encryption key as an explicitly typed YAML string and never discard it silently

Associated MADR: [0001-MADR-encryptionkey-yaml-tag-round-trip.md](0001-MADR-encryptionkey-yaml-tag-round-trip.md) (status: accepted)

## Goal

An encryption key written by `mcp-server-recall configure` must be readable by the server on the
next load, for every valid 64-character hex key, and no non-interactive invocation may destroy an
existing key while reporting success.

Done means all six acceptance criteria in [Verification](#verification) hold.

## Scope

| File | Lines | Change |
|---|---|---|
| `cmd/mcp-server-recall/configure.go` | 144-148 | replace value node instead of mutating it |
| `cmd/mcp-server-recall/configure.go` | 150-156 | same constructor on the insert path |
| `cmd/mcp-server-recall/configure.go` | 66-68 | refuse to blank an existing key non-interactively |
| `cmd/mcp-server-recall/configure.go` | 225 | register `--allow-unencrypted` |
| `cmd/mcp-server-recall/config_template.go` | 20 | `%s` → `%q` |
| `cmd/mcp-server-recall/configure_test.go` | new tests | round-trip + clobber-refusal coverage |
| `internal/config/config.go` | after 169 | recover a `!!null`-tagged key at load |

Out of scope: the database location (that is
[`mcplib` MADR 0002](../../mcplib/docs/0002-MADR-xdg-compliant-user-paths.md)), the keychain
option, typed-struct marshalling, and any change to `internal/memory/badger.go`.

## Preconditions

```bash
cd ~/gitrepos/go/mcp-server-recall
git status --porcelain -uno          # must be empty
git log --oneline -1                 # expect 5322425 build(deps): mcplib v1.0.1
go build ./... && go test ./...      # must be green before starting
```

Baseline facts confirmed in this tree at `5322425`:

* `forceBlockStyle` (`configure.go:232-242`) clears `FlowStyle` **only** on `SequenceNode` and
  `MappingNode`. It does not touch scalar `Style`, so an explicit `DoubleQuotedStyle` on the key
  scalar survives marshalling. No exemption is required.
* `configure_test.go` already sandboxes via `sandboxConfigDir(t)` (`paths_test.go:16-26`), which
  sets `HOME` and `XDG_CONFIG_HOME` to a `t.TempDir()`. Reuse it; do not write new sandboxing.
* `config.Name = "mcp-server-recall"` and `config.DefaultDBName = ".mcp_recall"`
  (`internal/config/config.go:23-24`).
* Writes go through `util.WriteFileAtomic` (`configure.go:166`), which writes a temp file in the
  same directory and renames. No change needed.

## Implementation Steps

### Step 1 — Add failing tests

Write these **first** and confirm each fails against `5322425`. A fix that lands without a
previously-failing test has not been shown to fix anything.

Append to `cmd/mcp-server-recall/configure_test.go`:

```go
func TestConfigure_KeyRoundTrips(t *testing.T) {
	for _, tc := range []struct{ name, key string }{
		{"mixed", "8e71e69965ade5e8fd42c399212ed45e324bfe9e41ca2d32266a9d7ebd2dacc0"},
		{"digits_only", strings.Repeat("1", 64)}, // implicit numeric typing
		{"all_f", strings.Repeat("f", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := sandboxConfigDir(t)
			Cfg = config.New("test-roundtrip")
			t.Setenv("RECALL_ENCRYPTION_KEY", tc.key) // takes the env branch at configure.go:63
			if err := ensureInitialized(true); err != nil {
				t.Fatalf("ensureInitialized: %v", err)
			}
			if err := configureCmd.RunE(configureCmd, []string{}); err != nil {
				t.Fatalf("configure: %v", err)
			}

			// (a) the file must not carry a null tag on a populated key
			p := filepath.Join(base, config.Name, "recall.yaml")
			content, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if bytes.Contains(content, []byte("encryptionkey: !!null")) {
				t.Errorf("key written with a !!null tag:\n%s", grepLine(content, "encryptionkey"))
			}

			// (b) the round trip is what actually matters
			reloaded := config.New("test-roundtrip")
			if got := reloaded.EncryptionKey(); got != tc.key {
				t.Errorf("key did not survive write->read: got %q want %q", got, tc.key)
			}
		})
	}
}
```

`grepLine` is a small test helper returning the matching line, for a legible failure message.

Confirm `reloaded := config.New(...)` actually performs the read; if construction is lazy, call
whatever `New` defers to so the assertion exercises viper's typed decode at `config.go:169`
rather than a cached value. **This is the single most important assertion in the change** — it
is the one the current code fails.

```go
func TestConfigure_NonInteractiveRefusesToClobber(t *testing.T) {
	base := sandboxConfigDir(t)
	Cfg = config.New("test-clobber")
	key := strings.Repeat("a", 64)

	// Seed a config that already carries a key, via the env branch.
	t.Setenv("RECALL_ENCRYPTION_KEY", key)
	if err := ensureInitialized(true); err != nil {
		t.Fatalf("ensureInitialized: %v", err)
	}
	if err := configureCmd.RunE(configureCmd, []string{}); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	cfgPath := filepath.Join(base, config.Name, "recall.yaml")
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read seeded config: %v", err)
	}

	// Force the non-interactive branch: no env key, stdin is not a TTY under `go test`.
	t.Setenv("RECALL_ENCRYPTION_KEY", "")

	if err := configureCmd.RunE(configureCmd, []string{}); err == nil {
		t.Fatal("expected refusal when clobbering an existing key non-interactively")
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config after refusal: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("config was modified despite the refusal")
	}
}
```

Note `t.Setenv(…, "")` rather than `os.Unsetenv`: `configure.go:62` tests `envKey != ""`, so an
empty value takes the same branch as unset, and `t.Setenv` restores the prior value on cleanup.
The essential assertions are **non-zero error** and **file byte-identical**.

Add `TestConfigure_NonInteractiveAllowsExplicitOptOut`: same setup, but set the new flag, and
assert success plus a blank key.

Expected failures at this point:

| Test | Expected failure against `5322425` |
|---|---|
| `TestConfigure_KeyRoundTrips` | (a) finds `!!null`; (b) reloaded key is `""` |
| `TestConfigure_NonInteractiveRefusesToClobber` | no error returned; file modified |
| `TestConfigure_NonInteractiveAllowsExplicitOptOut` | compile error — flag does not exist |

Run: `go test ./cmd/mcp-server-recall/ -run TestConfigure -v` and record the failures.

### Step 2 — Fix serialization

In `cmd/mcp-server-recall/configure.go`, add the constructor near `forceBlockStyle`:

```go
// newKeyScalar returns a scalar node pinned to !!str. The tag and quoted style are explicit
// because an inferred type is wrong for a digit-only key, which YAML resolves as a number.
// See docs/0001-MADR-encryptionkey-yaml-tag-round-trip.md.
func newKeyScalar(v string) *yaml.Node {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Style: yaml.DoubleQuotedStyle,
		Value: v,
	}
}
```

Replace lines 144-148. The defect is that `valNode.Tag` survives; assigning a fresh node is what
removes it:

```go
// before (144-148)
if keyNode.Value == "encryptionkey" {
    valNode.Value = input
    keyFound = true
    break
}

// after
if keyNode.Value == "encryptionkey" {
    mappingNode.Content[i+1] = newKeyScalar(input)
    keyFound = true
    break
}
```

`valNode` becomes unused inside the loop; remove the declaration at line 143 if the compiler
flags it, keeping `keyNode`.

Replace the insert path at 150-156 so both paths are identical:

```go
if !keyFound {
    mappingNode.Content = append(mappingNode.Content,
        &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "encryptionkey"},
        newKeyScalar(input),
    )
}
```

### Step 3 — Refuse non-interactive clobbering

Two ordering facts govern this step:

1. `existingKey := Cfg.EncryptionKey()` (`configure.go:58`) returns `""` when the file is
   corrupt — the compounding effect described in the MADR. A guard built on `existingKey` cannot
   see the key it protects.
2. `rootNode` is already parsed at `configure.go:44` and *does* hold the raw value regardless of
   typed-decode failure.

Therefore read the key from `rootNode`, not from `Cfg`. Add:

```go
// existingKeyFromNode reads encryptionkey directly from the parsed document, so the guard
// holds even when typed decoding failed and Cfg.EncryptionKey() is empty.
func existingKeyFromNode(root *yaml.Node) string {
	if len(root.Content) == 0 { return "" }
	m := root.Content[0]
	for i := 0; i < len(m.Content)-1; i += 2 {
		if m.Content[i].Value == "encryptionkey" {
			return strings.TrimSpace(m.Content[i+1].Value)
		}
	}
	return ""
}
```

Replace lines 66-68:

```go
} else if !term.IsTerminal(int(os.Stdin.Fd())) {
    if existingKeyFromNode(&rootNode) != "" && !allowUnencrypted {
        return fmt.Errorf(
            "refusing to overwrite an existing encryption key non-interactively; " +
                "re-run attached to a terminal, set RECALL_ENCRYPTION_KEY, " +
                "or pass --allow-unencrypted to intentionally disable encryption")
    }
    pterm.Warning.Println("Non-interactive terminal detected. Proceeding without encryption.")
    input = ""
}
```

Declare `var allowUnencrypted bool` beside `forceInit`, and register it next to the existing
flag at `configure.go:225`:

```go
configureCmd.Flags().BoolVar(&allowUnencrypted, "allow-unencrypted", false,
    "Permit a non-interactive run to disable encryption on a config that already has a key")
```

Note `TestConfigureCommand_Sandboxed` runs non-interactively against a **freshly initialised**
config whose key is blank, so `existingKeyFromNode` returns `""` and the guard does not fire.
That test must keep passing unchanged; treat any change to it as a signal the guard is too broad.

### Step 4 — Fix the template

`cmd/mcp-server-recall/config_template.go:20`:

```go
encryptionkey: %s   →   encryptionkey: %q
```

`ensureInitialized` calls `fmt.Sprintf(FullConfigTemplate, "")` (`configure.go:215`), so the
empty case renders `encryptionkey: ""` — which is exactly the first alternative
`configure_test.go:54` already accepts. Verify that test still passes.

### Step 5 — Recover already-corrupted configs at load

A file containing `encryptionkey: !!null <hex>` still fails typed decode after Steps 2-4,
because nothing rewrites it. In `internal/config/config.go`, after `v.ReadInConfig()` at line
169, detect this exact shape and recover:

* Trigger **only** when the error is a YAML decode error naming `encryptionkey` and `!!null`.
* Re-read the file untyped, pull the scalar's string value, inject it via `v.Set("encryptionKey", …)`.
* `slog.Warn` that the config carries a legacy null-tagged key and should be rewritten with
  `configure`.

Keep this narrow. A broad "retry harder" fallback would mask genuine corruption, which is the
opposite of the MADR's intent.

### Step 6 — Repair the live host

The operator has confirmed the datastore is empty, so no key recovery is required and
`--force` carries no data risk here.

```bash
cp "$HOME/Library/Application Support/mcp-server-recall/recall.yaml"{,.pre-repair}
make build-all && make install
RECALL_ENCRYPTION_KEY=$(openssl rand -hex 32) mcp-server-recall configure
grep -c '!!null' "$HOME/Library/Application Support/mcp-server-recall/recall.yaml"   # expect 0
launchctl kickstart -k gui/$(id -u)/<magictools-service-label>
```

Then confirm `serve` reports `encrypted=true` (`internal/memory/badger.go:336`).

## Verification

```bash
cd ~/gitrepos/go/mcp-server-recall
go test ./cmd/mcp-server-recall/ -run TestConfigure -v
go test ./... && go build ./...
GOOS=windows go build ./... && GOOS=linux go build ./...

# pre-commit gate, per changed file
gofmt -l cmd/mcp-server-recall/configure.go cmd/mcp-server-recall/config_template.go \
         cmd/mcp-server-recall/configure_test.go internal/config/config.go
golint  cmd/mcp-server-recall/configure.go internal/config/config.go
golangci-lint run -c .golangci.yml ./...        # pinned v2.13.1
```

End-to-end against a scratch HOME, never the live config:

```bash
TMP=$(mktemp -d)
KEY=$(openssl rand -hex 32)
HOME="$TMP" RECALL_ENCRYPTION_KEY="$KEY" ./dist/mcp-server-recall-darwin-arm64 configure
CFG="$TMP/Library/Application Support/mcp-server-recall/recall.yaml"
grep -E '^encryptionkey:' "$CFG"        # expect: encryptionkey: "<KEY>"
grep -c '!!null' "$CFG"                 # expect: 0

# the clobber guard
HOME="$TMP" ./dist/mcp-server-recall-darwin-arm64 configure </dev/null; echo "exit=$?"   # expect non-zero
grep -qE "^encryptionkey: \"$KEY\"$" "$CFG" && echo "key intact"
```

Acceptance criteria — all must hold:

1. `TestConfigure_KeyRoundTrips` passes for all three key shapes, including digit-only.
2. A written config carrying a key contains zero `!!null` occurrences.
3. Non-interactive invocation against an existing key exits non-zero and leaves the file
   byte-identical.
4. `--allow-unencrypted` permits the blank-key path, and `TestConfigureCommand_Sandboxed` passes
   **unmodified**.
5. `go test ./...`, all three `GOOS` builds, and `golangci-lint` are clean.
6. On the host, `serve` logs `encrypted=true`.

## Rollout and Rollback

**Rollout.** One commit per step group — Steps 1-2 (serialization + tests), Step 3 (guard),
Steps 4-5 (template + recovery) — each passing the pre-commit gate. Then Step 6 on the host.
Nothing is pushed or tagged without explicit approval.

Release as a patch (`v1.0.3`) once verified. No repository imports `mcp-server-recall`, so there
are no downstream consumers.

**Rollback.** Plain revert; no schema, file-location, or on-disk format changes. The previous
binary reads anything the fixed binary writes — with the pre-existing caveat that it will again
fail to decode a key it wrote itself.

Config rollback is file-level:

* `scratchpad/recall.yaml.bak` — pre-diagnosis config, original key.
* `recall.yaml.pre-repair` — created in Step 6.

Restore with `cp` plus a service restart.

**Residual risk.** With the datastore confirmed empty, there is no irreversible risk in this
change. Should that change before execution — if the store is repopulated — Step 6 must first
re-establish whether `.mcp_recall/` is encrypted, since a lost key makes an encrypted store
unrecoverable.

## Sequencing against mcplib MADR 0002

Both records edit `cmd/mcp-server-recall/config_template.go` and `internal/config/config.go`.
MADR 0002 additionally changes `configure.go:75` and `configure.go:210-211`, which locate and
create the database directory under the config directory.

Land this plan **first**: it is self-contained, needs no `mcplib` release, and its tests pin the
key behaviour before path resolution moves underneath it. Rebase 0002 onto the result. Do not run
both in the same working tree concurrently.
