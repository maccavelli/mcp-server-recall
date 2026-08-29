---
status: "accepted"
date: 2026-08-29
decision-makers: maccavelli
consulted: maccavelli
informed: maccavelli
---

# 0002-MADR: Treat empty dbpath as the platform data directory and materialize the datastore during configure

## Context and Problem Statement

`mcp-server-recall configure` reports success and does not produce a usable database. A fresh
run creates `recall.yaml` with `dbpath: ""` and an empty `.mcp_recall` directory next to that
file. It never opens BadgerDB, so `MANIFEST`, `LOCK`, `KEYREGISTRY`, and the value log are
absent. The server then resolves that empty `dbpath` to the process working directory, so the
real store — if it is created at all — appears in CWD, not in the OS-standard user data
location the template comments describe.

This is independent of
[0001-MADR-encryptionkey-yaml-tag-round-trip.md](0001-MADR-encryptionkey-yaml-tag-round-trip.md),
which decided how the encryption key is serialized. 0001 is implemented at `0f60745`. This
record is about *where* the store lives, *what empty `dbpath` means*, and *what `configure`
must create*.

### Reproduction (observed 2026-08-29, host macOS, installed `mcp-server-recall` `1.0.2-1-g0f60745`)

Isolated `HOME`, XDG variables unset, stdin not a TTY:

```bash
HOME=/tmp/recall-configure-bBV8 mcp-server-recall configure </dev/null
```

The command exited `0` and printed `Configuration Successful!` plus
`Database configured securely for unencrypted operations.` The filesystem then contained:

* `~/Library/Application Support/mcp-server-recall/recall.yaml` with `dbpath: ""`
* `~/Library/Application Support/mcp-server-recall/.mcp_recall/` — empty directory, no
  `MANIFEST`, no `LOCK`, no `KEYREGISTRY`, no `*.vlog`
* `~/Library/Caches/mcp-server-recall/crash.log` (from `main.go`, not from `configure`)

A second run with `RECALL_ENCRYPTION_KEY` set to a 64-character hex string wrote a quoted key
(the 0001 shape) and still left `dbpath: ""` and an empty `.mcp_recall`.

### Mechanism: empty `dbpath` shadows the default and becomes CWD

Three sites combine.

1. [`cmd/mcp-server-recall/config_template.go:16`](../cmd/mcp-server-recall/config_template.go)
   writes `dbpath: ""`. The surrounding comments say an empty value means "use the OS default
   config directory alongside this file."

2. [`internal/config/config.go:115`](../internal/config/config.go) sets
   `v.SetDefault("dbPath", filepath.Join(appConfigDir, DefaultDBName))`. Viper applies a default
   only when the key is unset. A present empty string is set. Confirmed against the scratch
   `recall.yaml` with viper `v1.21.0` (the version in `go.mod`):

   ```
   GetString(dbPath)=""
   resolved GetDBPath clone=<process cwd>
   equals_cwd=true
   equals_default=false
   ```

   With no config file at all, the same default *does* apply and resolves under the config
   directory. The template's empty key is what disables it.

3. [`internal/config/config.go:257-268`](../internal/config/config.go) `GetDBPath`:

   ```go
   p := c.state.DBPath
   if filepath.IsAbs(p) {
       return p
   }
   if abs, err := filepath.Abs(p); err == nil {
       return abs
   }
   return p
   ```

   `filepath.IsAbs("")` is false. On this host, `filepath.Abs("")` returns the current working
   directory (`/Users/saxsmith/gitrepos/go/mcp-server-recall` when run from the repo). There is
   no empty-string branch.

`ExportDir` already special-cases empty as `os.TempDir()`
([`config.go:275-283`](../internal/config/config.go)). `GetDBPath` does not. The two accessors
disagree on the meaning of an empty YAML scalar that the template documents as "use the OS
default."

[`cmd/mcp-server-recall/serve.go:125`](../cmd/mcp-server-recall/serve.go) then opens Badger at
`Cfg.GetDBPath()`. After a fresh `configure`, that is CWD. `NewMemoryStore`
([`internal/memory/badger.go:280`](../internal/memory/badger.go)) calls `os.MkdirAll(dbPath, 0o750)`
on that path and writes `MANIFEST` / `LOCK` / vlog *into the working directory*.

### Compounding effect on `purge`

[`cmd/mcp-server-recall/purge.go:20-23`](../cmd/mcp-server-recall/purge.go) refuses only `""`,
`"/"`, and `"."`. After `GetDBPath` has turned `""` into an absolute CWD, none of those match.
`purge --force` would therefore `os.RemoveAll(<cwd>)` — the entire working tree, not a recall
store. Confirmed: the viper probe printed `purge_guard_would_block=false` for the scratch
config whose `dbpath` is `""`.

This is not a hypothetical. It is the same empty-string path, one command later.

### `configure` never materializes a store

[`ensureInitialized`](../cmd/mcp-server-recall/configure.go) (`configure.go:198-238`):

* Returns immediately if `recall.yaml` already exists and `--force` is unset, *without*
  checking that the data directory exists.
* On first run, `os.MkdirAll` of `filepath.Join(configDirPath(), config.DefaultDBName)` at
  `0700`. A mkdir failure is a `pterm.Warning`, not an error; the YAML is still written and
  the command still reports success (`configure.go:227-229`).
* Writes the template via `os.WriteFile`, not `util.WriteFileAtomic` (the later encryption-key
  rewrite uses the atomic helper).
* Never calls `memory.NewMemoryStore`. Badger files are created only when `serve` runs.

The interactive branch then does:

```go
dbDir := filepath.Join(configDirPath(), config.DefaultDBName)
entries, dirErr := os.ReadDir(dbDir)
if dirErr != nil {
    return fmt.Errorf("read database directory: %w", dirErr)
}
```

(`configure.go:89-93`). `os.IsNotExist` is not handled. Reproduced: after a successful
`configure`, removing the empty `.mcp_recall` and re-running *non-interactively* succeeds and
does **not** recreate the directory (`ensureInitialized` sees the YAML and returns). The same
missing directory on a TTY is a hard error, so the wizard cannot repair the layout it is
supposed to own.

Both the mkdir and the ReadDir use `configDirPath()`, not `Cfg.GetDBPath()`. Even if
`GetDBPath` were fixed, these two sites would still inspect and create a different directory
unless they change with it.

### The live host currently works only because `dbpath` is not empty

The live file at
`~/Library/Application Support/mcp-server-recall/recall.yaml` contains an absolute
`dbpath` pointing at
`~/Library/Application Support/mcp-server-recall/.mcp_recall`. That store contains
`MANIFEST`, `LOCK`, `KEYREGISTRY`, a sparse `000001.vlog`, `00001.mem`, `DISCARD`,
`search_index/`, and `telemetry.ring` (`du -sh` = 3.1M). `recall.yaml.pre-repair` already
had the same absolute `dbpath` (and the `!!null` key recorded in 0001).

The *current* wizard does not write that absolute path. A new install gets `dbpath: ""` and
the CWD behaviour above. The live host is not a counter-example; it is a pre-existing
absolute override.

### OS path defaults: what the standard library gives, and where recall is wrong

Go 1.26.5 still has no `os.UserDataDir`. `go doc os` on this toolchain lists
`UserCacheDir`, `UserConfigDir`, and `UserHomeDir` only. From
`$GOROOT/src/os/file.go` (go1.26.5):

| API | Linux | macOS / Darwin | Windows |
|---|---|---|---|
| `UserConfigDir` | `$XDG_CONFIG_HOME` if absolute, else `$HOME/.config` | `$HOME/Library/Application Support` (**ignores XDG**) | `%AppData%` (Roaming) |
| `UserCacheDir` | `$XDG_CACHE_HOME` if absolute, else `$HOME/.cache` | `$HOME/Library/Caches` (**ignores XDG**) | `%LocalAppData%` |
| `UserDataDir` | *does not exist* | *does not exist* | *does not exist* |

Go's own test `TestUserConfigDirXDGConfigDirEnvVar` skips `darwin`, `windows`, and `plan9`
with `$XDG_CONFIG_HOME is effective only on Unix systems`
(`$GOROOT/src/os/os_test.go:3119-3122`).

This investigation host has XDG variables set *and* a live store under
`~/Library/Application Support`:

```
XDG_CONFIG_HOME=/Users/saxsmith/.config
XDG_DATA_HOME=/Users/saxsmith/.local/share
XDG_CACHE_HOME=/Users/saxsmith/.cache
XDG_STATE_HOME=/Users/saxsmith/.local/state
os.UserConfigDir()=/Users/saxsmith/Library/Application Support   # XDG ignored
```

Neither `~/.config/mcp-server-recall` nor `~/.local/share/mcp-server-recall` exists.

Standards for *data* (the Badger/Bleve store), distinct from *configuration* (`recall.yaml`):

* **XDG Base Directory Spec 0.8** — `$XDG_DATA_HOME` (default `$HOME/.local/share`) for
  user-specific data files; `$XDG_CONFIG_HOME` (default `$HOME/.config`) for configuration.
  Relative `$XDG_*` values must be ignored. Destination directories should be created with
  mode `0700`. A growing binary datastore in `~/.config` is a spec violation.
* **Apple File System Programming Guide** — `~/Library/Application Support` holds
  app-specific data that is not user documents; `~/Library/Caches` holds regenerable cache;
  `~/Library/Preferences` holds preference plists. XDG is not an Apple convention. Putting
  both `recall.yaml` and `.mcp_recall` under Application Support is consistent with Apple's
  layout.
* **Windows Known Folders** — `%AppData%` (`FOLDERID_RoamingAppData`) roams with a profile;
  `%LocalAppData%` (`FOLDERID_LocalAppData`) does not. A Badger value log and LSM must not
  roam. Go maps those folders to `UserConfigDir` and `UserCacheDir` respectively.

Recall today joins the database onto `UserConfigDir` in three places
(`config.go:115`, `configure.go:89`, `configure.go:226`). That is:

* **Linux:** a datastore in `~/.config/mcp-server-recall/.mcp_recall` — violates XDG.
* **Windows:** a datastore in `%AppData%\mcp-server-recall\.mcp_recall` — roams.
* **macOS:** a datastore in `~/Library/Application Support/mcp-server-recall/.mcp_recall` —
  correct for Apple, and it is where this host's live store already is.

[`cmd/mcp-server-recall/paths.go:12-24`](../cmd/mcp-server-recall/paths.go) documents those
config locations and falls back to `"."` when `UserConfigDir` fails, printing a warning on
stderr. `config.New` does the same fallback (`config.go:100-104`). Combined with
`v.AddConfigPath(".")` (`config.go:111`), a missing user config plus an empty `dbpath`
loads or creates state in CWD — the same class of failure as `filepath.Abs("")`.

`paths_test.go` is `//go:build !windows`. There is no Windows path test.

### Relationship to mcplib MADR 0002

[mcplib `0002-MADR-xdg-compliant-user-paths.md`](../../mcplib/docs/0002-MADR-xdg-compliant-user-paths.md)
(accepted 2026-08-23) decided a shared `mcplib/paths` package with platform-native defaults
**and** `XDG_*` honoured on every platform, including macOS. Its plan has not been executed:
`mcplib` at the version this module requires (`v1.0.1`) has no `paths` package.

Two of that record's claims do not hold on this host:

1. "On macOS the resolved paths are unchanged, so this host needs no migration." That is
   true only when `XDG_*` are unset. They are set here. Honouring `XDG_DATA_HOME` would
   resolve the database to `~/.local/share/mcp-server-recall/.mcp_recall` and look like a
   new empty install next to a live store under Application Support.
2. "The operator has confirmed the existing recall database is empty." The live directory
   contains `KEYREGISTRY`, `MANIFEST`, a 128 MiB sparse vlog, a Bleve `search_index`, and a
   `telemetry.ring`. Emptiness of *records* was not re-verified in this pass; emptiness of
   *the directory* is false.

Waiting for `mcplib/paths` therefore both blocks a working `configure` and, if adopted as
written, would relocate this host's store. Recall must decide its own mapping now. Adopting
`mcplib/paths` later is a separate change, and only after that mapping is reconciled with
this host.

### Documentation already disagrees with the binary

* README "Step 3" and the `configure` flag table document `--key` / `-k`
  ([README.md:117](../README.md), [README.md:206](../README.md)). The installed binary's
  `configure --help` exposes only `--force` / `-f` and `--allow-unencrypted`. There is no
  `--key` flag in `configure.go`.
* README default config path is `~/.config/mcp-server-recall/recall.yaml`
  ([README.md:371](../README.md)), which is the Linux path, published unconditionally.
* README and the template comment call the key a "32-character hex" key
  ([README.md:386](../README.md), `config_template.go:19`). The wizard requires 64 hex
  characters (32 bytes). 0001 already fixed the length check; the comments were not updated.
* `configure` reads `RECALL_ENCRYPTION_KEY`. `config.New` binds
  `MCP_RECALL_ENCRYPTION_KEY` / `MCP_RECALL_ENCRYPTIONKEY` (`config.go:95`). They are not
  the same variable. A caller following the README vs a caller following the env prefix
  will not hit the same path.
* Template `apiport: 18001` is never read. `ResolveAPIPort` (`config.go:306-315`) uses only
  `MCP_ENDPOINT_API_PORT`, else `47669`. The wizard writes a port the server ignores.
* `State.DBPath` is tagged `mapstructure:"dbPath"` (`config.go:58`) while the file key is
  `dbpath`. Viper lowercases, so this is believed to work; it is untested, the same class
  of mismatch 0001 already flagged for `encryptionKey`.

These are not the root cause of the missing database, but they are why `configure` cannot
be operated from the README as written.

### Why the tests did not catch it

* `TestEnsureInitialized_FirstRun` (`init_test.go:39-41`) asserts only that the file
  contains the substring `dbpath:`. An empty value satisfies that.
* `TestConfigureCommand_Sandboxed` asserts a blank encryption key, not a store, not
  `GetDBPath`, and not `MANIFEST`.
* `TestConfig_AllAccessors` (`accessors_test.go:61-64`) asserts `GetDBPath()` is non-empty.
  CWD is non-empty.
* `paths_test.go` never runs on Windows and never asserts a data directory distinct from
  the config directory.
* No test opens a config written by `configure` and compares `GetDBPath()` to CWD.
* No test runs `configure` and asserts Badger files exist.

## Decision Drivers

* An empty `dbpath` that means "OS default" in the comments and "CWD" in the accessor is
  a lying configuration surface. MCP servers are launched with unpredictable working
  directories (project root, `/`, launchd).
* `purge` deleting CWD is an unacceptable consequence of that lie.
* `configure` reporting success while leaving an empty directory is the operator-visible
  failure: there is no initial database.
* A Badger store is application data, not configuration. Linux and Windows already have
  distinct locations for those; macOS uses Application Support for both.
* This host has a live store under `~/Library/Application Support` *and* XDG variables
  set. Honouring XDG on Darwin would hide that store. Apple's standard, and Go 1.26.5's
  Darwin `UserConfigDir`, ignore XDG. Platform-native defaults must not relocate this
  host.
* Go 1.26.5 still has no `UserDataDir`. The data path must be constructed from the APIs
  that exist, per OS, not invented as a fourth copy of `UserConfigDir`.
* `mcplib/paths` is the right *fleet* home for this logic, but it does not exist yet and
  its accepted macOS-XDG behaviour is unsafe on this host. Recall cannot wait, and must
  not import that behaviour unchanged.
* All installs are assumed greenfield (operator constraint, 2026-08-29). Locating,
  copying, or preferring a store left under a previous config-directory default is out
  of scope. An explicit absolute `dbpath` in YAML is a configured override, not a
  migration, and still wins.

## Considered Options

* **Fix empty-string only, keep the database under the config directory** — make
  `GetDBPath` treat `""` like `ExportDir` treats `""`, have `configure` open and close
  Badger at `filepath.Join(configDir, DefaultDBName)`, leave Linux `~/.config` and
  Windows Roaming in place.
* **Recall-local platform resolvers, empty `dbpath` means data dir, `configure`
  materializes the store** — small `ConfigDir` / `DataDir` / `CacheDir` helpers in this
  repository using Go 1.26.5 stdlib per OS; do not honour XDG on Darwin; Windows data
  under `%LocalAppData%`; Linux data under `$XDG_DATA_HOME` or `~/.local/share`;
  `configure` creates and opens the store at `GetDBPath()`.
* **Wait for `mcplib/paths` and adopt it as specified in mcplib MADR 0002** — honour
  `XDG_*` on every platform, including macOS, then convert this repository.
* **Materialize an absolute `dbpath` into `recall.yaml` during `configure`** — write the
  resolved path into the file so viper cannot see `""`.
* **Honour XDG on Darwin as well as Linux** — same mapping as mcplib 0002, implemented in
  recall.

## Decision Outcome

Chosen option: **"Recall-local platform resolvers, empty `dbpath` means data dir,
`configure` materializes the store"**, because that is the smallest change that makes
`configure` produce a real database at the location each OS actually uses, without
waiting on an unimplemented library and without relocating this host's store.

Resolution table for application-scoped directories (`filepath.Join(base, config.Name)`):

| Resolver | Linux | macOS | Windows |
|---|---|---|---|
| Config (YAML) | `os.UserConfigDir()` (`$XDG_CONFIG_HOME` or `~/.config`) | `os.UserConfigDir()` (`~/Library/Application Support`); **XDG ignored** | `os.UserConfigDir()` (`%AppData%`) |
| Data (Badger + Bleve) | `$XDG_DATA_HOME` if set and absolute, else `$HOME/.local/share` | `os.UserConfigDir()` (same Application Support tree as config) | `os.UserCacheDir()` (`%LocalAppData%`) |
| Cache (crash log) | `os.UserCacheDir()` | `os.UserCacheDir()` (`~/Library/Caches`) | `os.UserCacheDir()` (`%LocalAppData%`) |

Default `dbpath` is `filepath.Join(DataDir, config.DefaultDBName)`. An empty or
whitespace-only `dbpath` in YAML or in the unmarshaled struct **must** resolve to that
default. `filepath.Abs("")` is never an acceptable database path. `DefaultDBPath` does
not inspect the filesystem and does not look under `ConfigDir` for a leftover store.

Installs are greenfield. A machine that already has data under the old
config-directory default (`~/.config/mcp-server-recall/.mcp_recall` on Linux,
`%AppData%\mcp-server-recall\.mcp_recall` on Windows) is outside this record: the
server will open the new data-dir path and that old directory is ignored. Darwin's
config and data locations are the same Application Support path, so the greenfield
default coincides with today's layout there.

`configure` / `ensureInitialized` must:

1. Create the config directory and write `recall.yaml` (atomic) as today.
2. Resolve the database path through the same `GetDBPath` the server uses — never a
   parallel `filepath.Join(configDirPath(), DefaultDBName)`.
3. `MkdirAll` that path at `0o700`; failure is a returned error, not a warning.
4. If `recall.yaml` already exists, still ensure the data directory exists. Absence of
   the YAML is not the only first-run condition.
5. If the directory has no Badger `MANIFEST`, open `memory.NewMemoryStore` with the key
   that is about to be written, then close it, so `MANIFEST` / `LOCK` / `KEYREGISTRY`
   exist before the command prints success. If the directory is lock-contended because
   `serve` is running, skip the open, keep the directory, and report that the running
   server holds the store — do not fail the YAML write, and do not claim a new store
   was created.
6. Treat `ReadDir` `os.IsNotExist` as empty, not as a wizard-ending error.

Do not write a resolved absolute `dbpath` into the YAML. Empty continues to mean "platform
default," which is what the comments already say; the accessor must finally implement
that. Existing absolute values, including this host's, continue to win.

Do not honour `XDG_CONFIG_HOME` / `XDG_DATA_HOME` on Darwin. That matches Go 1.26.5 and
Apple's layout. Linux continues to honour XDG because the stdlib and the spec both
require it. When `mcplib/paths` exists, replacing the local helpers is a new MADR; it
must not silently start honouring XDG on Darwin.

`UserConfigDir` / `UserCacheDir` failure must not fall back to `"."`. `configure` and
`config.New` should return or log a hard error and refuse to create a store in CWD.
`v.AddConfigPath(".")` is retained only as a last-resort override *after* the user config
dir; it must not change the empty-`dbpath` default, which stays the data directory.

`purge` must refuse any path that is CWD, a volume root, or not inside the app data/config
directory and not an explicit absolute `dbpath` whose directory contains a Badger
`MANIFEST`. `RemoveAll` of a resolved empty string is not an acceptable outcome.

### Consequences

* Good, because a fresh `configure` leaves a Badger store at a stable, OS-correct path
  that `serve` will open regardless of CWD.
* Good, because empty `dbpath` finally means what the template and README say.
* Good, because this host's live absolute `dbpath` is unchanged, and Darwin's default
  *is* that same Application Support path even for new empty values.
* Good, because Linux data leaves `~/.config` and Windows data leaves Roaming.
* Good, because `purge` can no longer delete the working tree as a consequence of a
  blank YAML key.
* Neutral, because `mcplib/paths` remains the long-term fleet home; this repository
  carries a small helper until that package exists *and* Darwin XDG is resolved.
* Neutral, because YAML keeps `dbpath: ""` and operators who want a pinned path still
  set an absolute value, as this host already does.
* Neutral, because greenfield scope means a pre-existing Linux `~/.config` or Windows
  Roaming store is not opened, not copied, and not warned about. That is accepted: this
  record does not serve those layouts.
* Bad, because honouring XDG on Darwin — the behaviour mcplib 0002 accepted — is
  explicitly not done here. Fleet conversion must not copy that Darwin behaviour onto
  this host as if it were a no-op.
* Bad, because opening Badger during `configure` can fail if `serve` holds the lock.
  The skip-and-report path is required so the wizard remains usable against a running
  server.

### Confirmation

The decision is confirmed when all of the following hold:

1. A test writes a config via `configure` with `dbpath: ""`, loads it through
   `config.New`, and asserts `GetDBPath()` equals the platform data-dir default and
   is not `os.Getwd()`.
2. That test is run from a working directory that is *not* the config/data dir, so CWD
   leakage is actually observable.
3. After `configure` on a scratch `HOME`, the data directory contains a Badger
   `MANIFEST`. With a 64-character key it also contains `KEYREGISTRY`.
4. `GetDBPath()` with no config file, with `dbpath: ""`, and with an explicit absolute
   path are all tested. The absolute path wins.
5. On Darwin, `XDG_DATA_HOME` / `XDG_CONFIG_HOME` being set does not change ConfigDir
   or DataDir. On Linux, `XDG_DATA_HOME` does change DataDir, and DataDir is not under
   ConfigDir.
6. A Windows-tagged test asserts DataDir is under `%LocalAppData%` (or the sandboxed
   equivalent), not `%AppData%`.
7. `purge` against a config with `dbpath: ""` refuses, and a test asserts it does not
   call `RemoveAll` on CWD.
8. Interactive `configure` with YAML present and the data directory missing creates the
   directory rather than returning `read database directory`.
9. `gofmt`, per-file `golint`, `golangci-lint`, `go test ./...`, and
   `GOOS=linux|windows|darwin go build ./...` are clean.

## Pros and Cons of the Options

### Fix empty-string only, keep the database under the config directory

* Good, because it is the smallest patch that stops CWD resolution and lets
  `configure` create a real store on this macOS host.
* Good, because this host's default path does not change.
* Neutral, because Darwin is already correct under this option.
* Bad, because Linux continues to grow a binary store in `~/.config`.
* Bad, because Windows continues to roam a Badger value log through `%AppData%`.
* Bad, because it leaves the three `configDir + DefaultDBName` sites as the definition
  of "the database," which is the layout mcplib 0002 already rejected for good reasons.

### Recall-local platform resolvers, empty `dbpath` means data dir, `configure` materializes the store

* Good, because it uses Go 1.26.5's actual APIs per OS rather than pretending
  `UserConfigDir` is a data directory everywhere.
* Good, because Darwin stays on Application Support, matching both the stdlib and this
  host.
* Good, because `configure` and `serve` share one resolver, so the wizard cannot
  initialize a directory the server will not open.
* Neutral, because a few tens of lines live in this repository until `mcplib/paths`
  exists.
* Bad, because a second, later conversion to `mcplib/paths` is still required for fleet
  consistency — but only after Darwin XDG is settled.

### Wait for `mcplib/paths` and adopt it as specified in mcplib MADR 0002

* Good, because one implementation would eventually serve the fleet.
* Bad, because the package does not exist, so `configure` stays broken until a
  different repository ships a minor release.
* Bad, because honouring XDG on Darwin would resolve this host's data dir to
  `~/.local/share/mcp-server-recall` while the live store remains under Application
  Support. Under greenfield scope that would still be the wrong Darwin default.
* Bad, because it couples a working `configure` to an unresolved fleet decision.

### Materialize an absolute `dbpath` into `recall.yaml` during `configure`

* Good, because viper would no longer see `""`, so today's `GetDBPath` would stop
  returning CWD without an accessor change.
* Good, because the live host already works in this shape.
* Bad, because the file becomes user- and machine-specific; copying a dotfiles-style
  config, changing `HOME`, or restoring a backup onto another account points at the
  wrong disk.
* Bad, because it does not fix Linux/Windows placement, the empty-directory mkdir, the
  `ReadDir` hard error, or the `purge` guard, unless those are done anyway.
* Bad, because the template comments would have to stop saying empty means default,
  which is the portable form operators actually want.

### Honour XDG on Darwin as well as Linux

* Good, because an operator who set `XDG_DATA_HOME` on a Mac stated an intent.
* Good, because it would match mcplib 0002 as written.
* Bad, because it is not Apple's standard and not what Go 1.26.5 does.
* Bad, because on *this* host those variables are set for other tooling, and honouring
  them would abandon the live Application Support store. Intent is not established
  merely by the variables being present.

## More Information

Implementation steps, verification commands, and rollback are in
[0002-PLAN-configure-os-native-datastore-init.md](0002-PLAN-configure-os-native-datastore-init.md).

### Amendment (2026-08-29): greenfield only

The operator constrained this record to greenfield installs. The original draft of the
decision outcome included a Linux/Windows legacy lookup (prefer an existing store under
`ConfigDir` when `DataDir` did not yet exist). That lookup, any copy or move of an old
store, and any "existing install" detection are **out of scope**. `DefaultDBPath` is a
pure function of the platform table above. The live host's absolute `dbpath` remains a
configured override and is unchanged by that constraint.

### Relationship to other records

* [0001-MADR-encryptionkey-yaml-tag-round-trip.md](0001-MADR-encryptionkey-yaml-tag-round-trip.md)
  (accepted) — how the encryption key is serialized. Implemented. This record does not
  reopen that decision. The remaining 0001 follow-up (narrow `recoverNullTaggedKey`)
  stays with 0001 and must not be mixed into this change set.
* [mcplib 0002-MADR-xdg-compliant-user-paths.md](../../mcplib/docs/0002-MADR-xdg-compliant-user-paths.md)
  (accepted, unimplemented) — fleet-wide path helpers. This record disagrees with that
  record's Darwin XDG behaviour as applied to this host, and does not wait for the
  package. Reconcile before any later swap.

### Go 1.26.5 idiom and standards gaps in the same code

These are in scope for the plan's idiom phase because they sit on the files this change
already has to touch. They are not separate architectural choices.

* `errors.AsType` (Go 1.26) should replace the `errors.As` dance on
  `viper.ConfigFileNotFoundError` in `config.New`.
* Library-ish path helpers must return errors, not `fmt.Fprintf(os.Stderr, "Warning: …")`
  (`paths.go:20`).
* Octal literals should be consistent `0o700` / `0o600`. `configure.go` uses `0700` /
  `0600`; `badger.go` uses `0o750` for the same class of directory; `main.go` uses
  `0755` / `0666` for the crash log.
* `ensureInitialized` must use `util.WriteFileAtomic` like the encryption-key rewrite.
* `t.Setenv` rather than `os.Setenv` in tests that this change extends
  (`config_extra_test.go` still uses `os.Setenv`).
* Pin `mapstructure` tags against the YAML keys with a test (0001's open question for
  `encryptionKey`, same risk for `dbPath` / `exportDir`).
* `configure` must read the same encryption-key environment variables `config.New`
  binds, with `RECALL_ENCRYPTION_KEY` kept as an alias so 0001's tests and operators
  keep working.
* README `--key`, "32-character hex", and `~/.config/...` default path must match the
  binary. Either implement `--key` as an alias of `RECALL_ENCRYPTION_KEY` or delete it
  from the README; this record prefers documenting the env var and flags that exist,
  not adding a third key channel.
* `apiport` in the YAML is written and ignored. Recording the gap is required; changing
  `ResolveAPIPort` to read YAML is **out of scope** unless a follow-up MADR says so,
  because it changes the listening port of a running fleet.

### Evidence

* Installed binary: `/Users/saxsmith/.local/bin/mcp-server-recall`,
  `mcp-server-recall version 1.0.2-1-g0f60745`, Mach-O arm64, dated 2026-08-23.
* Scratch `HOME=/tmp/recall-configure-bBV8`: `configure` exit 0, `dbpath: ""`, empty
  `.mcp_recall`, no `MANIFEST`.
* Scratch `HOME=/tmp/recall-configure-key-SNYd` with `RECALL_ENCRYPTION_KEY`: quoted
  64-hex key, still `dbpath: ""`, still empty `.mcp_recall`.
* Viper probe of that YAML: `GetString(dbPath)=""`, resolved path equals CWD, default
  path unused, `purge_guard_would_block=false`.
* Viper probe of the live YAML: absolute `dbpath` under Application Support, equals the
  viper default path, not CWD.
* Viper probe with no YAML: default path under Application Support is used.
* `filepath.Abs("")` on this host equals CWD (Go 1.26.5).
* `os.UserConfigDir()` on this host equals
  `/Users/saxsmith/Library/Application Support` while `XDG_CONFIG_HOME` is
  `/Users/saxsmith/.config`.
* `go doc os.UserDataDir` does not exist on go1.26.5.
* Live store listing (2026-08-29): `MANIFEST`, `LOCK`, `KEYREGISTRY`, `000001.vlog`,
  `00001.mem`, `DISCARD`, `search_index/`, `telemetry.ring`.
* After deleting a scratch `.mcp_recall` and re-running non-interactive `configure`,
  the directory is not recreated.
* `configure --help` lists `--allow-unencrypted` and `--force` only.
* mcplib tree at `~/gitrepos/go/mcplib` has no `paths/` package.
* `$GOROOT/src/os/file.go:544-573` (go1.26.5) for Darwin ignoring XDG and Windows
  using `%AppData%`.
* XDG Base Directory Specification 0.8,
  https://specifications.freedesktop.org/basedir-spec/latest/
