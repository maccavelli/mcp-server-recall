# Implement Persist the recall encryption key as an explicitly typed YAML string and never discard it silently

Associated MADR: [0001-MADR-encryptionkey-yaml-tag-round-trip.md](0001-MADR-encryptionkey-yaml-tag-round-trip.md) (status: accepted)

## Execution Status

Last reviewed: 2026-08-29.

Repository implementation exists in commit
`0f6074536a14db0176af598510265b4cac28db8d` (`fix(configure): persist the encryption
key as a typed YAML string`). The implementation, tests, this MADR, and this PLAN all landed in
that one commit. This differs from the three phase commits specified under
[Rollout and Rollback](#rollout-and-rollback).

Status by outcome:

| Outcome | Status | Repository evidence |
|---|---|---|
| Explicitly typed and quoted key serialization | implemented and verified | `configure.go:151-173`; `TestConfigure_KeyRoundTrips` |
| Non-interactive clobber refusal | implemented and verified | `configure.go:72-82`; `TestConfigure_NonInteractiveRefusesToClobber` |
| Explicit non-interactive opt-out | implemented and verified | `configure.go:240-243`; `TestConfigure_NonInteractiveAllowsExplicitOptOut` |
| Positive recovery of the observed legacy key shape | implemented and verified | `config.go:173-188`; `TestConfig_RecoversNullTaggedKey` |
| Narrow recovery that cannot trigger on unrelated `!!null` failures | not implemented or verified | `recoverNullTaggedKey` checks only for `!!null`; no negative recovery tests exist |
| Live-host repair and `encrypted=true` confirmation | not established | `recall.yaml.pre-repair` exists and the live file now has a quoted key, but no `serve` log in this repository shows `encrypted=true`. Directory emptiness is false (see 0002); record-count emptiness is still unmeasured. |
| Patch release | not completed at reviewed HEAD | no tag points at `0f60745`; latest reachable release tag is `v1.0.2` |

Repository-local verification observed on 2026-08-29:

* `go test ./...` passed.
* `go build ./...` passed on the review host (`darwin/arm64`).
* `GOOS=windows go build ./...` passed.
* `GOOS=linux go build ./...` passed.
* `gofmt -l` produced no output for all five changed Go files.
* `golint` passed when invoked separately for each changed Go file.
* `golangci-lint run -c .golangci.yml ./...` reported `0 issues`.

The plan is therefore **implemented with a required recovery-narrowing follow-up**, not fully
complete under its acceptance criteria.

## Goal

An encryption key written by `mcp-server-recall configure` must be readable by the server on the
next load, for every valid 64-character hex key, and no non-interactive invocation may destroy an
existing key while reporting success.

Done means all seven acceptance criteria in [Verification](#verification) hold. Criterion 6 is
currently unmet, and criterion 7 has no repository evidence.

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
| `internal/config/config_test.go` | new tests | positive and negative legacy-recovery coverage |

Out of scope: the database location (that is
[`mcplib` MADR 0002](../../mcplib/docs/0002-MADR-xdg-compliant-user-paths.md)), the keychain
option, typed-struct marshalling, and any change to `internal/memory/badger.go`.

## Historical Baseline and Preconditions

This plan was authored against commit `5322425` and implemented by `0f60745`. The original
baseline commands are retained below as historical execution context; they are not current
instructions for reapplying an already-landed change.

```bash
cd ~/gitrepos/go/mcp-server-recall
git status --porcelain               # must be empty; do not hide untracked files
git log --oneline -1                 # expect 5322425 build(deps): mcplib v1.0.1
go build ./...
go test ./...                        # must be green before starting
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
169 in the baseline, detect this exact shape and recover.

The reproduced Viper error contains `!!null` and the scalar value but does **not** name the YAML
mapping key. Do not require an error substring that the observed decoder does not emit. Instead:

* Require the read error to identify a `!!null` decode mismatch before attempting recovery.
* Re-read the file and unmarshal it into a `yaml.Node`. Do not extract the value with line-based
  string matching.
* Locate `encryptionkey` only in the document's top-level mapping.
* Require its value node to be a scalar tagged exactly `!!null` with a non-empty value.
* Require the value to be exactly 64 characters and require `hex.DecodeString` to succeed.
* Only after all checks pass, inject the value via `v.Set("encryptionKey", …)`.
* `slog.Warn` that the config carries a legacy null-tagged key and should be rewritten with
  `configure`.

Keep this narrow. A broad "retry harder" fallback would mask genuine corruption, which is the
opposite of the MADR's intent.

Add tests in `internal/config/config_test.go`:

* the observed top-level `encryptionkey: !!null <64-hex>` shape is recovered;
* an unrelated `!!null` decode failure with a correctly tagged encryption key does not invoke
  legacy recovery;
* a nested `encryptionkey: !!null <64-hex>` is not recovered as the top-level key;
* empty, non-hex, short, and long legacy values are rejected; and
* a file without a `!!null` decode mismatch is not recovered.

**Execution finding.** Commit `0f60745` implemented only the positive path. Its helper checks the
error for `!!null`, scans trimmed lines for `encryptionkey:`, and does not validate the node tag,
scope, length, or hexadecimal encoding. The negative cases above remain required follow-up work.

### Step 6 — Live-host repair (blocked pending external evidence)

Do not replace the live encryption key based on this plan. The repository does not contain
evidence that the approximately 128 MB datastore is empty, unencrypted, disposable, or
recoverable with the recorded key. It also does not identify the launchd service label. The
earlier assertion that the operator had confirmed an empty datastore is unsupported by the
available artifacts and is withdrawn.

Before a separate live-host runbook can be approved, record all of the following outside the
repository or in a deliberately redacted operational record:

* the datastore disposition and whether its contents must be preserved;
* whether the current store is encrypted and which key, if any, opens it;
* the exact configuration path and a verified backup path;
* the exact service label and restart command; and
* the operator-approved rollback procedure.

The only live command retained here is the non-destructive configuration backup. Its destination
must not already exist:

```bash
test ! -e "$HOME/Library/Application Support/mcp-server-recall/recall.yaml.pre-repair"
cp "$HOME/Library/Application Support/mcp-server-recall/recall.yaml" \
   "$HOME/Library/Application Support/mcp-server-recall/recall.yaml.pre-repair"
cmp "$HOME/Library/Application Support/mcp-server-recall/recall.yaml" \
    "$HOME/Library/Application Support/mcp-server-recall/recall.yaml.pre-repair"
```

Generating a replacement key, installing a binary, modifying the live configuration, and
restarting the service are intentionally absent until the external facts above are supplied.

## Verification

```bash
cd ~/gitrepos/go/mcp-server-recall
go test ./cmd/mcp-server-recall/ -run TestConfigure -v
go test ./internal/config/ -run TestConfig_RecoversNullTaggedKey -v
go test ./...
go build ./...
GOOS=windows go build ./...
GOOS=linux go build ./...

# pre-commit gate, per changed file
gofmt -l cmd/mcp-server-recall/configure.go cmd/mcp-server-recall/config_template.go \
         cmd/mcp-server-recall/configure_test.go internal/config/config.go \
         internal/config/config_test.go
golint cmd/mcp-server-recall/configure.go
golint cmd/mcp-server-recall/config_template.go
golint cmd/mcp-server-recall/configure_test.go
golint internal/config/config.go
golint internal/config/config_test.go
golangci-lint run -c .golangci.yml ./... # pinned v2.13.1
```

`gofmt -l` and every `golint` invocation must produce no output. `golint` is intentionally run
once per file: combining files from the `main` and `config` packages in one invocation fails with
`internal/config/config.go is in package config, not main` and does not perform the required
per-file checks.

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
6. Legacy recovery passes every positive and negative case specified in Step 5 and cannot recover
   a normally tagged, nested, malformed, short, long, or non-hex key merely because another
   field caused a `!!null` decode error.
7. If live repair is separately approved, its execution record establishes the datastore
   disposition, records a verified configuration backup, identifies the exact service, and
   captures the post-restart `encrypted=true` log from `internal/memory/badger.go:336`.

## Rollout and Rollback

**Intended rollout.** One commit per step group — Steps 1-2 (serialization + tests), Step 3
(guard), Steps 4-5 (template + recovery) — each passing the pre-commit gate. Then Step 6 on the
host after separate operational approval. Nothing is pushed or tagged without explicit approval.

**Actual repository rollout.** Commit `0f60745` combined the MADR, PLAN, all source changes, and
all tests. It did not follow the intended phase cadence. Do not rewrite public history merely to
manufacture the missing phases. Land the recovery-narrowing follow-up as its own reviewed phase
after updating and approving its source-change plan. As of 2026-08-29, no release tag points at
`0f60745`; release remains pending. This repository alone cannot establish whether downstream
deployment or automation consumers exist.

**Repository rollback.** Revert commit `0f60745`; no schema, file-location, or on-disk database
format changed. The previous binary can parse the explicitly quoted string emitted by the fixed
binary. Reverting also restores the defects recorded in the MADR, so do this only with an
approved alternative mitigation.

**Live configuration rollback.** As of 2026-08-29, `recall.yaml.pre-repair` exists next to the
live configuration (observed while investigating recall MADR 0002). `scratchpad/recall.yaml.bak`
is not present in this working tree. Restoring `recall.yaml.pre-repair` would reintroduce the
`!!null`-tagged key this plan exists to eliminate; do not restore it unless recovering from a
later, unrelated live-config failure, and only with an approved runbook that names the service
to restart.

**Residual risk.** Repository-local serialization and guard changes are reversible. Live key
replacement is potentially irreversible if existing data was encrypted with a different key.
The datastore state is unresolved, so this plan makes no claim that live replacement is safe.

## Sequencing against mcplib MADR 0002

Both records edit `cmd/mcp-server-recall/config_template.go` and `internal/config/config.go`.
MADR 0002 additionally changes `configure.go:75` and `configure.go:210-211`, which locate and
create the database directory under the config directory.

This plan landed first in `0f60745`. Before executing mcplib MADR 0002, revalidate its assumptions
against that commit and keep its path changes separate from the recovery-narrowing follow-up.
Do not execute both change sets concurrently in the same working tree.

## Sequencing against recall MADR 0002

[0002-MADR-configure-os-native-datastore-init.md](0002-MADR-configure-os-native-datastore-init.md)
covers empty `dbpath` → CWD, OS data-directory placement, and `configure` materializing a
Badger store. It also edits `configure.go`, `config.go`, and `config_template.go`.

Do not combine 0002 execution with this plan's remaining recovery-narrowing follow-up.
0001's serialization and clobber-guard work is already on `main` at `0f60745`; 0002
assumes that work remains. The recovery-narrowing follow-up in Step 5 stays with this
record.
