# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

This repository is **pre-implementation**. The only content today is the design
specification at `docs/mcli-design.md`. There is no Go code, `go.mod`, build, or
git history yet. When writing the first code, follow the architecture and module
layout the design doc prescribes (summarized below) rather than inventing a new one,
and read the relevant design section before implementing a feature — the doc is the
contract.

## What mcli is

`mcli` is a multi-database command-line workbench (Go + Bubble Tea v2 TUI) for SQL
development, data exploration, import/export, and AI-assisted database work. The
central abstraction is the **workspace**: a named, task-oriented working context that
restores its current server, current database, SQL files, import/export folders, and
history log when entered. Distribution target is a single self-contained binary that
cross-compiles to Linux/macOS/Windows with no C toolchain.

## Build & run (once code exists)

The default build must stay pure-Go so cross-compilation is a plain GOOS/GOARCH build:

```
go build ./cmd/mcli              # interactive TUI (default run mode)
mcli                             # launch TUI
mcli mcp serve                   # headless stdio MCP server (same as \mcp serve in TUI)
GOOS=windows GOARCH=amd64 go build ./cmd/mcli   # cross-compile, no CGo
go test ./...                    # all tests
go test ./internal/core/...      # one package tree
go test -run TestName ./internal/core/workspace  # single test
```

CGo-requiring adapters (e.g. the DB2 `ibmdb/go_ibm_db` option) must sit behind Go
build tags so they never break the default build. Build them with `-tags`.

## Architecture (the load-bearing idea)

**One UI-agnostic core, two front-ends.** The TUI and the MCP server are both thin
clients of `internal/core`. The core owns *all* domain logic — workspaces, server
registry, query execution, import/export, history, and especially the safety
guardrails — so the two front-ends inherit identical behavior instead of
re-implementing it. The core must not import Bubble Tea or the MCP layer.

Planned module layout (from design §5):

```
cmd/mcli/            main; selects run mode (TUI vs `mcp serve`)
internal/core/       UI-agnostic domain
  workspace/  server/  adapter/  query/  transfer/  history/  safety/  config/
internal/adapters/   one build-tagged package per database (postgres/ mysql/ mssql/ oracle/ db2/)
internal/tui/        Bubble Tea v2 front-end (model/ repl/ grid/ editor/ prompt/)
internal/mcp/        MCP server wrapping core functions as tools
internal/ai/         AI provider clients + context assembly
```

### Database adapters

All databases implement one common `Adapter` interface (design §22): connect, list
databases/schemas/tables/views, describe, `RunQuery` (streamed `RowStream`),
`RunStatement`, explain, search, lineage, and `Dialect()` (which picks the Chroma
lexer). Adapters self-register into a registry keyed by server `type`. Because the
interface is identical regardless of driver, platform-specific messiness (CGo, native
clients) stays quarantined in one package. Import/export is implemented **once** in
`core/transfer` against `RowStream`/`RunStatement`, never per adapter.

Default drivers are all pure Go: PostgreSQL `jackc/pgx`, MySQL/MariaDB
`go-sql-driver/mysql`, SQL Server `microsoft/go-mssqldb`, Oracle `sijms/go-ora` (not
`godror` — that's CGo). DB2 is deferred and tagged.

### TUI specifics (Bubble Tea v2 — not v1)

Target `charm.land/bubbletea/v2`; the import path and API differ from v1, so do not
follow v1 examples. Key facts: `View()` returns a `tea.View` struct (not a string),
keys arrive as `tea.KeyPressMsg`, clipboard is OSC 52 via `tea.SetClipboard`/
`tea.ReadClipboard`, and bracketed paste is a distinct event.

- The root model is a small **mode state machine** (`mode` field: `repl`, `grid`),
  one sub-model per surface, plus a handle to the core. `Update` routes by mode;
  `View` sets `AltScreen = true` only in `grid` mode.
- The external editor is **not a mode** — `\edit` is a `tea.ExecProcess` round-trip
  that suspends the program and returns to `repl` on exit.
- `Update` is single-threaded: never block it. Every query, connection, and
  import/export runs as a `tea.Cmd` returning a `tea.Msg`, each carrying a
  `context.Context` whose cancel is wired to `Ctrl-C` (cancel running query, don't
  quit the app).
- REPL rule: **Enter executes the current line** — single-line input, no
  statement-accumulation buffer, no terminating semicolon. Multi-line work lives
  behind `\edit`; a paste containing newlines opens `\edit` pre-filled instead of
  firing as multiple partial executions.

### Safety guardrails live in the core

Because `mcli` may connect to production, safety settings (dangerous-SQL detection,
read-only mode, `max_rows_default`, production write guards, environment-colored
prompt) are implemented in `internal/core/safety` so both the TUI and MCP front-ends
inherit them. The MCP server must apply the same guards — each MCP tool is a thin
wrapper over a core function (design §21).

## Conventions

- **Paths:** the home directory is `~/.mcli`, resolved via `os.UserHomeDir()` (correct
  on Windows where `~` is not literal). Layout in design §8. Config files:
  `settings.json`, `servers.json`, `ai.json` (global); `workspace.json` per workspace.
- **Secrets:** never store plaintext passwords by default. Password sources are
  `prompt`, `env:VAR`, and `keyring` (`zalando/go-keyring`). Keyring fails on headless
  Linux without D-Bus, so `prompt`/`env:` must always remain available fallbacks.
- **Commands** are backslash-prefixed (`\workspace`, `\connect`, `\edit`, `\run`,
  `\import`, `\export`, `\ai`, `\mcp serve`), except `use <db>` which is bare. Full
  command surface in design §12–§21.
- **Windows:** what matters for rendering is the terminal host, not the shell; Bubble
  Tea downsamples color automatically. The one place to code defensively is the
  external-editor handoff (resolution order: `editor` setting → `$VISUAL` → `$EDITOR`
  → platform default `notepad`/`nano`/`vi`) — don't assume a Unix editor exists.

## Implementation order

The design doc (§24) defines an 11-phase plan; build in that order: (1) core +
config, (2) REPL shell, (3) first pure-Go adapter, (4) grid + SQL files + external
editor, (5) import/export, (6) more adapters, (7) server mgmt + safety hardening,
(8) AI, (9) MCP server, (10) built-in editor [deferred], (11) live-table grid editing
[optional]. Deferred features (built-in editor, editable live-table grid) are
intentionally postponed until the capability they unlock is actually needed.
