# Dashboard and observability

The `dash` command launches a Bubble Tea alternate-screen terminal UI. It
combines fast loopback UDP telemetry with a persisted snapshot/event ring under
the datastore.

```bash
mcp-server-recall dash
```

## Controls

| Key | Action |
|---|---|
| Up or `k` | Previous navigation item |
| Down or `j` | Next navigation item |
| Enter | Activate Quit when selected |
| `q` or Ctrl+C | Exit |

Windows initialization enables virtual terminal processing before starting the
UI. Use a modern Windows Terminal or PowerShell host when ANSI rendering is
incorrect.

## Telemetry paths

### Hot path: UDP

`serve` binds the first free loopback UDP port in this default list:

```text
49156, 49157, 49158, 49159, 49160
```

The dashboard probes each port, registers its address by sending a byte, and
then requests/receives `MetricPayload` updates every 500 ms. It retries with a
2–10 second backoff and reconnects after repeated failures.

Override the candidate list for both processes:

```bash
MCP_TELEMETRY_UDP_PORTS=49200-49204 mcp-server-recall serve
MCP_TELEMETRY_UDP_PORTS=49200-49204 mcp-server-recall dash
```

The parser also accepts comma-separated single ports and ranges. A completely
invalid value falls back to the defaults.

Only one dashboard address is retained by the UDP server at a time; the latest
client to ping becomes the target.

### Cold path: `telemetry.ring`

Every 10 seconds, `serve` writes an atomic snapshot to:

```text
<dbpath>/telemetry.ring
```

The first line is JSON telemetry. Remaining lines are JSON log events from the
in-memory log buffer. `dash` reads this file immediately and every 10 seconds.
This provides richer, slower data and lets the dashboard display the last
snapshot when live UDP is unavailable.

The current UI calls this fallback “BuntDB,” but no BuntDB dependency or
database is used; it is the `telemetry.ring` file.

## Navigation pages

The current dashboard has eight data pages plus Quit:

### Summary

- Live connection state and selected UDP port.
- CPU, Go heap allocation, goroutines, uptime, GC cycles, and cache-hit ratio.
- Badger LSM/value-log sizes from the persisted snapshot.
- The last 12 persisted telemetry events.

There is no separate Event Log page. The event tail is on Summary.

### Memory Consolidation & GC

- Value-log GC sweep and pruned-node counters.
- Counts for nine displayed namespaces.
- 24-hour, 7-day, and 30-day pruning horizons.
- Created, updated, and dedup-merged write counts.

The record model defines eleven domains, but the dashboard tables omit
`ecosystem` and `documents`.

### Semantic Search Engine

- Bleve document count and index directory size.
- Heuristic search-index drift alerts.
- Average measured search latency.

The label is historical: the implementation is BM25 full-text plus fuzzy key
matching, not vector/embedding semantic search. “QPS” appears in a card title,
but the card currently displays latency, not a computed queries-per-second
value.

### Taxonomy & AST Pipeline

- Drift-bypass state and count of configured `exclude_dirs`.
- Namespace distribution.
- Top ten categories, recalculated every third snapshot (about 30 seconds).

“Estimated AST Derivations” is a heuristic equal to twice the `projects`
record count. It is not an actual parsed-file counter.

### RPC & Gateway Analytics

- Search count and average search latency on the hot path.
- Cache and Badger key hit/miss counters.
- response payload bytes recorded by formatted-result paths.
- Storage primitive operation rate and EMA latency.
- Top ten search queries from an in-memory 100-entry query ring.

Metrics are process-lifetime counters and reset when `serve` restarts, except
for information recovered from the last persisted snapshot.

### Network Topology

- Stdio-connected indicator.
- active HTTP port and total clients.
- HTTP client names and MCP session IDs from initialization requests.

The code counts stdio as connected while the server is running and adds one to
the HTTP client count. It does not independently probe the upstream client.

### Security & Cryptography

- Security/validation rejection counter.
- Static rows labeled Curve25519 and AES-GCM.

The two encryption rows are hard-coded to `ENABLED`/`SECURE`; they do not inspect
`encryptionkey`, Badger state, TLS, or a Curve25519 implementation. Treat only
the numeric rejection counter as telemetry. Check configuration and storage
directly for encryption status.

### Config & Environment

- binary version passed into the config object;
- resolved database path;
- a fixed `INFO` log-level label;
- active `GOMEMLIMIT` string.

The log-level value is not dynamically measured.

## What the counters mean

| Counter | Source |
|---|---|
| Cache hits/misses | In-process key/cache paths in `MemoryStore` |
| DB hits/misses | Badger lookup outcomes |
| Bleve documents | Bleve `DocCount` |
| Drift alerts | Background sampled keys missing from Bleve |
| Search queries/latency | Successful search handlers |
| Boundary violations | Explicit storage/path/domain validation hooks that call the counter |
| Write operations | Create, update, and memory-dedup merge paths |
| GC sweeps/pruned nodes | Value-log GC and vacuum/prune work |
| RPC payload bytes | JSON payloads recorded by common result formatting, not necessarily every RPC byte |

## Troubleshooting

### Dashboard never becomes live

1. Confirm exactly one `serve` process is running.
2. Confirm server and dashboard use the same `MCP_TELEMETRY_UDP_PORTS`.
3. Check whether another process occupies every candidate UDP port.
4. Confirm local security software permits loopback UDP.
5. Wait through the reconnect backoff; the ring-file snapshot should still
   populate after the server's first 10-second cycle.

### Dashboard is blank or says loading

Verify that `dash` resolves the same `dbpath` as `serve`. An environment override
applied to only one process makes them read different `telemetry.ring` files.

### Values look stale

Some cards depend only on the 10-second snapshot, even while live UDP is
connected. Categories update about every 30 seconds. Restarting the server
resets process-lifetime counters.

### Terminal layout is clipped

The UI uses fixed-width cards and an 80-column event message cell. Increase the
terminal dimensions or reduce terminal font size; the current UI has no
responsive narrow layout.
