# Implement Treat empty dbpath as the platform data directory and materialize the datastore during configure

Associated MADR: [0002-MADR-configure-os-native-datastore-init.md](0002-MADR-configure-os-native-datastore-init.md) (status: accepted)

## Execution Status

Approved to execute 2026-08-29. Phases land as separate commits; nothing is pushed.

## Goal

`mcp-server-recall configure` must leave a real Badger store at the OS-native data
directory, `GetDBPath()` must treat empty `dbpath` as that directory rather than CWD, and
`purge` must not be able to delete the working tree as a result. Darwin paths stay on
`~/Library/Application Support`. Linux data is `$XDG_DATA_HOME` or `~/.local/share`.
Windows data is `%LocalAppData%`. Installs are greenfield: no lookup, copy, or preference
of a store left under a previous config-directory default.

Done means every acceptance criterion in [Verification](#verification) holds.

## Scope

| File | Change |
|---|---|
| `internal/config/dirs.go` (new) | `ConfigDir`, `DataDir`, `CacheDir`, `DefaultDBPath` |
| `internal/config/dirs_linux.go` (new) | Linux data-dir base (`XDG_DATA_HOME` or `~/.local/share`) |
| `internal/config/dirs_darwin.go` (new) | Darwin data-dir base = `UserConfigDir` |
| `internal/config/dirs_windows.go` (new) | Windows data-dir base = `UserCacheDir` (`%LocalAppData%`) |
| `internal/config/dirs_test.go` (new) | Unix tests: empty dbpath, Darwin XDG ignored when tagged |
| `internal/config/dirs_linux_test.go` (new) | Linux: DataDir ≠ ConfigDir, XDG_DATA_HOME wins |
| `internal/config/dirs_windows_test.go` (new) | Windows: DataDir under LocalAppData, not AppData |
| `internal/config/config.go` | Use the helpers; `GetDBPath` empty → default; no CWD fallback; `errors.AsType` |
| `internal/config/config_test.go` | Empty `dbpath` does not become CWD; absolute wins |
| `cmd/mcp-server-recall/paths.go` | Delegate to `config.ConfigDir`; return error, no stderr |
| `cmd/mcp-server-recall/paths_test.go` | Sandbox via the same helpers; keep `!windows` or share helpers |
| `cmd/mcp-server-recall/configure.go` | Create/open store at `GetDBPath()`; mkdir is an error; missing dir is not |
| `cmd/mcp-server-recall/configure_test.go` | MANIFEST after configure; missing-dir repair; CWD independence |
| `cmd/mcp-server-recall/config_template.go` | Comments: data dir per OS; 64-hex key; Windows LocalAppData |
| `cmd/mcp-server-recall/purge.go` | Refuse CWD, volume roots, and paths without a Badger MANIFEST |
| `cmd/mcp-server-recall/purge_test.go` (new or extend) | Empty dbpath must not purge CWD |
| `cmd/mcp-server-recall/main.go` | Crash log via `config.CacheDir`; no `TempDir`+Name unless cache fails |
| `README.md` | Drop `--key`; fix default paths; 64-hex; env vars |
| `internal/config/config_extra_test.go` | `t.Setenv` if this change touches the file |

Out of scope: `mcplib/paths` (unimplemented; Darwin XDG disagrees with this MADR);
narrowing `recoverNullTaggedKey` (0001 follow-up); making YAML `apiport` control
`ResolveAPIPort`; moving the process lock out of `os.TempDir`; keychain storage;
rewriting `os.UserHomeDir` harvest paths; **any migration** — detecting, reading,
copying, moving, or preferring a store at a previous config-directory path
(`~/.config/mcp-server-recall/.mcp_recall` on Linux, `%AppData%\mcp-server-recall\.mcp_recall`
on Windows). Greenfield only. An explicit absolute `dbpath` in YAML is a configured
override and still wins; that is not a migration.

Do not execute this plan in the same working tree as a `mcplib/paths` conversion or the
0001 recovery-narrowing follow-up.

## Historical Baseline and Preconditions

```bash
cd ~/gitrepos/go/mcp-server-recall
git status --porcelain               # docs-only untracked/modified is acceptable before
                                     # execution; once execution starts, the tree must
                                     # otherwise be clean
git log --oneline -1                 # expect 0f60745 on main, or a descendant that
                                     # still contains the 0001 serialization fix
go env GOVERSION                     # go1.26.5
go test ./...
go build ./...
```

Baseline facts confirmed 2026-08-29:

* Installed binary `1.0.2-1-g0f60745` writes `dbpath: ""` and an empty `.mcp_recall`.
* `filepath.Abs("")` is CWD. `GetDBPath` has no empty-string branch.
* `ExportDir` already treats `""` as `os.TempDir()`. Copy that shape, not `Abs("")`.
* Live host `dbpath` is already an absolute Application Support path. Tests must not
  rewrite it. Execution must not run `configure --force` against the live `HOME`.
* `sandboxConfigDir` (`paths_test.go:16-26`) sets `HOME` and `XDG_CONFIG_HOME`. After
  this change Darwin still ignores `XDG_CONFIG_HOME`; keep sandboxing via `HOME` so
  `UserConfigDir` lands under the temp dir's `Library/Application Support`.
* `config.Name = "mcp-server-recall"`, `config.DefaultDBName = ".mcp_recall"`.
* `memory.NewMemoryStore` already `MkdirAll`s and opens Badger; `configure` should call
  it and close it, not duplicate open options.
* `util.WriteFileAtomic` already exists and is used for the post-wizard YAML rewrite.

## Implementation Steps

### Phase 1 — Failing tests

Write these first. Confirm they fail on the baseline before changing production code.

**`internal/config`**

* `TestGetDBPath_EmptyYAMLIsNotCWD` — sandbox HOME, write a minimal `recall.yaml` with
  `dbpath: ""`, `chdir` into a *different* temp dir, `New`, assert
  `GetDBPath() != wd` and `GetDBPath()` equals `filepath.Join(DataDir(Name), DefaultDBName)`.
* `TestGetDBPath_AbsentFileUsesDefault` — no YAML, same assertion.
* `TestGetDBPath_AbsoluteWins` — YAML with an absolute path under the sandbox, assert
  equality with that path.
* `TestGetDBPath_IgnoresConfigDirStore` — Linux-tagged: create a fake store only under
  the config dir, leave the data dir absent, assert `GetDBPath()` is still the data-dir
  default and is not the config-dir path. Greenfield: leftover config-dir files are
  ignored. On Darwin the two paths coincide, so skip.

**`cmd/mcp-server-recall`**

* `TestConfigure_MaterializesBadgerManifest` — `sandboxConfigDir`, `ensureInitialized`
  + `configureCmd.RunE` with `RECALL_ENCRYPTION_KEY`, assert `MANIFEST` and
  `KEYREGISTRY` exist under `GetDBPath()`, and `GetDBPath()` is not CWD.
* `TestConfigure_RecreatesMissingDataDir` — write YAML, remove the data dir, run
  `configureCmd.RunE` (non-TTY), assert the data dir exists again. Today this fails:
  `ensureInitialized` returns because the YAML exists.
* `TestPurge_EmptyDBPathDoesNotDeleteCWD` — config with `dbpath: ""`, `chdir` to a
  scratch tree that contains a canary file, run `purgeCmd` with `--force`, assert the
  canary still exists and the command returned an error.

Run: `go test ./internal/config/ ./cmd/mcp-server-recall/ -count=1`. Record the
failures. Do not "fix" a test to pass against the old behaviour.

Commit this phase only after the tests exist and fail for the stated reason.

### Phase 2 — Empty `dbpath` and `purge` safety

This phase stops CWD resolution and the `RemoveAll(cwd)` footgun. It may still place
the default under the *current* config directory; Phase 3 switches Linux/Windows
greenfield defaults to the data directory.

In `GetDBPath`:

1. Trim space. If empty, use `DefaultDBPath()` (for this phase, joining ConfigDir +
   `DefaultDBName` is acceptable if Phase 3 lands immediately after; prefer calling
   the helper even if Darwin and Linux still coincide in the helper's first version).
2. Reject `"/"`, `"\\"`, and `filepath.VolumeName(p)+string(os.PathSeparator)` as
   resolved results; return the default instead of a volume root.
3. If the path is not absolute, `filepath.Abs` it only when it is non-empty.
4. Never return `os.Getwd()` as a successful database path.

In `purge.go`:

* After `GetDBPath()`, refuse if the path is CWD, a parent of CWD, a volume root, or a
  directory that does not contain a `MANIFEST`. The error must mention that empty
  `dbpath` is not a purge target.
* Do not satisfy the guard by comparing to `""` / `"."` only.

`config.New` / `paths.go`: on `UserConfigDir` error, do **not** set `configDir = "."`.
Surface the error (`slog.Error` + empty `appConfigDir` that `GetDBPath` then treats as
unusable, or make `New` still return a `*Config` whose `GetDBPath` errors — pick the
shape that keeps `New`'s current signature and makes `serve` / `configure` fail closed).
Preferred: keep `New`'s signature, store the resolve error, and have `GetDBPath` return
a non-CWD sentinel that `serve` and `purge` reject. Simplest correct shape: resolve
directories in helpers that return `(string, error)` and have `configure` / `serve`
fail if they cannot resolve.

Re-run Phase 1 tests. Empty-YAML CWD tests and the purge test must pass. The
`MANIFEST` configure test will still fail until Phase 4; leave it failing.

Commit.

### Phase 3 — Platform data directory

Add `internal/config/dirs.go` plus build-tagged bases:

```go
func ConfigDir() (string, error) // UserConfigDir + Name; error if unresolved or relative
func DataDir() (string, error)   // platform base + Name
func CacheDir() (string, error)  // UserCacheDir + Name
func DefaultDBPath() (string, error)
```

`DefaultDBPath` is `filepath.Join(DataDir(), DefaultDBName)`. It does not `Stat` the
filesystem and does not consult `ConfigDir`. A leftover store under the old
config-directory default is ignored.

Darwin `DataDir` base **is** `UserConfigDir`. A Darwin test with `XDG_DATA_HOME` and
`XDG_CONFIG_HOME` set to a temp dir must still resolve under
`$HOME/Library/Application Support`. Linux tests must assert `DataDir != ConfigDir`
when XDG is unset (`~/.local/share` vs `~/.config`) and that an absolute
`XDG_DATA_HOME` wins. A relative `XDG_DATA_HOME` is ignored (XDG spec).

Wire:

* `config.New` uses `ConfigDir` / `DefaultDBPath` / `CacheDir` as needed. Drop the
  duplicated `os.UserConfigDir` block.
* `v.SetDefault("dbPath", defaultDB)` using the helper.
* `cmd/.../paths.go` `configDirPath` calls `config.ConfigDir`. No stderr.
* `configure.go` mkdir and ReadDir use `Cfg.GetDBPath()` (or `DefaultDBPath` before
  `Cfg` is refreshed). Delete `filepath.Join(configDirPath(), config.DefaultDBName)`.
* `main.go` crash log uses `config.CacheDir()`. Fallback to `os.TempDir` only if
  `CacheDir` errors, still scoped by `config.Name`.
* Template comments: Linux data `~/.local/share/mcp-server-recall/.mcp_recall`;
  macOS Application Support (unchanged); Windows `%LocalAppData%\mcp-server-recall\.mcp_recall`.

`sandboxConfigDir` must keep returning a base that production code actually uses. After
this phase that is `ConfigDir()`'s parent or `ConfigDir()` itself; do not keep
returning `os.UserConfigDir()` if a helper wraps it with `Name`. Update callers that
join `base, config.Name` so they do not double-append.

Commit.

### Phase 4 — `configure` materializes the store

`ensureInitialized`:

1. Always `MkdirAll` the config dir (`0o700`) and the data dir from `DefaultDBPath()`
   / `GetDBPath()`. Mkdir failure returns an error.
2. If `recall.yaml` is missing or `--force` confirmed, write the template with
   `util.WriteFileAtomic` and mode `0o600`.
3. If `recall.yaml` exists and `--force` is false, **still** perform step 1. This is
   the missing-dir repair.

After the key is chosen and validated, and after the YAML is written:

4. If `$GetDBPath/MANIFEST` is missing, call `memory.NewMemoryStore` with that path,
   the key just written (possibly empty), `Cfg.SearchLimit()`, and `Cfg.BatchSettings()`,
   then `Close()`. Create the Bleve index dir only if `SearchEnabled` — calling
   `search.InitStorage` and closing is acceptable and keeps `serve` from treating
   search as a first-start special case; if that pulls too much of `serve` into
   `configure`, creating the Badger store alone is the acceptance bar.
5. If open fails because of a directory lock (reuse the substrings already tested in
   `openBadgerWithRetry`), print that a running server holds the store and do not
   treat it as a wizard failure.
6. Success text must print the resolved database path and whether `MANIFEST` now
   exists. Stop claiming "Database configured securely" when the directory is empty.

Interactive `ReadDir`: `errors.Is(err, os.ErrNotExist)` → empty entries, then continue
into the create path. Any other error still fails.

`configure` should also read `MCP_RECALL_ENCRYPTION_KEY` / `MCP_RECALL_ENCRYPTIONKEY`
in addition to `RECALL_ENCRYPTION_KEY` (first non-empty wins, document the order). Do
not add `--key`.

Phase 1's `TestConfigure_MaterializesBadgerManifest` and
`TestConfigure_RecreatesMissingDataDir` must pass.

Commit.

### Phase 5 — Documentation

README:

* Remove `--key` / `-k` from Step 3 and the flag table.
* Document `--force` and `--allow-unencrypted` as they exist.
* Document `RECALL_ENCRYPTION_KEY` and `MCP_RECALL_ENCRYPTION_KEY` (64 hex chars).
* Default config path is OS-specific, not unconditionally `~/.config/...`.
* Default `dbpath` empty means the **data** directory table from the MADR.
* Fix "32-character hex" to "64-character hex (32 bytes)".

Template comments: same path table and key length.

Do not change `apiport` behaviour. A one-line template comment that the YAML `apiport`
is currently unused (env `MCP_ENDPOINT_API_PORT` / default 47669) is allowed so
operators are not misled; do not wire it without a new MADR.

Commit.

### Phase 6 — Go 1.26.5 idioms on touched files

Only files already in this plan:

* `config.New`: `errors.AsType[viper.ConfigFileNotFoundError](err)` instead of
  `errors.As` + named variable.
* `0o700` / `0o600` in `configure.go` and `main.go` crash-log creation.
* Mapstructure pin test: a YAML file using lowercase `dbpath`, `exportdir`,
  `encryptionkey` round-trips through `New` into the struct fields. This also
  closes 0001's open question for the keys this change relies on.
* `config_extra_test.go`: `t.Setenv` if edited.
* No new `fmt.Fprintf(os.Stderr, "Warning:` in path helpers.
* `golint` per changed `.go` file; `gofmt -l` empty; `golangci-lint run -c .golangci.yml ./...`.

Do not drive-by replace `os.MkdirTemp` across `internal/memory` tests.

Commit.

## Verification

```bash
cd ~/gitrepos/go/mcp-server-recall

go test ./internal/config/ -count=1 -v
go test ./cmd/mcp-server-recall/ -count=1 -run 'TestConfigure|TestEnsure|TestPaths|TestPurge' -v
go test ./...
go build ./...
GOOS=linux GOARCH=amd64 go build ./...
GOOS=windows GOARCH=amd64 go build ./...
GOOS=darwin GOARCH=arm64 go build ./...

gofmt -l $(git diff --name-only --cached -- '*.go')
# golint per staged .go file, not mixed packages in one invocation
golangci-lint run -c .golangci.yml ./...
```

End-to-end against a **scratch HOME**, never the live config:

```bash
TMP=$(mktemp -d)
KEY=$(openssl rand -hex 32)
env -i HOME="$TMP" PATH="$PATH" \
  RECALL_ENCRYPTION_KEY="$KEY" \
  ./dist/mcp-server-recall-darwin-arm64 configure
CFG="$TMP/Library/Application Support/mcp-server-recall/recall.yaml"
DB="$TMP/Library/Application Support/mcp-server-recall/.mcp_recall"
grep -E '^dbpath:' "$CFG"                 # expect: dbpath: ""
test -f "$DB/MANIFEST"
test -f "$DB/KEYREGISTRY"
# CWD independence: run a trivial helper or `serve --help` is not enough;
# use go test TestGetDBPath_EmptyYAMLIsNotCWD as the assertion, and additionally:
test ! -f "$(pwd)/MANIFEST"

# Darwin XDG must not relocate
env -i HOME="$TMP" PATH="$PATH" \
  XDG_DATA_HOME="$TMP/xdg-data" XDG_CONFIG_HOME="$TMP/xdg-config" \
  ./dist/mcp-server-recall-darwin-arm64 configure --help >/dev/null
test -d "$TMP/Library/Application Support/mcp-server-recall"
test ! -d "$TMP/xdg-data/mcp-server-recall"
test ! -d "$TMP/xdg-config/mcp-server-recall"

# live host must be untouched
test -f "$HOME/Library/Application Support/mcp-server-recall/recall.yaml"
```

Cross-compile Linux behaviour is asserted by `dirs_linux_test.go` with `GOOS=linux`
test compilation where possible; full Linux execution is not available on this Darwin
host and must not be faked. Windows behaviour is asserted by `dirs_windows_test.go`
built with `GOOS=windows` (compile) and, if a Windows test run is unavailable, by
reviewing that `UserCacheDir` is the data base.

Acceptance criteria — all must hold:

1. `TestGetDBPath_EmptyYAMLIsNotCWD` passes from a working directory other than the
   data dir.
2. Fresh `configure` on a scratch HOME creates `MANIFEST` (and `KEYREGISTRY` when a
   key is provided) under the Darwin Application Support data dir.
3. `GetDBPath` with an explicit absolute YAML value returns that value.
4. Darwin with XDG set still uses Application Support; Linux helpers put data under
   `~/.local/share` or `$XDG_DATA_HOME`, not `~/.config`.
5. Windows helper uses `%LocalAppData%`, not `%AppData%`.
6. A leftover store under the Linux or Windows *config* directory is not selected;
   `GetDBPath()` with empty `dbpath` is always the data-dir default.
7. `purge --force` with `dbpath: ""` does not delete CWD and returns an error.
8. `configure` with YAML present and data dir missing recreates the data dir.
9. Mkdir failure is an error; the command does not print `Configuration Successful!`.
10. README no longer documents `--key` or a 32-character key; default paths are
    OS-specific.
11. `go test ./...`, three `GOOS` builds, `gofmt`, per-file `golint`, and
    `golangci-lint` are clean.
12. The live host's `recall.yaml` and `.mcp_recall` are byte-identical to before
    execution (this plan does not run live `configure`).

## Rollout and Rollback

**Rollout.** One commit per phase (1 tests, 2 empty-path/`purge`, 3 platform dirs,
4 materialize, 5 docs, 6 idioms), each passing the pre-commit gate on staged files.
Nothing is pushed or tagged without an explicit ask in that same turn. Do not run
`configure --force` on the live HOME. Do not replace the live encryption key.

**Rollback.** Revert the phase commits in reverse. No on-disk schema change. Darwin
rollback moves no files. Linux/Windows greenfield stores created under the new data
dir during testing of this plan live only in scratch `HOME`s.

**Live host.** Default Darwin path equals the existing live path. Empty `dbpath` on
this host would resolve to the same directory as the current absolute override.
Still do not rewrite the live YAML in this plan.

**Residual risk.** Opening Badger during `configure` while `serve` holds the lock is
handled by skip-and-report. Opening with a *changed* key against an existing
encrypted store remains dangerous; that is already warned in the interactive wizard
and is unchanged. Because migrations are out of scope, a non-greenfield Linux or
Windows machine that already stored data under the config directory will be opened
at the new empty data-dir path. That is accepted, not mitigated.

## Sequencing against other records

* 0001 recovery-narrowing follow-up: different files in `internal/config/config.go`
  (`recoverNullTaggedKey`). Do not combine.
* mcplib 0002 / `mcplib/paths`: blocked on a Darwin-XDG reconciliation. This plan
  implements recall-local helpers. A future swap is a new MADR, not a silent
  continuation of this one.
