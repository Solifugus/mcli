# mcli Development Plan

Living progress tracker for `mcli`. The authoritative design is `docs/mcli-design.md`;
this file tracks **what is done and what is next**. Update the checkboxes and the
"Current status" pointer as work lands, and commit alongside the code.

Legend: `[ ]` not started · `[~]` in progress · `[x]` done

---

## Current status

- **Phase:** 2 in progress. REPL mechanics done (root model, Enter-executes,
  workspace commands, scrollback, quit); UX polish remaining.
- **Next up:** Phase 2 remainder — syntax highlight (Chroma), history ring (Up/Down),
  Tab completion (commands + workspace files), env-color prompt (§18), bracketed-paste
  routing to `\edit`. Highlight/paste-to-`\edit` partly depend on Phase 3/4, so the
  next concrete steps are the history ring, Tab completion, and env-color prompt.
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
- [ ] Chroma single-line syntax highlighting (dialect by connection)
- [ ] History ring (Up/Down) and Tab completion (commands + files first)
- [ ] Prompt context + environment color (§18)
- [ ] Bracketed-paste routing (multi-line paste opens `\edit`)

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
