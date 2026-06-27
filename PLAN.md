# mcli Development Plan

Living progress tracker for `mcli`. The authoritative design is `docs/mcli-design.md`;
this file tracks **what is done and what is next**. Update the checkboxes and the
"Current status" pointer as work lands, and commit alongside the code.

Legend: `[ ]` not started · `[~]` in progress · `[x]` done

---

## Current status

- **Real-use ready.** All ten built phases plus post-Phase-10 polish are done and
  the docs (README, CLAUDE.md, this file, design doc) are current. Only optional
  Phase 11 (live-table grid editing) is deferred.
- **Command prefix is now `.`** (was `\`): swept across dispatch, help, completion,
  banner, core/safety/MCP messages, tests, and all docs incl. the design doc. A
  stray `\command` reports unknown with a migration hint. `use <db>` stays bare.
- **Inline result rendering is width-aware:** query results and `.describe` cap
  columns, truncate long cells with `…`, and clip rows to the terminal width with
  `›` so they no longer wrap; the summary flags lossy views and points at `.grid`.
- **Post-Phase-10 additions:** `.clear` (clear screen + scrollback via a raw
  terminal sequence, since `tea.ClearScreen` only erases the inline region),
  `.ai help` (practical per-subcommand examples), and an **SQL linter**
  (`.lint <file|current> [live]` plus the `lint_sql` MCP tool). The linter lives in
  `internal/core/lint` so both front-ends share it: static checks (safety/correctness
  via the classifier, lexical syntax — unbalanced parens, unterminated
  literals/comments, unknown leading keyword, JOIN-without-ON; style — SELECT *,
  trailing whitespace, tabs, optional keyword casing), plus a connected `LiveLint`
  that EXPLAINs each query for deep syntax/schema errors. New `safety.Mask` exposes
  the noise-blanking scanner. Live-verified against `gbasic`: an unknown relation was
  caught by the live check; a valid query produced none. Settings gained a `lint`
  block.
- **Phase:** 10 complete. Built-in SQL editor landed: a new alt-screen
  `modeEditor` behind `"editor": "builtin"`, with live Chroma highlighting,
  insert/overwrite (INS/OVR cue), keyboard selection, OSC 52 copy, and — its
  reason to exist — running the statement under the cursor (or selection) against
  the live connection through the same safety guard, results returning to the grid
  then back to the editor. New `safety.StatementSpans`/`StatementAt` (comment/
  string-aware) split the buffer. Highlighting reuses `highlight.go` via the
  extracted `renderLineSpans`. Live-verified against `gbasic`: a `SELECT` ran from
  the editor into the grid with real rows; a `DELETE` without `WHERE` was gated by
  the confirm prompt. Phases 1–10 done.
- **Next up:** only Phase 11 (live-table grid editing) remains in the plan, and it
  is optional — deferred until the PK-aware DML it unlocks is wanted. Loose ends:
  live SQL Server round-trip (needs password); DB2 (needs a working driver);
  Anthropic key still out of credit (OpenAI funded + verified).
- **Beyond the plan:** future directions (lineage/search as TUI commands, analysis
  features, another import format, and a GUI front-end) are captured in
  [`docs/roadmap.md`](docs/roadmap.md) — intent only, nothing scheduled.
- **Last updated:** 2026-06-26 (real-use ready: `.`-prefix, width-aware results,
  `.clear`/`.ai help`/SQL linter; all docs synced)
- **Notes:** `go.mod` is on Go 1.25.7 (bumped by go-mssqldb). `GOTOOLCHAIN=auto`
  auto-downloads the toolchain (no sudo). `gh` not installed — use plain `git`.
  Non-Postgres test DB creds are in the `test-databases` memory; MariaDB uses a
  dedicated TCP user `mcli`/`mcli_test` (root there is unix_socket-only).
  **GOTCHA (revised):** `go-db2` is only used under `-tags db2`. The PLAN
  previously warned a bare `go mod tidy` would prune it — but the Go 1.25.7
  `tidy` actually KEEPS tag-gated requires (verified: go-db2 survived a bare
  tidy, now a direct require). Still, double-check `go build -tags db2 ./...`
  after any `go mod tidy`.

---

## Phase 0 — Project bootstrap
- [x] `go.mod` (`module github.com/Solifugus/mcli`, Go 1.24 for now — bump to 1.25 at Phase 2)
- [x] `cmd/mcli/main.go` run-mode dispatch (TUI default vs `mcp serve`, plus help/version)
- [x] Package skeleton: `internal/core/{config,workspace,history}`, placeholder `internal/{tui,mcp,ai}`
- [x] `.gitignore` (binaries, build output, OS cruft)
- [~] CI: workflow written but **parked** at `.github/ci.yml.disabled` — the push token
  lacks `workflow` scope so it can't live under `.github/workflows/`. See `.github/CI.md`
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
- [x] `.enter` and workspace commands (§12), `.help`, `.quit`, scrollback via `tea.Println`
- [x] `core` facade (`internal/core`) shared by both front-ends; `cmd/mcli` launches the TUI
- [x] History ring (Up/Down) and Tab completion (commands + workspace names; files in Phase 4)
- [x] Chroma single-line syntax highlighting (dialect by connection) — done with Phase 3
- [x] Prompt context + environment color (§18) — done with Phase 3
- [ ] Bracketed-paste routing (multi-line paste opens `.edit`) — deferred to Phase 4 (needs `.edit`)

## Phase 3 — First database adapter (pure Go)
- [x] Adapter interface + registry (`internal/core/adapter`, §22)
- [x] PostgreSQL via pgx (`internal/adapters/postgres`); registered through `internal/adapters`
- [x] `.connect` / `use` / `.list` / `.describe` / bare-SQL execution (§13–14)
- [x] Queries run as `tea.Cmd` with `context` cancel on Ctrl-C (prompt snapshot avoids races)
- [x] `max_rows_default` guardrail + aligned inline result table
- [x] Live verification against a real Postgres (PG 17.10, gbasic_site_dev): connect,
      list databases/tables, describe (PK detection), streaming query — all confirmed.
      Fixed `.pgpass` fallback for discrete params (build a keyword DSN, parse once)
- [x] Ride-alongs: Chroma single-line syntax highlighting (dialect-aware, cursor
      overlay, coalesced spans) and env-color prompt (§18: dev green / test·stage
      yellow / prod red / unknown gray), gated by the `color_prompt` setting

## Phase 4 — Grid surface, SQL files, external editor
- [x] `.files`, `.edit` (external editor via `tea.ExecProcess`, resolution order §11), `.run`, `.cat`, `.copy`, `.rename`, `.delete` — verified incl. editor suspend/resume
- [x] Log file operations to history (WRITE/EDIT/COPY/RENAME/DELETE)
- [x] Tab completion for file commands
- [x] Alt-screen grid mode (`bubbles/table`, vertical paging/scroll, Esc/q to return);
      horizontal scroll for very wide rows deferred (columns capped at 60, clipped to width)
- [x] "Open full result in grid" via `.grid` from the last query (fetches up to
      gridRowCap=10000; inline shows first max_rows with a `.grid` hint)
- [x] Bracketed-paste routing: multi-line paste parks in scratch.sql and opens `.edit`

## Phase 5 — Import / export (`internal/core/transfer`)
- [x] CSV export, then CSV import (`.export query|table|current to <path>`, `.import <path> into <table>`)
- [x] TSV and pipe-delimited (delimiter inferred from extension: .csv/.tsv/.psv)
      — verified round-trip against real Postgres (export → import → counts match)
- [x] Excel `.xlsx` (export query/table/current; import with optional `sheet <name>`) —
      verified round-trip against real Postgres; pure-Go via `xuri/excelize/v2`
- [x] Fixed-width flat files (`.txt`/`.fix`): export derives widths from the data —
      **default** buffers up to 10000 rows (notes truncation, points at `exact`);
      **`exact`** keyword runs a two-pass streaming scan (measure, then re-run + write)
      for flat memory with nothing curtailed. Import requires explicit `widths N,N,...`
      (files are non-self-describing) and inserts positionally. Round-trip-verified
      against real Postgres incl. NULL→blank→NULL. Flat-file grid editing deferred (§11).

## Phase 6 — Additional adapters
- [x] SQL Server (`microsoft/go-mssqldb`, pure Go; type `sqlserver`, DialectTSQL).
      Brought forward because a live dev VM was available. Code-complete + unit-tested;
      reached TDS login against the VM. Live data round-trip pending the real password
      (the committed `ass` DSN shows a placeholder). Added a per-server `options` map
      (config.Server.Options → ConnectParams.Params) so flags like `encrypt=disable`
      are explicit rather than an insecure adapter default.
- [x] MySQL / MariaDB (`go-sql-driver/mysql`, pure Go; type `mysql`, DialectMySQL) —
      **live-verified** against local MariaDB: connect, list, describe (PK via
      COLUMN_KEY), typed query, CSV export/import round-trip, fixed-width export.
      Required dialect-aware identifier quoting in core (MySQL backticks vs standard
      double quotes) since `"id"` is a string literal in MySQL without ANSI_QUOTES.
      ExplainQuery implemented (renders `EXPLAIN` rows). schema==database mapping.
- [x] Oracle (`sijms/go-ora/v2`, pure Go; type `oracle`, DialectOracle) —
      **live-verified** against the oracle-free 23ai container: connect, schema
      navigation (`use` → ALTER SESSION SET CURRENT_SCHEMA on a single pinned
      connection; ListDatabases/Schemas = non-system `all_users`), list/describe
      (PK via all_constraints), typed query, CSV export/import round-trip. Needed
      two Oracle-specific accommodations: (1) the import path emits
      `INSERT ALL … SELECT 1 FROM dual` for Oracle since it rejects multi-row
      `VALUES (a),(b)` (dialect-aware in core/transfer); (2) the adapter pins ISO
      `NLS_DATE_FORMAT`/`NLS_TIMESTAMP_FORMAT` and renders `time.Time` to match, so
      DATE columns round-trip through text-literal import.
- [x] DB2 behind `-tags db2` (pure-Go `obaydullahmhs/go-db2`; type `db2`,
      DialectDB2). Adapter code-complete and compiles (`go build -tags db2`),
      unit-tested (DSN/registration), with standard Db2 SQL (SYSCAT catalog,
      `SET CURRENT SCHEMA`, double-quote idents, multi-row VALUES; date round-trips
      as date-only). Default build stays pure-Go — db2 import lives in a tagged
      file (`adapters_db2.go`). **UNVERIFIED against a live server:** the chosen
      pure-Go driver is a self-described WIP whose DRDA prepare (`PRPSQLSTT`) fails
      on every statement against Db2 Community 11.5 (connects/pings fine, then EOF).
      User chose to keep the pure-Go adapter and revisit when the driver matures;
      the CGo `ibmdb/go_ibm_db` is the fallback if working DB2 is needed sooner.

## Phase 7 — Server management and safety hardening
- [x] `.server list` and `.server show <name>` (read-only) brought forward for usability;
      bare `.connect` lists available servers; Tab completes server names + `.list` targets
- [x] Safety core (`internal/core/safety`, §17): a pure classifier (read-only /
      write / dangerous; comments + literals blanked so a WHERE or keyword hiding
      in one can't fool it) + a Policy that decides Allow/Confirm/Block from the
      verdict and the connected server's environment. Settings gained `read_only`,
      `block_dangerous_on_prod`, `dangerous_sql`. Core enforces Block in
      RunStatement (safety net for every front-end); Confirm is a front-end job.
      TUI: a reusable interactive sub-prompt primitive (modePrompt + pending) —
      every SQL entry point funnels through `guardedSQL`; `.readonly [on|off]`.
- [x] `.server add/edit/remove/test` (§13): core CRUD persisting servers.json with
      validation, plus TestServer (throwaway dial). TUI add/edit run an interactive
      wizard built on the sub-prompt primitive (one field per line, blank = keep
      default, re-ask on validation error, Esc cancels). Live-verified add+test
      against local Postgres.
- [x] Password sources: keyring (`zalando/go-keyring`) with `prompt`/`env:`
      fallback. resolvePassword now handles env:VAR and keyring (miss or
      unavailable → `ErrPasswordRequired`, the headless fallback §7); `prompt`
      always returns it. Connect/TestServer return ErrPasswordRequired; the TUI
      catches it (in the background op, keeping keyring off the UI thread), opens a
      MASKED prompt, and retries via ConnectWithPassword/TestServerWith. `.server
      set-password`/`clear-password` store/remove a keyring secret. Keyring access
      unit-tested via `keyring.MockInit()`.

## Phase 8 — AI assistance (`internal/ai`)
- [x] OpenAI-compatible chat client (`internal/ai`): minimal `/chat/completions`
      body (model + messages; temperature omitted for max compatibility),
      base_url defaults to OpenAI so the same client serves local Ollama (no key)
      and hosted providers. Prompt/context assembly (system prompt grounded in
      dialect/environment/database + capped table-name schema hint) lives here.
- [x] `ai.json` providers loaded into the core; `.ai providers` lists them and
      marks the default. Provider resolution picks the configured default (or the
      sole provider) and resolves `api_key_source` (none / env:VAR).
- [x] `.ai ask <q>`, `.ai explain <file|current>`, `.ai fix <file|current>` (§20).
      The TUI tracks the last statement and its error so `current` works and
      `fix` can include the DB error. Runs as a background op; AI never executes
      SQL — output is text for the user to review and run.
- [x] Schema-context support gated by `send_schema_context` (capped at 60 tables).
- [~] Live-verified up to the billing wall: both the OpenAI key (env
      OPENAI_API_KEY) and Anthropic key (via its OpenAI-compatible endpoint)
      AUTHENTICATE and reach the real APIs with the correct request shape — both
      return billing errors (OpenAI 429 quota; Anthropic 400 low credit), cleanly
      surfaced. No local Ollama was available for a free completion. The full
      path (auth, endpoint, request/response, error parsing) is proven against two
      real providers; an actual completion awaits credits or a local model.
- [ ] Deferred to a later pass: `.ai generate import for <path>`, `.ai lineage`.

## Phase 9 — MCP server (`internal/mcp`)
- [x] Self-contained stdio JSON-RPC 2.0 transport (newline-delimited; initialize/tools.list/tools.call/ping, notifications ignored, parse/method errors)
- [x] 19 tools, each a thin wrapper over a core function: workspaces (list/enter/status), servers (list/connect), schema (databases/use/tables/views/describe/search columns+views), files (list/read/write), query (run_query/run_saved_sql), transfer (export_query/import_file)
- [x] `mcli mcp serve` (headless over stdin/stdout, SIGINT-clean) wired in `cmd/mcli`
- [x] `.mcp serve` in the TUI via `tea.Exec` custom `ExecCommand` — hands the suspended terminal's stdio to the same in-process server, returns on Ctrl-C/EOF
- [x] Safety controls applied identically to the TUI: read-only/dangerous/prod guards via core `GuardStatement`; dangerous statements refused unless `confirm=true` (the headless analogue of the interactive prompt); secrets never returned (curated server view, no connection_string)
- [x] Two thin core wrappers added (`SearchColumns`/`SearchViews`) so search tools stay thin
- [x] Tests: protocol (initialize/echo version, notification→no reply, tools/list, unknown method, parse error), tool calls (list_workspaces, status, unknown tool, no-connection, dangerous-refused, confirm-bypasses, write→read round-trip)
- [x] Live-verified against `gbasic` Postgres: connect → list_tables (6 tables) → run_query (real rows) → describe_table (PK detected) → DELETE-without-WHERE refused

## Phase 10 — Built-in editor
- [x] `safety.StatementSpans`/`StatementAt`: comment/string-aware semicolon split (basis for "statement under the cursor", future multi-statement .run/MCP) — tested
- [x] `editorModel` (`internal/tui/editor_builtin.go`): line buffer, cursor, viewport scroll, insert/overwrite, keyboard selection, multi-line paste, statement-at-cursor — Bubble-Tea-free
- [x] Live Chroma highlighting reusing `highlight.go` via the extracted `renderLineSpans` (per-line; cursor + selection overlay; INS underline / OVR block cue)
- [x] New alt-screen `modeEditor` wired into the root model behind `.edit` + `"editor": "builtin"` (entry, View, resize, paste)
- [x] `handleEditorKey` (`internal/tui/editor_keys.go`): ^R run, ^S save, ^Y copy (OSC 52), Ins overwrite, Esc quit (dirty-save prompt), movement/editing
- [x] SQL-aware execution through the same guard as the REPL; results reuse the grid and return to the editor (Esc); dangerous statements gated by confirm
- [x] Tests: statement split, buffer/cursor/selection ops, statement-at-cursor, Ctrl-S save, run-without-connection, clean-Esc; live-verified run + dangerous-gate against `gbasic`

## Phase 11 — Live-table grid editing (optional / later)
- [ ] PK-aware editable grid generating DML through the safety layer — only if it proves worth it over `.edit` + `.run`
