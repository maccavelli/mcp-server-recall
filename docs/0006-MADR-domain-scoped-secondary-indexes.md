---
status: "accepted"
date: 2026-08-30
decision-makers: maccavelli
consulted: maccavelli
informed: maccavelli
---

# 0006-MADR: Redesign the secondary index key schema

## Context and Problem Statement

Phase 9b of [0005-PLAN](0005-PLAN-remove-harvest-and-go-toolchain-dependency.md)
recorded that three readers scan a domain-agnostic index and discard
non-matching records after loading them. Investigating that led to a broader
finding: **the secondary index schema has four independent defects**, of which
the scan cost is only one, and two are correctness bugs.

The operator has directed that this be treated as **greenfield**: no data
migration, no preservation of existing stores, no backwards compatibility with
the current on-disk format. That removes the constraint that dominated the
first draft of this MADR and makes a clean redesign the obvious course.

### The current schema

`createRecordIndices` (`badger.go:2621`) writes four flat index families, each
storing the record key as both the final key component *and* the value:

```text
_idx:t:<hex unixnano>:<key>   -> <key>
_idx:cat:<lowercased cat>:<key> -> <key>
_idx:tag:<lowercased tag>:<key> -> <key>
_idx:domain:<domain>:<key>      -> <key>
```

`deleteRecordIndices` (`badger.go:2589`) mirrors it exactly.

### Defect 1 — `:` is not a safe separator, and it silently corrupts data

Categories, tags and record keys are all user-supplied and routinely contain
`:`. Tags are *defined* with colons (`project:foo`, `type:func`,
`implements:io.Reader`), and keys are colon-structured
(`pkg:mypkg:Symbol`, `srv:standards:1730000000`).

`ListCategories` parses the category back out with
`strings.Split(key, ":")` and takes `parts[2]` (`badger.go:2984-2986`). This is
unrecoverably ambiguous. **Reproduced on the current tree:**

```text
saved category "team:platform" -> ListCategories returned map[team:1]
```

The category is silently truncated at the first colon. This is a live data
correctness bug, not a theoretical one.

### Defect 2 — the time index encoding is width-dependent

`_idx:t:%x` formats `UnixNano` as variable-width hex, then relies on
lexicographic ordering for `GetRecent`'s reverse chronological scan. Ordering
holds **only while every value has the same hex width**:

| Instant | hex width |
|---|---:|
| 1970-01-02 | 12 |
| 2006-07-15 | 16 |
| 2026-08-30 | 16 |
| 2554-07-21 | 16 |

Width 16 spans roughly 2006-07 to 2554, so ordering is correct today by
coincidence of the current epoch, not by construction. Any value outside that
window — including a negative `UnixNano` from a zero `time.Time`, which formats
with a leading `-` — sorts into the wrong place.

### Defect 3 — no index is domain-scoped, so reads discard work

| Reader | Index scanned | Discard test | Site |
|---|---|---|---|
| `searchByTag` | `_idx:tag:<tag>:` | `rec.Domain != DomainMemories` | `badger.go:1908` |
| `GetRecent` | `_idx:t:` | `rec.Domain != DomainMemories` | `badger.go:2066` |
| `ListSessions` | `_idx:tag:{trace,project,outcome}:<v>:` | `rec.Domain != domain` | `badger.go:2205-2209`, `:2246` |

Each rejected candidate costs a Badger point-read, a Zstd `DecodeAll` and a
`json.Unmarshal` (`badger.go:1263` → `record.go:127-136`) to read one field and
throw the result away. Complexity is O(records carrying the tag) rather than
O(records in the domain).

This is reached in practice because `handleSaveToRecall` stamps `project:`,
`outcome:`, `model:` and `trace:` tags onto records in **every** namespace it
writes (`server.go:405-420`), so one `project:` value spans many domains in a
single flat index.

### Defect 4 — index values duplicate the key

Every entry stores the record key as its value, when that key is already the
final component of the index key. The codebase demonstrates it is redundant:
`PurgeDomain` sets `PrefetchValues = false` and recovers the key with
`strings.TrimPrefix` (`badger.go:3802`-). Every other reader pays a value read
for information it already holds.

### Non-goals

Every tag lookup is already domain-scoped in intent — `GetByAttributes` rejects
an empty domain outright (`badger.go:3983-3985`). No cross-domain tag query
exists or is wanted, so nothing depends on the domain-agnostic layout.

## Decision Drivers

* Correctness first: an index must round-trip arbitrary user input.
* Every query is domain-scoped, so the schema should make that the cheap case.
* Sort-order must be a property of the encoding, not of the current date.
* Index storage and write amplification should be proportionate.
* One uniform scheme is easier to reason about than four ad-hoc prefixes.
* Greenfield: no migration, no legacy format, no compatibility shims.

## Considered Options

1. **Patch the four defects in place**, keeping four flat `:`-separated families.
2. **Add composite indexes alongside the existing ones** (the migration-era
   proposal from this MADR's first draft).
3. **Replace the schema with one domain-first, key-only, unambiguous encoding.**

## Decision Outcome

Chosen option: **Option 3 — replace the schema.**

Option 1 cannot fix Defect 1 without an escaping scheme, which is strictly more
complex than choosing a separator that cannot collide. Option 2 was written to
avoid a migration that no longer needs avoiding; carrying both a legacy and a
composite family would double write cost to preserve a format the operator has
said to discard. With greenfield licence, neither is defensible.

### Schema

One index family, one encoding:

```text
"_x" 0x00 <domain> 0x00 <kind> 0x00 <value> 0x00 <key>   ->   (empty value)
```

* `0x00` is the separator. It cannot appear in any component: the write boundary
  **rejects** a domain, category, tag or key containing a NUL byte, rather than
  escaping it. Rejection is verifiable; escaping is another parser to get wrong.
  Every component is therefore NUL-free text, which is what makes a
  separator-delimited key safe to split on read.
* `<kind>` is one byte: `t` time, `c` category, `g` tag.
* `<value>` is the indexed term — for `t`, `UnixNano` as **16-character
  zero-padded lowercase hex** (`%016x`), with reverse-chronological reads served
  by Badger's reverse iteration rather than by encoding tricks.

  > **Correction, 2026-08-30.** This originally specified an 8-byte big-endian
  > `uint64`. That is **incompatible with a `0x00` separator** and was caught by
  > prototyping the schema against Badger v4.9.1 before writing the plan: every
  > realistic `UnixNano` contains NUL bytes in big-endian form —
  > `1788066000000000000` encodes as `18 d0 7c 8d ac f4 20 00`, and
  > `108000000000000` as `00 00 62 39 b5 a2 c0 00`. A separator-delimited key
  > cannot carry raw binary that may contain the separator; the prototype lost
  > one of four time entries and panicked on parse. Fixed-width hex keeps the
  > component NUL-free while preserving the property that matters — lexicographic
  > order equals numeric order, for the whole `uint64` range, by construction
  > rather than by the current epoch.
* The value is **empty**. Readers set `PrefetchValues = false` and recover the
  record key from the final key component.

### The domain index disappears

Every record gets exactly one `t` entry, so `_x\0<domain>\0t\0` **is** the
membership index for a domain. A separate `_idx:domain:` family is redundant and
is removed.

Index entries per record therefore drop from `N+3` to `N+2` for a record with
`N` tags, while every entry also loses its duplicated value. The redesign is
strictly cheaper than the schema it replaces, which is unusual for a change that
adds a capability.

### Query shapes this enables

| Query | Scan |
|---|---|
| records in a domain | `_x\0<d>\0t\0` |
| recent in a domain | `_x\0<d>\0t\0`, reverse |
| domain + tag | `_x\0<d>\0g\0<tag>\0` |
| domain + category | `_x\0<d>\0c\0<cat>\0` |
| **drop an entire domain's indexes** | `_x\0<d>\0` — one prefix sweep |

The last row is a bonus: `DeleteDomain` and `PurgeDomain` currently delete each
record's index entries individually via `deleteRecordIndices`. Domain-first
ordering makes index teardown a single prefix range.

### Consequences

* Good, because Defect 1 becomes structurally impossible — no component can
  contain the separator, enforced at the write boundary.
* Good, because Defect 2 is fixed by construction: fixed-width big-endian
  timestamps sort correctly for every representable instant.
* Good, because Defect 3 is fixed for all three readers: each becomes a single
  domain-scoped prefix scan with no discarded records.
* Good, because Defect 4 is fixed: empty values, `PrefetchValues = false`.
* Good, because the store gets *smaller* — one fewer entry per record and no
  duplicated values.
* Good, because domain teardown becomes a prefix sweep.
* **Bad, because every existing datastore is unreadable and must be recreated.**
  This is the explicit consequence of the greenfield direction, not an oversight.
  Operators upgrading across this change lose their data unless they export to
  JSONL first and re-import afterwards.
* Bad, because index keys stop being greppable ASCII, which makes ad-hoc
  datastore inspection harder. A small `recall dash`-side or debug-only decoder
  mitigates this.
* Bad, because rejecting NUL bytes is a new input constraint that must be
  enforced and tested at every write path, not just the obvious ones.
* Neutral, because read semantics are unchanged. `domain_scoping_test.go` must
  pass **unmodified**; if it needs editing, isolation behaviour has changed and
  the implementation is wrong.

### Release handling

This is a breaking datastore change. It warrants a **major** version bump, a
release note stating plainly that existing stores must be recreated, and the
`export_records` → upgrade → `import_records` path documented as the only
supported way to carry data across it.

### Schema validated by prototype

The schema was prototyped directly against Badger v4.9.1 — the pinned version —
outside the repository, before committing to it. Confirmed with NUL-separated
keys and empty values:

| Scan | Result |
|---|---|
| `domain=memories, tag=project:foo` | 1 hit — does not see the sessions record with the same tag |
| `domain=sessions, tag=project:foo` | 1 hit |
| `domain=memories, tag=team:platform` | 1 hit — the category/tag colon case that breaks today |
| `domain=memories, all tags` | 2 hits |
| `domain=memories, all kinds` (teardown) | 7 hits — one prefix covers every index kind |
| `domain=memories, time`, reverse | 4 hits, correctly ordered |

Ascending time scan returned `108000000000000`, `1152939600000000000`,
`1788066000000000000`, `18446677200000000000` in that order — 1970 through 2554
in correct numeric sequence.

### Confirmation

1. **Round-trip property test.** Categories, tags and keys containing `:`, `%`,
   newlines, spaces, Unicode and adversarial substrings (`_x`, the kind bytes)
   all round-trip through save → index → list/search → delete. The specific
   reproduction above — category `"team:platform"` — must return
   `map["team:platform":1]`.
2. **NUL rejection.** A domain, category, tag or key containing `0x00` is
   rejected with a clear error at the write boundary, and no partial index is
   written.
3. **Ordering.** `GetRecent` returns strict reverse-chronological order across a
   set spanning 1970, 2006, 2026 and 2554, and across a zero `time.Time`. No
   index key contains a `0x00` byte outside a separator position — assert this
   directly over a full store scan, since it is the invariant the whole schema
   rests on.
4. **Zero discards.** Instrumented counts of records loaded and then discarded
   are zero for `searchByTag`, `GetRecent` and tag-filtered `ListSessions`.
5. `internal/memory/domain_scoping_test.go` passes **unmodified**.
6. **Index accounting.** A record with `N` tags produces exactly `N+2` index
   entries, all with empty values.
7. **Teardown.** `DeleteDomain` and `PurgeDomain` leave no orphaned index entries
   under any prefix — verified by scanning `_x\0` after a purge.
8. `go vet`, `CGO_ENABLED=0 go test ./...` and `golangci-lint` stay clean; the
   four-platform build succeeds.

## More Information

* Supersedes the migration-oriented first draft of this MADR, which proposed
  additive composite indexes plus a backfill. That approach existed only to
  preserve existing stores and is obsolete under the greenfield direction.
* **One finding from that draft survives and should be handled regardless.** The
  existing "Fix #15" domain-index backfill (`badger.go:585`) sits inside
  `SyncSearchIndex`, which returns early when `searchEngine == nil`
  (`:512`) and when the record count exceeds `searchLimit` (`:519`) — and
  `SetSearchEngine` only runs under `Cfg.SearchEnabled()` (`serve.go:165`). On a
  store with search disabled, that backfill has never run. Under this MADR the
  backfill is deleted outright along with the schema it repairs, so the latent
  defect is resolved by removal rather than by fixing it.
* The store has no index version marker; a grep for `idx_version`,
  `schema_version` and `_meta:` returns nothing. Since this schema is versioned
  by the release that introduces it and no migration is supported, a marker is
  **not** added — but if compatibility is ever wanted again, that is the first
  thing that must change.
* No implementation plan exists yet. Per repository convention,
  `0006-PLAN-domain-scoped-secondary-indexes.md` must be written and approved
  before any code changes begin.
