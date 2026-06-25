# mcli Development Plan

Living progress tracker for `mcli`. The authoritative design is `docs/mcli-design.md`;
this file tracks **what is done and what is next**. Update the checkboxes and the
"Current status" pointer as work lands, and commit alongside the code.

Legend: `[ ]` not started · `[~]` in progress · `[x]` done

---

## Current status

- **Phase:** 0 — project bootstrap (not yet started; only design + docs exist)
- **Next up:** Phase 1, task 1 — create `go.mod` and the `cmd/mcli` entrypoint skeleton.
- **Last updated:** 2026-06-24

---

## Phase 0 — Project bootstrap
- [ ] `go.mod` (`module github.com/Solifugus/mcli`, Go 1.25+)
- [ ] `cmd/mcli/main.go` run-mode dispatch (TUI default vs `mcp serve`)
- [ ] Directory skeleton for `internal/core`, `internal/tui`, `internal/adapters`, `internal/mcp`, `internal/ai`
- [ ] `.gitignore` (binaries, build output, OS cruft)
- [ ] CI: `go build` + `go test` + cross-compile check (GOOS linux/darwin/windows)

## Phase 1 — Core and configuration
- [ ] `~/.mcli` layout creation via `os.UserHomeDir()` path resolution (`internal/core/config`)
- [ ] Load/save `settings.json`, `servers.json`, `ai.json`
- [ ] Workspace manager + default workspace + `workspace.json` (`internal/core/workspace`)
- [ ] Per-workspace `history.log` writer (`internal/core/history`)
- [ ] No UI dependencies in `internal/core` (enforced by package boundaries)

## Phase 2 — REPL shell (TUI)
- [ ] Bubble Tea v2 root model + mode state machine (`repl` mode)
- [ ] Single-line input, **Enter executes**, no statement buffer
- [ ] Chroma single-line syntax highlighting (dialect by connection)
- [ ] History ring (Up/Down) and Tab completion (commands + files first)
- [ ] Prompt context + environment color (§18)
- [ ] Bracketed-paste routing (multi-line paste opens `\edit`)
- [ ] `\enter` and workspace commands (§12)

## Phase 3 — First database adapter (pure Go)
- [ ] Adapter interface + registry (`internal/core/adapter`, §22)
- [ ] First adapter: PostgreSQL (`pgx`) or SQL Server (`go-mssqldb`)
- [ ] `\connect` / `use` / `\list` / `\describe` / query execution (§13–14)
- [ ] Queries run as `tea.Cmd` with `context` cancel on `Ctrl-C`
- [ ] `max_rows_default` guardrail + basic inline result display

## Phase 4 — Grid surface, SQL files, external editor
- [ ] Alt-screen grid mode (`bubbles/table` + `viewport`, paging, horizontal scroll)
- [ ] "Open full result in grid" from a truncated inline result
- [ ] `\files`, `\edit` (external editor via `tea.ExecProcess`, resolution order §11), `\run`, `\cat`, `\copy`, `\rename`, `\delete`
- [ ] Log file operations to history

## Phase 5 — Import / export (`internal/core/transfer`)
- [ ] CSV export, then CSV import
- [ ] TSV and pipe-delimited
- [ ] Excel `.xlsx`
- [ ] Fixed-width flat files (with flat-file grid editing)

## Phase 6 — Additional adapters
- [ ] MySQL / MariaDB (`go-sql-driver/mysql`)
- [ ] Oracle (`go-ora`, not CGo `godror`)
- [ ] DB2 last, behind a build tag — decide pure-Go (`obaydullahmhs/go-db2`) vs CGo (`ibmdb/go_ibm_db`)

## Phase 7 — Server management and safety hardening
- [ ] `\server add/edit/remove/test` (§13)
- [ ] Password sources: keyring (`zalando/go-keyring`) with `prompt`/`env:` fallback
- [ ] Safety core: dangerous-SQL confirmation, read-only mode, production write guards, optional command blocking on prod (`internal/core/safety`, §17)

## Phase 8 — AI assistance (`internal/ai`)
- [ ] `ai.json` providers
- [ ] `\ai ask`; explain/fix current SQL (§20)
- [ ] Schema-context support; configurable providers; never auto-execute SQL

## Phase 9 — MCP server (`internal/mcp`)
- [ ] `mcli mcp serve` / `\mcp serve` exposing workspace/server/schema/query/transfer/file tools over core (§21)
- [ ] Safety controls applied identically to the TUI

## Phase 10 — Built-in editor (deferred)
- [ ] Internal alt-screen editor behind `\edit` + `"editor": "builtin"`: Chroma highlighting, insert/overwrite, OSC 52 copy/paste, keyboard selection, SQL-aware execution

## Phase 11 — Live-table grid editing (optional / later)
- [ ] PK-aware editable grid generating DML through the safety layer — only if it proves worth it over `\edit` + `\run`
