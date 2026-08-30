---
status: "proposed"
date: 2026-08-30
decision-makers: maccavelli
pairs-with: 0006-MADR-domain-scoped-secondary-indexes.md
---

# 0006-PLAN: Replace the secondary index key schema

Implementation plan for
[0006-MADR](0006-MADR-domain-scoped-secondary-indexes.md). Execution begins only
after the operator approves **this document**. Every count and line number below
was verified against the working tree on 2026-08-30.

## Baseline

| Fact | Value |
|---|---|
| `_idx:` occurrences, `badger.go` | 39 |
| `_idx:` occurrences, `ingest.go` | 4 |
| `_idx:` occurrences, `badger_vacuum_test.go` | 4 |
| Distinct functions touching the index | 21 |
| Files outside `internal/memory` referencing `_idx:` | **0** |
| `go test ./internal/memory/ ./internal/server/` | pass |
| Schema prototyped against Badger v4.9.1 | yes — see MADR |

The index is fully encapsulated in `internal/memory`. No handler, CLI command,
or telemetry path constructs or parses an index key.

## Complete edit surface

Every site, grouped by role. This inventory is the plan's contract: if a site
turns up that is not listed here, that is a deviation.

### Writers — 2 functions, 8 sites

| Function | Lines |
|---|---|
| `createRecordIndices` | `2623`, `2630`, `2638`, `2646` |
| `deleteRecordIndices` | `2591`, `2598`, `2606`, `2614` |

### Sentinel checks — "skip index keys during a full scan" — 5 sites

`SyncSearchIndex:538`, `SyncSearchIndex:598`, `performAudit:742`,
`ExportJSONL:3382`, `PruneDomain:3921`.

### Backfill to delete — 2 sites

`SyncSearchIndex:586`, `:604` — the "Fix #15" domain-index backfill. Deleted
outright per the MADR; the schema it repairs ceases to exist.

### Readers — 4 index kinds, 23 sites

| Kind | Function : line |
|---|---|
| domain | `VacuumMemories:1677`, `VacuumStandards:1837`, `searchGeneral:1925`, `ListKeys:2102`, `FindSessionBySuffix:2153`, `ListSessions:2203`, `ListDomainOverview:3150`, `SearchDomain:3247`, `PurgeDomain:3815`, `PruneDomain:3897`, `GetByAttributes:3989`, `VacuumProjects:4084`, `DeleteDomain (ingest.go:101)` |
| category | `findSimilarLocked:1282`, `ListCategories:2979`, `DeleteStandards:3614`, `DeleteProjects:3727`, `DeleteByCategory (ingest.go:31)` |
| tag | `searchByTag:1897`, `ListSessions:2205`, `:2207`, `:2209` |
| time | `GetRecent:2057` |

### Readers that lose a post-scan domain filter

These currently scan a domain-agnostic index and discard after loading. Under a
domain-scoped prefix the filter becomes dead code and **must be removed**, not
left in place:

| Function | Redundant filter |
|---|---|
| `searchByTag` | `rec.Domain == DomainMemories` (`:1908`) |
| `GetRecent` | `rec.Domain == DomainMemories` (`:2066`) |
| `ListSessions` | `rec.Domain == domain` (`:2246`) |
| `findSimilarLocked` | `rec.Domain == DomainMemories` (`:1294`) |
| `DeleteStandards` | `rec.Domain == DomainStandards` |
| `DeleteProjects` | `rec.Domain == DomainProjects` |
| `DeleteByCategory` | `rec.Domain == DomainMemories` |

### Test files coupled to the schema

`badger_vacuum_test.go:55`, `:59`, `:131`, `:192` construct index keys by hand.
`ingest_test.go:98` and `domain_scoping_test.go:111` mention the format in
comments only.

---

## Phase 1 — The key codec

**Goal:** one tested encoder/decoder. No behaviour change; nothing calls it yet.

### Files

New: `internal/memory/indexkey.go`, `internal/memory/indexkey_test.go`.

### Steps

1. Define the kind constants — `kindTime = 't'`, `kindCategory = 'c'`,
   `kindTag = 'g'` — and the `"_x"` sentinel.
2. `encodeIndexKey(domain string, kind byte, value, recordKey string) ([]byte, error)`
   joining `"_x"`, domain, kind, value, recordKey with `0x00`, returning an error
   if any component contains `0x00`.
3. `encodeTimeValue(t time.Time) string` returning `fmt.Sprintf("%016x", uint64(t.UnixNano()))`.
   **Not** big-endian binary — see the MADR correction; binary timestamps
   contain NUL bytes and cannot coexist with a NUL separator.
4. `indexPrefix(domain string, kind byte, value string) []byte` for scans, with a
   trailing `0x00` when `value` is non-empty so `tag:foo` cannot prefix-match
   `tag:foobar`.
5. `decodeIndexKey(key []byte) (domain string, kind byte, value, recordKey string, ok bool)`
   splitting on `0x00` and validating component count.

### Verification

```bash
CGO_ENABLED=0 go test ./internal/memory/ -run TestIndexKey
```

Table tests must cover: components containing `:`, `%`, spaces, newlines,
Unicode, the literal `_x`, and the kind bytes; NUL in each component position
rejected; `encode` → `decode` round-trip identity; `tag:foo` prefix does not
match `tag:foobar`; hex ordering correct across `0`, `108000000000000`,
`1152939600000000000`, `1788066000000000000`, `18446677200000000000`,
`math.MaxUint64`.

### Acceptance

Codec round-trips arbitrary NUL-free input; rejects NUL; hex ordering is
lexicographic. **Zero production callers yet** — `git grep encodeIndexKey`
returns only the codec and its test.

**Commit:** `feat(memory): add domain-scoped index key codec`

---

## Phase 2 — Writers and NUL rejection

**Goal:** records are indexed under the new schema. Readers still use the old
one, so the store carries both families for exactly one commit.

### Steps

1. Rewrite `createRecordIndices` (`:2621`) to emit three entries — `t`, `c`
   (when category is non-empty), and one `g` per tag — via `encodeIndexKey`,
   with `txn.Set(key, nil)`. **No `_idx:domain:` entry:** the `t` entry is the
   domain membership index.
2. Mirror it in `deleteRecordIndices` (`:2589`).
3. Add NUL validation at the two write boundaries, `Save` (`:881`) and
   `SaveBatch` (`:2679`), rejecting domain, key, category or any tag containing
   `0x00` with a clear error **before** any write occurs, so no partial index is
   created.
4. Update the five sentinel checks to `bytes.HasPrefix(k, []byte("_x\x00"))`.
   `SyncSearchIndex:538`/`:598`, `performAudit:742`, `ExportJSONL:3382`,
   `PruneDomain:3921`.
5. Delete the "Fix #15" backfill (`:586`-`:641`) and its `backfillKeys` scan.

### Verification

```bash
go build ./... && go vet ./...
CGO_ENABLED=0 go test ./internal/memory/ -run 'TestIndexKey|TestSave'
```

### Acceptance

* A saved record produces exactly `N+2` entries for `N` tags, all with empty
  values — asserted by a test that scans `_x\x00` and counts.
* Saving with a NUL in any component returns an error and writes nothing.
* No `_idx:` key is written any more.
* The tree builds. Readers are knowingly stale until Phase 3, so **reader tests
  are expected to fail at this commit** — this is the one deliberate exception
  to green-at-every-commit, and Phases 2 and 3 therefore land together on the
  same branch and must not be released independently.

**Commit:** `feat(memory): write the domain-scoped index schema`

---

## Phase 3 — Readers

**Goal:** every reader scans the new schema; redundant post-scan filters are
removed.

### Steps, by kind

1. **Domain readers (13 sites).** Replace `_idx:domain:<d>:` with
   `indexPrefix(d, kindTime, "")`. Recover the record key with
   `decodeIndexKey` rather than `TrimPrefix`. Set `PrefetchValues = false` —
   values are now empty.
2. **Category readers (5 sites).** Replace `_idx:cat:<cat>:` with
   `indexPrefix(domain, kindCategory, cat)`. Each caller now has a domain:
   `findSimilarLocked` is memory-domain by contract (its doc comment and the
   filter at `:1294` both say so); `DeleteByCategory` is memory-domain;
   `DeleteStandards`/`DeleteProjects` have theirs. Remove the redundant filters.
3. **`ListCategories` (`:2979`).** Scan `indexPrefix(domain, kindCategory, "")`
   and take the category from `decodeIndexKey`, replacing
   `strings.Split(key, ":")[2]` (`:2984`). This is the fix for the corruption
   bug. When `domain == ""`, scan `_x\x00` and decode each key.
4. **Tag readers (4 sites).** `searchByTag` takes a domain argument and scans
   `indexPrefix(domain, kindTag, tag)`; its caller `Search` (`:1421`) passes
   `DomainMemories`. `ListSessions` (`:2205`-`:2209`) narrows **within** its
   domain instead of replacing the prefix.
5. **Time reader (`GetRecent:2057`).** Scan `indexPrefix(DomainMemories,
   kindTime, "")` in reverse. Seek to the prefix plus `0xff`.
6. Remove every filter listed in "Readers that lose a post-scan domain filter".

### Verification

```bash
go build ./... && go vet ./...
CGO_ENABLED=0 go test -count=1 ./...
GOLANGCI_LINT="$(go env GOPATH)/bin/golangci-lint" make lint
grep -rn '_idx:' internal/ cmd/          # expect: no output
```

### Acceptance

* No `_idx:` string remains anywhere, including tests and comments.
* `internal/memory/domain_scoping_test.go` passes **unmodified**. If it needs
  editing, isolation behaviour changed and the implementation is wrong — stop.
* Full suite green.

**Commit:** `feat(memory): read the domain-scoped index schema`

---

## Phase 4 — Tests for the defects this fixes

**Goal:** pin each MADR defect with a test that fails against the old schema.

### Steps

1. **Separator corruption.** Save category `"team:platform"`; assert
   `ListCategories` returns `map["team:platform":1]`. Verified to return
   `map["team":1]` on the current tree — this test fails before the change and
   passes after.
2. **Ordering.** `GetRecent` across records timestamped 1970, 2006, 2026, 2554
   and a zero `time.Time`, asserting strict reverse-chronological order.
3. **NUL invariant.** Scan the whole store and assert no index key contains
   `0x00` outside a separator position — the invariant the schema rests on.
4. **Entry accounting.** A record with `N` tags yields exactly `N+2` entries, all
   empty-valued.
5. **Teardown.** After `DeleteDomain` and `PurgeDomain`, a scan of
   `_x\x00<domain>\x00` returns nothing.
6. **Adversarial round-trip.** Categories, tags and keys containing `:`, `_x`,
   kind bytes, newlines and Unicode survive save → index → list → search →
   delete.
7. Update `badger_vacuum_test.go:55`, `:59`, `:131`, `:192` to build keys with
   the codec instead of `fmt.Sprintf`.

### Acceptance

Every test in step 1-6 fails when reverted against the pre-change schema — each
must be demonstrated to have teeth, not merely to pass.

**Commit:** `test(memory): pin index schema correctness invariants`

---

## Phase 5 — Documentation and release

### Steps

1. `docs/guides/operations-and-security.md` and `docs/guides/configuration.md`:
   state that upgrading requires recreating the datastore, and that
   `export_records` → upgrade → `import_records` is the only supported path.
2. `README.md`: note the breaking datastore change under current limitations.
3. `docs/guides/repository-assessment.md`: annotate the storage row, as done for
   0005.
4. Release note: **major** version bump, existing stores unreadable, export path
   documented.

**Commit:** `docs: record the breaking index schema change`

---

## Final gate

```bash
grep -rn '_idx:' internal/ cmd/                    # expect: no output
gofmt -l $(git ls-files '*.go')                    # expect: empty
go vet ./...
CGO_ENABLED=0 go test -count=1 ./...
GOLANGCI_LINT="$(go env GOPATH)/bin/golangci-lint" make lint
cp go.mod /tmp/m && cp go.sum /tmp/s && go mod tidy && diff -q go.mod /tmp/m && diff -q go.sum /tmp/s
make build-all
```

Manual gates:

1. `serve` against a **fresh** datastore, then `save_to_recall`, `list`,
   `search`, `prune_records` and `delete` across `memories`, `standards`,
   `projects` and `sessions`.
2. `export_records` from a store built on the old schema, upgrade the binary,
   `import_records` into a fresh store, and confirm record counts and content
   match — this is the operator's documented upgrade path and must be shown to
   work end to end.
3. `dash` renders Category Distribution correctly, since `ListCategories("")`
   backs it.

## Deviation protocol

Anything this plan does not cover — an index site not in the inventory, a reader
whose domain is not statically determinable, a test that cannot be re-expressed
— is a **deviation**. Stop, present evidence, and offer real fixes. Record the
decision here and amend the MADR when it changes a decision or contradicts an
asserted fact, before executing.

Anticipated risks:

* **Phase 3, step 2** — if any category reader's domain is not statically
  determinable, the whole domain-first design is wrong for that path. Stop; do
  not fall back to a full `_x\x00` scan silently.
* **Phase 2/3 coupling** — the tree does not pass tests between them. They land
  together. Do not release from a commit in between.
* **`GetRecent` reverse seek** — reverse iteration with a prefix requires seeking
  to `prefix + 0xff`; getting this wrong yields an empty result rather than an
  error. The ordering test in Phase 4 is what catches it.

## Rollback

Phases 1 and 4-5 revert independently. Phases 2 and 3 are coupled and revert
together. There is no data to preserve, so any rollback is `git revert` plus
recreating the datastore.

## Out of scope

* Any migration, backfill, or dual-format read path. The MADR is explicit that
  existing stores are discarded.
* An index version marker. Not added, per the MADR — recorded there as the first
  thing that must change if compatibility is ever wanted again.
* Bleve. The full-text index is untouched by this work.
