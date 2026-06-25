# mcli Development Plan

Living progress tracker for `mcli`. The authoritative design is `docs/mcli-design.md`;
this file tracks **what is done and what is next**. Update the checkboxes and the
"Current status" pointer as work lands, and commit alongside the code.

Legend: `[ ]` not started · `[~]` in progress · `[x]` done

---

## Current status

- **Phase:** 5 mostly complete. CSV/TSV/pipe + Excel `.xlsx` import/export all working
  and round-trip-verified against real Postgres. Remaining: fixed-width flat files,
  which is underspecified (column-width spec source) and includes an editable grid.
- **Next up:** decide the fixed-width approach with the user, OR move to Phase 6
  (additional adapters: MySQL, Oracle, DB2-tagged) and circle back to fixed-width.
- **Last updated:** 2026-06-24
- **Notes:** `go.mod` is on Go 1.25.0. The system Go is 1.24.4, but `GOTOOLCHAIN=auto`
  auto-downloads 1.25 into `~/go/pkg/mod` (no sudo) — verified building/testing under
  1.25.0. `gh` CLI is not installed — use plain `git`.

---

## Phase 0 — Project bootstrap
- [x] `go.mod` (`module github.com/Solifugus/mcli`, Go 1.24 for now — bump to 1.25 at Phase 2)
- [x] `cmd/mcli/main.go` run-mode dispatch (TUI default vs `mcp serve`, plus help/version)
- [x] Package skeleton: `internal/core/{config,workspace,history}`, placeholder `internal/{tui,mcp,ai}`
- [x] `.gitignore` (binaries, build output, OS cruft)
- [~] CI: workflow written but **parked** at `.github/ci.yml.disabled` — the push token
  lacks `workflow` scope so it can't live under `.github/workflows/`. See `.github/README.md`
  to activate (add `workflow` scope to the PAT, or add the file via the GitHub web UI).

## Phase 1 — Core and configuration
- [x] `~/.mcli` layout creation via `os.UserHomeDir()` path resolution (`internal/core/config`)
- [x] Load/save `settings.json`, `servers.json`, `ai.json` (defaults when absent, 0600 perms)
- [x] Workspace manager + default workspace + `workspace.json` (create/list/load/save/rename/delete, name validation)
- [x] Per-workspace `history.log` writer (`internal/core/history`)
- [x] No UI dependencies in `internal/core` (config/workspace/history import only stdlib)

## Phase 2 — REPL shell (TUI)
- [x] Bubble Tea v2 root model + mode state machine (`repl` mode; `grid` reserved)
- [x] Single-line input, **Enter executes**, no statement buffer
- [x] `\enter` and workspace commands (§12), `\help`, `\quit`, scrollback via `tea.Println`
- [x] `core` facade (`internal/core`) shared by both front-ends; `cmd/mcli` launches the TUI
- [x] History ring (Up/Down) and Tab completion (commands + workspace names; files in Phase 4)
- [x] Chroma single-line syntax highlighting (dialect by connection) — done with Phase 3
- [x] Prompt context + environment color (§18) — done with Phase 3
- [ ] Bracketed-paste routing (multi-line paste opens `\edit`) — deferred to Phase 4 (needs `\edit`)

## Phase 3 — First database adapter (pure Go)
- [x] Adapter interface + registry (`internal/core/adapter`, §22)
- [x] PostgreSQL via pgx (`internal/adapters/postgres`); registered through `internal/adapters`
- [x] `\connect` / `use` / `\list` / `\describe` / bare-SQL execution (§13–14)
- [x] Queries run as `tea.Cmd` with `context` cancel on Ctrl-C (prompt snapshot avoids races)
- [x] `max_rows_default` guardrail + aligned inline result table
- [x] Live verification against a real Postgres (PG 17.10, gbasic_site_dev): connect,
      list databases/tables, describe (PK detection), streaming query — all confirmed.
      Fixed `.pgpass` fallback for discrete params (build a keyword DSN, parse once)
- [x] Ride-alongs: Chroma single-line syntax highlighting (dialect-aware, cursor
      overlay, coalesced spans) and env-color prompt (§18: dev green / test·stage
      yellow / prod red / unknown gray), gated by the `color_prompt` setting

## Phase 4 — Grid surface, SQL files, external editor
- [x] `\files`, `\edit` (external editor via `tea.ExecProcess`, resolution order §11), `\run`, `\cat`, `\copy`, `\rename`, `\delete` — verified incl. editor suspend/resume
- [x] Log file operations to history (WRITE/EDIT/COPY/RENAME/DELETE)
- [x] Tab completion for file commands
- [x] Alt-screen grid mode (`bubbles/table`, vertical paging/scroll, Esc/q to return);
      horizontal scroll for very wide rows deferred (columns capped at 60, clipped to width)
- [x] "Open full result in grid" via `\grid` from the last query (fetches up to
      gridRowCap=10000; inline shows first max_rows with a `\grid` hint)
- [x] Bracketed-paste routing: multi-line paste parks in scratch.sql and opens `\edit`

## Phase 5 — Import / export (`internal/core/transfer`)
- [x] CSV export, then CSV import (`\export query|table|current to <path>`, `\import <path> into <table>`)
- [x] TSV and pipe-delimited (delimiter inferred from extension: .csv/.tsv/.psv)
      — verified round-trip against real Postgres (export → import → counts match)
- [x] Excel `.xlsx` (export query/table/current; import with optional `sheet <name>`) —
      verified round-trip against real Postgres; pure-Go via `xuri/excelize/v2`
- [ ] Fixed-width flat files (with flat-file grid editing) — needs a design decision on
      where the column-width spec comes from; the grid-editing part needs an editable grid

## Phase 6 — Additional adapters
- [ ] MySQL / MariaDB (`go-sql-driver/mysql`)
- [ ] Oracle (`go-ora`, not CGo `godror`)
- [ ] DB2 last, behind a build tag — decide pure-Go (`obaydullahmhs/go-db2`) vs CGo (`ibmdb/go_ibm_db`)

## Phase 7 — Server management and safety hardening
- [x] `\server list` and `\server show <name>` (read-only) brought forward for usability;
      bare `\connect` lists available servers; Tab completes server names + `\list` targets
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
