---
status: "accepted"
date: 2026-08-29
decision-makers: maccavelli
consulted: maccavelli
informed: maccavelli
---

# 0001-MADR: Persist the recall encryption key as an explicitly typed YAML string and never discard it silently

## Context and Problem Statement

`mcp-server-recall configure` writes an encryption key that the server cannot read back. The
wizard reports success, the file is written, and the key is present in the file — yet every
subsequent load discards it. To an operator the symptom is "configure is failing to write out
its config": the key never takes effect, so it looks as though nothing was saved.

The defect is a YAML *type tag* that survives an in-place node edit.

### Reproduction (observed 2026-08-23, host macOS, `mcp-server-recall` v1.0.2)

The live configuration at
`~/Library/Application Support/mcp-server-recall/recall.yaml` contained, at line 15:

```yaml
encryptionkey: !!null 8e71e699…dacc0
```

Running the wizard emitted, on stderr:

```
WARN error parsing recall.yaml error="While parsing config:
  yaml: cannot decode !!str `8e71e699…dacc0` as a !!null"
```

`While parsing config:` is viper's prefix, produced by `v.ReadInConfig()` at
[`internal/config/config.go:169`](../internal/config/config.go). The node carries an explicit
`!!null` tag while holding a 64-character hex string; a `!!null`-tagged node may only be null,
so typed decoding fails.

### Mechanism

Three code sites combine:

1. [`cmd/mcp-server-recall/config_template.go:20`](../cmd/mcp-server-recall/config_template.go)
   writes `encryptionkey: %s`. On a fresh install the substitution is empty, producing
   `encryptionkey:` — which YAML resolves to a scalar node tagged `!!null`.

2. [`cmd/mcp-server-recall/configure.go:144-148`](../cmd/mcp-server-recall/configure.go)
   performs AST surgery that assigns **only the value**:

   ```go
   if keyNode.Value == "encryptionkey" {
       valNode.Value = input   // valNode.Tag is still "!!null"
       keyFound = true
       break
   }
   ```

   `valNode.Tag` is never cleared, so `yaml.Marshal` at line 160 re-emits the stale tag
   alongside the new string: `encryptionkey: !!null <64-hex>`.

3. On the next load, viper's typed decode rejects that node, `Cfg.EncryptionKey()`
   ([`internal/config/config.go:287`](../internal/config/config.go)) returns `""`, and
   [`internal/memory/badger.go:427`](../internal/memory/badger.go) takes the
   `encryptionKey == ""` branch — opening the store unencrypted.

The writer therefore produces a file its own reader refuses. The round trip is broken, not the
write.

### Why the existing recovery path does not catch it

`configure.go:44` unmarshals into an untyped `yaml.Node`. The malformed file is *syntactically*
valid YAML, so that call succeeds and the recovery branch at `configure.go:46` — which would
rename the file to `.bak` and regenerate from the template — never runs. This was confirmed
empirically: after the failing run, no `.bak` file existed in the configuration directory.

The failure is only visible to the *typed* decode, which happens in a different package and is
logged at WARN rather than returned as an error.

### Compounding effect on the wizard's own logic

Because the typed load fails, `existingKey := Cfg.EncryptionKey()` at `configure.go:58`
evaluates to `""`. The wizard consequently believes no key is configured even when one is
present in the file, so the "Valid encryption key already mapped in configuration" branch at
line 70 and the "Changing the encryption key will render existing database contents
irrecoverable!" warning at line 81 are both suppressed. A corrupted key silences the very
guardrail that protects the key.

### Second defect: silent key destruction when not attached to a terminal

`configure.go:66-68` handles a non-interactive stdin by discarding the key outright:

```go
} else if !term.IsTerminal(int(os.Stdin.Fd())) {
    pterm.Warning.Println("Non-interactive terminal detected. Proceeding without encryption.")
    input = ""
}
```

The command then writes a blank key, prints `Configuration Successful!`, and exits `0`. Any
non-interactive caller — a provisioning script, CI, a launchd job, an agent — silently strips
an existing encryption key while reporting success. This was reproduced accidentally on the
live host during diagnosis; the key was recoverable only because the file had been copied
beforehand.

This is distinct from the tag defect but shares its consequence — key loss with a success
report — and the two are therefore recorded together.

### Why the tests did not catch either defect

`cmd/mcp-server-recall/configure_test.go:54-55` asserts only that the artifact contains an
*explicitly blank* key:

```go
if !bytes.Contains(content, []byte("encryptionkey: \"\"")) && … {
    t.Errorf("Sanbox configuration artifact did not contain an explicitly blank encryptionkey entry")
}
```

The suite runs non-interactively, so it exercises exactly the `input = ""` path and asserts the
outcome that path produces. No test writes a non-empty key, and no test loads a written file
back through `config.Load`. The blank-key case is the only one covered, and it is the one case
where the tag bug cannot manifest.

## Decision Drivers

* An encryption key that fails to persist is silent data-at-rest exposure: the store is opened
  unencrypted while the operator believes it is encrypted.
* A wizard that reports `Configuration Successful!` while destroying a key is actively
  misleading; exit code `0` makes it undetectable to automation.
* The failure is invisible at the layer that causes it — node surgery succeeds, and the error
  surfaces later, in another package, as a WARN.
* Any fix must survive keys that are ambiguous under YAML's implicit typing rules, not merely
  the key that was observed.
* The repository has existing test infrastructure for `configure`; the gap is coverage, not
  tooling.

## Considered Options

* **Clear the stale tag in place** — set `valNode.Tag = ""` after assigning the value and let
  the encoder infer the type.
* **Replace the value node with an explicitly typed string node** — assign a fresh
  `yaml.Node{Kind: ScalarNode, Tag: "!!str", Style: DoubleQuotedStyle}`, on both the update and
  insert paths, and quote the template substitution.
* **Abandon AST surgery and marshal a typed struct** — load the config into `State`, set the
  field, and re-marshal the whole document.
* **Move the key out of the YAML file entirely** — store it in the OS keychain or require
  `RECALL_ENCRYPTION_KEY`, leaving no key in `recall.yaml`.

## Decision Outcome

Chosen option: **"Replace the value node with an explicitly typed string node"**, combined with
refusing to overwrite an existing key when stdin is not a terminal.

An empty tag would let the encoder infer the type, which is correct for the observed key but
not for every valid key. A 64-character hex key may consist solely of decimal digits; emitted
unquoted, YAML's implicit resolver types such a scalar as a number, and it would round-trip as
an integer or overflow rather than the intended string. Pinning `!!str` with an explicit quoted
style makes the round trip deterministic for all 16^64 possible keys instead of the overwhelming
majority of them. The same treatment is applied to the insert path at `configure.go:152-155`,
which has the identical latent flaw, and to the template substitution at
`config_template.go:20`.

The non-interactive branch is changed to abort rather than blank the key when a key already
exists, so that no non-interactive invocation can destroy a key while reporting success.
Callers that legitimately want an unencrypted store retain an explicit opt-in.

### Consequences

* Good, because a key written by the wizard is readable by the server, which is the entire
  point of the command.
* Good, because the fix is confined to the wizard's serialization and one branch of its control
  flow; no configuration schema, file location, or on-disk database format changes.
* Good, because pinning `!!str` removes a latent class of failure — digit-only keys — that no
  observed run has yet hit and that would have been reintroduced by the minimal fix.
* Good, because aborting non-interactively converts a silent, successful-looking key deletion
  into a loud failure that automation can detect.
* Neutral, because existing correct configurations are unaffected: a config whose key already
  parses is rewritten to an equivalent quoted form.
* Bad, because existing corrupted configurations are not repaired by this change alone. A file
  already containing `!!null <hex>` continues to fail typed decode until rewritten. The plan
  therefore includes a repair path.
* Bad, because scripted callers that currently rely on `configure` succeeding non-interactively
  to produce an unencrypted config will begin failing unless they adopt the explicit opt-in.
  This is intended, but it is a behavioural break.

### Confirmation

The decision is confirmed when all of the following hold:

1. A test writes a non-empty key through `configure`, loads the file back through
   `config.Load`, and asserts the loaded key equals the written key.
2. That test is parameterised over at least one digit-only 64-character key, to cover implicit
   numeric typing.
3. A test asserts that a non-interactive invocation against a config with an existing key exits
   non-zero and leaves the file byte-identical.
4. `grep -c '!!null' recall.yaml` returns `0` for a config written by the fixed wizard that
   carries a key.
5. On the live host, `mcp-server-recall serve` logs `encrypted=true`
   ([`internal/memory/badger.go:336`](../internal/memory/badger.go)) after reconfiguration.

## Pros and Cons of the Options

### Clear the stale tag in place

* Good, because it is a one-line change at the exact site of the defect.
* Good, because it requires no restructuring and carries minimal review burden.
* Neutral, because it leaves the insert path at `configure.go:152-155` untouched; that path
  currently emits an untagged node, which is correct today only by accident.
* Bad, because an inferred type is wrong for digit-only keys, which would silently round-trip as
  numbers.

### Replace the value node with an explicitly typed string node

* Good, because the emitted type is pinned rather than inferred, so every valid key round-trips.
* Good, because the update and insert paths become identical, removing the asymmetry that made
  one path correct and the other not.
* Neutral, because it is marginally more code than clearing the tag.
* Bad, because it retains AST surgery, so future edits to other fields could reintroduce a
  tag-related defect elsewhere in the document.

### Abandon AST surgery and marshal a typed struct

* Good, because it makes an entire class of tag and style defects structurally impossible.
* Bad, because `recall.yaml` carries extensive explanatory comments — visible throughout
  `config_template.go` — and marshalling from a struct discards every one of them.
* Bad, because it silently drops any key present in the file but absent from `State`, converting
  a serialization bug into a data-loss bug with a wider blast radius.

### Move the key out of the YAML file entirely

* Good, because a key that is never serialized to YAML cannot be corrupted by YAML typing.
* Good, because it removes a long-lived secret from a plaintext file readable by any process
  running as the user.
* Bad, because it is a substantially larger change touching provisioning, the launchd service
  definition, and every existing deployment.
* Bad, because it does not address the non-interactive destruction path, which is independent of
  where the key is stored.
* Neutral, because it remains a defensible future direction; this record does not preclude it.

## More Information

Implementation steps, verification commands, and rollback are in
[0001-PLAN-encryptionkey-yaml-tag-round-trip.md](0001-PLAN-encryptionkey-yaml-tag-round-trip.md).

### Post-implementation review (2026-08-29)

This section records facts learned after the decision and implementation landed. It does not
retroactively change the rationale above.

#### Implementation and repository state

* Commit `0f6074536a14db0176af598510265b4cac28db8d` implemented the decision and added
  this MADR and its PLAN in the same commit on 2026-08-23.
* That commit modified `cmd/mcp-server-recall/config_template.go`,
  `cmd/mcp-server-recall/configure.go`, `cmd/mcp-server-recall/configure_test.go`,
  `internal/config/config.go`, and `internal/config/config_test.go`.
* As observed on 2026-08-29, `go test ./...`, `go build ./...`,
  `GOOS=windows go build ./...`, `GOOS=linux go build ./...`, per-file `golint` on all five
  changed Go files, and
  `golangci-lint run -c .golangci.yml ./...` pass. `gofmt -l` produces no output for those
  files.
* Repository evidence does not establish that the live-host repair ran, that the datastore was
  empty, that `serve` logged `encrypted=true`, or that a `v1.0.3` release was created. Those
  claims remain unconfirmed.

#### Recovery predicate is not as narrow as intended

The implementation plan required legacy recovery to trigger only when the Viper error named
both `encryptionkey` and `!!null`. That condition cannot be met by the reproduced error quoted
in this MADR: the error contains `!!null` and the scalar value but does not identify the mapping
key.

The implementation at `internal/config/config.go:409-425` instead checks only whether the error
text contains `!!null`. It then scans the file text for any top-level-looking line beginning with
`encryptionkey:`, removes a leading `!!null` if present, and returns the remaining value. It does
not establish that:

* the decode error came from `encryptionkey` rather than another field;
* the encryption-key node itself carries the `!!null` tag;
* the recovered value is exactly 64 hexadecimal characters; or
* the line belongs to the parsed top-level YAML mapping rather than a nested mapping or scalar
  block whose text happens to begin with `encryptionkey:`.

Consequently, an unrelated YAML decode error containing `!!null` can cause a normally tagged or
otherwise invalid encryption-key line to be injected through `v.Set`. The original parse warning
still appears, but the recovery warning incorrectly identifies the file as carrying a legacy
null-tagged key. The positive test at `internal/config/config_test.go:34-56` proves recovery of
the observed legacy shape; it does not constrain these false-positive cases.

The required follow-up is to parse the raw document as a `yaml.Node`, locate the top-level
`encryptionkey` value node, require that node's tag to be exactly `!!null`, and validate a
non-empty 64-character hexadecimal value before calling `v.Set`. Negative tests must cover an
unrelated `!!null` failure, a correctly tagged encryption key alongside an unrelated failure,
a nested `encryptionkey`, and an invalid legacy value. This is a source-code follow-up and was
not performed by the documentation-only review.

#### Decision-boundary limitation

The original record groups two distinct policies: deterministic YAML serialization and refusal
to erase an existing key non-interactively. The considered options compare serialization
strategies only; they do not compare alternatives for non-interactive behavior. The PLAN also
introduced load-time legacy recovery without recording alternatives in this MADR. The accepted
serialization decision remains clear, but the rationale for the clobber guard and recovery
policy is incomplete. Any future change to either policy requires a new MADR rather than a
silent rewrite of this accepted record.

#### Workflow deviation

The associated PLAN specified three phase commits and repository instructions require explicit
plan approval before implementation. Git history shows the MADR, PLAN, tests, and implementation
arriving together in the single commit named above. Git history cannot establish whether an
external review or approval occurred, and the promised per-phase commit cadence did not occur.
This is a historical process deviation; correcting documentation does not rewrite commit history.

### Open questions

* **Struct tag casing.** `internal/config/config.go:58` declares
  `EncryptionKey string \`mapstructure:"encryptionKey"\`` in camelCase, while the file key and
  the node surgery both use lowercase `encryptionkey`. viper lowercases keys internally, so this
  is *believed* to be harmless today — but it has not been verified, and it is the kind of
  latent mismatch that turns into a defect when the decoder is reconfigured. The plan includes a
  test that pins the observed behaviour.

* **Relationship to the empty datastore.** The original observation was that the live server
  reported zero records across all namespaces while approximately 128 MB occupied
  `.mcp_recall/`. If that store was written encrypted and is now opened without a key, the two
  observations could share this root cause. This remains a **hypothesis, not an established
  fact**: neither this repository nor its history contains the datastore inspection, service
  logs, or operator attestation needed to resolve it. No live repair that replaces the key is
  authorized by this record until that evidence is recorded.

### Evidence

* Failing config line: `encryptionkey: !!null 8e71e699…dacc0` (value redacted).
* viper error text quoted verbatim above, captured from stderr on 2026-08-23.
* Absence of `recall.yaml.bak` after the failing run, establishing that the recovery branch at
  `configure.go:46` did not execute.
* `configure_test.go:54-55`, establishing that only the blank-key path is asserted.
* Commit `0f6074536a14db0176af598510265b4cac28db8d`, establishing that the decision,
  plan, tests, and implementation landed together rather than in the planned phases.
* `internal/config/config.go:409-425`, establishing the implemented recovery predicate and
  line-oriented extraction behavior described in the post-implementation review.
* `internal/config/config_test.go:34-56`, establishing that recovery has a positive test but no
  negative test for false-positive recovery.

### Related record (2026-08-29)

Database location, the meaning of empty `dbpath`, and `configure`'s failure to materialize a
Badger store are out of scope for this record. They are decided in
[0002-MADR-configure-os-native-datastore-init.md](0002-MADR-configure-os-native-datastore-init.md).

Live-host observations gathered while investigating 0002, distinct from this record's
serialization decision:

* `recall.yaml.pre-repair` exists at
  `~/Library/Application Support/mcp-server-recall/recall.yaml.pre-repair` and still carries
  the `!!null`-tagged key this record described.
* The live `recall.yaml` now has a quoted `encryptionkey` (the 0001 serialization shape) and
  an *absolute* `dbpath` under Application Support. The current wizard does not write that
  absolute path; a fresh `configure` still emits `dbpath: ""`.
* The live `.mcp_recall` directory is not empty of files: it contains `MANIFEST`, `LOCK`,
  `KEYREGISTRY`, a sparse `000001.vlog`, `search_index/`, and `telemetry.ring`. Record
  counts were not re-measured. `serve` `encrypted=true` was not re-verified in that pass.
  The "empty datastore" open question above therefore remains open as to *records*, and is
  closed as to *the directory having no Badger files*.
