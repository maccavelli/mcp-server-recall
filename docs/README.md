# Documentation

This tree holds three kinds of document:

- reports that measured something;
- the decisions behind the work;
- guides you follow to do a thing.

- **[architecture.md](architecture.md)** — how the pieces fit together, as they are now.
- **[guides/](guides/)** — task-shaped walkthroughs for install, configuration, the CLI, the MCP tools, the dashboard, operations, and the repository assessment.
- **[decisions/](decisions/)** — numbered MADR/PLAN pairs.
- **reports/** — what was observed, deciding nothing. None exists yet; the first report creates this directory.

## Records

| Number | Kind | Record | Status |
| :--- | :--- | :--- | :--- |
| 0001 | MADR | [0001-MADR: Persist the recall encryption key as an explicitly typed YAML string and never discard it silently](decisions/0001-MADR-encryptionkey-yaml-tag-round-trip.md) | accepted |
| 0001 | PLAN | [Implement Persist the recall encryption key as an explicitly typed YAML string and never discard it silently](decisions/0001-PLAN-encryptionkey-yaml-tag-round-trip.md) | accepted |
| 0002 | MADR | [0002-MADR: Treat empty dbpath as the platform data directory and materialize the datastore during configure](decisions/0002-MADR-configure-os-native-datastore-init.md) | accepted |
| 0002 | PLAN | [Implement Treat empty dbpath as the platform data directory and materialize the datastore during configure](decisions/0002-PLAN-configure-os-native-datastore-init.md) | implemented |
| 0003 | MADR | [0003-MADR: Ship a curl/PowerShell bootstrap installer that reuses magic-cli-remote's release-asset contract](decisions/0003-MADR-curl-bootstrap-installer.md) | accepted |
| 0003 | PLAN | [Implement Ship a curl/PowerShell bootstrap installer that reuses magic-cli-remote's release-asset contract](decisions/0003-PLAN-curl-bootstrap-installer.md) | accepted |
| 0004 | MADR | [0004-MADR: The installer runs configure --encrypt-db so a piped install finishes a usable store](decisions/0004-MADR-installer-runs-configure.md) | accepted |
| 0004 | PLAN | [Implement The installer runs configure --encrypt-db so a piped install finishes a usable store](decisions/0004-PLAN-installer-runs-configure.md) | accepted |
| 0005 | MADR | [0005-MADR: Remove the harvest subsystem and every dependency on an external Go toolchain](decisions/0005-MADR-remove-harvest-and-go-toolchain-dependency.md) | accepted |
| 0005 | PLAN | [0005-PLAN: Remove the harvest subsystem and every dependency on an external Go toolchain](decisions/0005-PLAN-remove-harvest-and-go-toolchain-dependency.md) | accepted |
| 0006 | MADR | [0006-MADR: Redesign the secondary index key schema](decisions/0006-MADR-domain-scoped-secondary-indexes.md) | accepted |
| 0006 | PLAN | [0006-PLAN: Replace the secondary index key schema](decisions/0006-PLAN-domain-scoped-secondary-indexes.md) | accepted |
| 0007 | MADR | [0007-MADR: Move the toolchain to Go 1.27.4 and the floor to `go 1.26.11`, lint with v2.15.0, and bump `klauspost/compress` for GO-2026-6865](decisions/0007-MADR-move-toolchain-to-go-1-27-4-and-floor-to-1-26-11.md) | accepted |
| 0007 | PLAN | [0007-PLAN: Implement Go 1.27.4 as the toolchain, `go 1.26.11` as the floor, golangci-lint v2.15.0 and `klauspost/compress` v1.20.2](decisions/0007-PLAN-move-toolchain-to-go-1-27-4-and-floor-to-1-26-11.md) | implemented |
| 0008 | MADR | [0008-MADR: Stamp tag builds as release builds, hold it with a check, and correct the checksum snippet in the platform guide](decisions/0008-MADR-stamp-tag-builds-as-release-builds-and-fix-checksum-snippet.md) | accepted |
| 0008 | PLAN | [0008-PLAN: Pass `BUILD_KIND=release` in the tag build, hold it with a check, correct the checksum snippet, and prepare `v2.1.2`](decisions/0008-PLAN-stamp-tag-builds-as-release-builds-and-fix-checksum-snippet.md) | in-progress |
| 0009 | MADR | [Adopt the fleet agent workspace and move records 0001-0008 under docs/decisions](decisions/0009-MADR-adopt-fleet-workspace-scaffold.md) | accepted |
| 0009 | PLAN | [Implement the fleet agent workspace for mcp-server-recall](decisions/0009-PLAN-adopt-fleet-workspace-scaffold.md) | implemented |

## I want to…

| I want to… | Start here |
| :--- | :--- |
| understand how this fits together | [architecture.md](architecture.md) |
| install on macOS, Linux, or Windows | [Platform installation](guides/platform-installation.md) |
| get a server running | [Getting started](guides/getting-started.md) |
| connect an MCP client | [Client integration](guides/client-integration.md) |
| configure paths, encryption, search, or tools | [Configuration](guides/configuration.md) |
| use the CLI | [CLI reference](guides/cli-reference.md) |
| call the MCP tools | [MCP tools and namespaces](guides/mcp-tools.md) |
| read the dashboard | [Dashboard and observability](guides/dashboard-and-observability.md) |
| back up data and understand security boundaries | [Operations and security](guides/operations-and-security.md) |
| see what is complete, partial, or misleading | [Repository assessment](guides/repository-assessment.md) |
| know the rules agents follow in this repository | [CLAUDE.md](../CLAUDE.md) |
| know why this repository has its agent files and docs gates | [0009-MADR-adopt-fleet-workspace-scaffold.md](decisions/0009-MADR-adopt-fleet-workspace-scaffold.md) |
