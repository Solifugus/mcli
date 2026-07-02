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
- **GUI extension (Phases 12–15) underway.** A native graphical front-end plus an
  AI guidance channel are designed in `docs/mcli-design.md` §25–§28. Decisions
  taken: native toolkit (Fyne recommended), bound directly to `core`, shipped as a
  **separate `-tags gui` artifact** so the default binary stays pure-Go.
- **Phase 12 (unified object finder) is DONE** — the typed object finder
  (`SearchObjects`, kinds table/view/procedure/function) landed in the core, all
  adapters, the TUI (`.objects`/`.find`), and MCP (`search_objects`), live-verified
  end-to-end against `gbasic`.
- **Phase 13 (assist channel + live AI session) is DONE** (one hardening follow-up
  tracked) — `internal/core/assist` bus + vocabulary, `ui_*` MCP tools, a loopback
  **MCP Streamable HTTP** live endpoint (token + Origin + session.json), and the TUI
  `.assist on/off` renderer that stages AI prefills on the input line. Verified
  end-to-end under `-race`: an agent POSTs `ui_prefill` and it lands on the CLI input
  unsubmitted. **This delivers "the AI guides the user in the CLI."** Remaining
  follow-up: a coarse `Core` mutex for cross-goroutine safety when the live session
  runs real DML concurrently with the TUI user.
- **Phase 14 (native GUI shell) is DONE** — `internal/gui` (Fyne, `-tags gui`) is a
  third thin client of `core`: `mcli gui` launch mode, connect dialog (reusing the
  core password sources + `ErrPasswordRequired` fallback), the object finder panel
  (type checkboxes + search box over `core.SearchObjects`), a SQL editor over a
  paged `widget.Table` grid fed by `RunQuery`/`RowStream`, the safety guard rendered
  as GUI dialogs (Block → info, Confirm → yes/no), import/export dialogs, and an
  environment-colored status bar. The default `go build` stays pure-Go (verified
  with `CGO_ENABLED=0`); only `-tags gui` pulls in CGo. Headless Fyne-`test` coverage
  green; full GUI binary builds and links.
- **Capability-area roadmap adopted (Phases 16–22).** Design discussion settled a
  four-area model (Data / Processing / Scheduling / Security) that both front-ends
  share — the GUI's nav dropdown, the CLI's command groups. Decisions taken: build
  **all four** areas incl. Security editing and the lineage flow chart; **hybrid**
  capability model (lean base `Adapter` + optional interfaces + a `Capabilities()`
  advertisement); **core primitives first**, before the deferred Phase 15 GUI assist
  renderer. Everything lands core+CLI first, GUI consumes later (parity, §28).
- **Phase 16 (capability layer) is DONE** — the load-bearing foundation. New
  `adapter.Capability`/`CapabilitySet` (+ `Caps`, `AllCapabilities`, `Sorted`), a
  `Capabilities()` method on the `Adapter` interface implemented by all five
  adapters (Postgres/MySQL/DB2 advertise `explain`; MSSQL/Oracle advertise nothing
  yet), plus `adapter.ErrUnauthorized` to distinguish "engine can't" from "you lack
  privileges." Surfaced through the core (`Capabilities`, `Supports`, and the
  previously-hidden `Explain`/`PreLineage`/`PostLineage`), the CLI (`.caps`), and
  MCP (`get_capabilities`). Tests cover the set algebra, the disconnected-empty
  contract, per-adapter advertisement, and both front-ends.
- **Phase 17 (source retrieval + body search) is DONE** — the `AdapterSource`
  optional interface (`Source` + `SearchRoutines`) advertised via `CapSource`,
  implemented across all five adapters (view/procedure/function definition text;
  MSSQL uses untruncated `sys.sql_modules`, Oracle `DBMS_METADATA.GET_DDL`). Surfaced
  through the core (type-asserts the interface, `ErrUnsupported` fallback), the CLI
  (`.source`, `.grep`), and MCP (`get_source`, `search_routines`). **Live-verified on
  Postgres.** Tables are intentionally excluded — no stored definition; use
  `.describe`.
- **Phase 18 (table-valued functions) is DONE** — an additive `KindTableFunction`
  + `AdapterTableFunctions` (`SearchTableFunctions`) advertised via
  `CapTableFunctions`, so the existing name/kind finder is untouched. Classification
  per engine (PG `proretset`, MSSQL `IF/TF/FT`, DB2 `functiontype='T'`; MySQL none,
  Oracle deferred). Pure `adapter.TabularQuery` builds the dialect-correct
  `SELECT * FROM f(...)` / `TABLE(f(...))`. Surfaced through core, CLI (`.tablefuncs`
  /`.tvf`, listing TVFs with their query template), and MCP
  (`search_table_functions`). **Live-verified on Postgres** (set-returning classified,
  scalar excluded). This **completes the Data area's core primitives**.
- **Phase 19 (Scheduling — jobs/agents) is DONE** — `AdapterJobs`
  (`ListJobs`/`DescribeJob`/`JobHistory`) + `CapJobs`, implemented for SQL Server
  Agent (msdb), Oracle DBMS_SCHEDULER, and MySQL Events; Postgres greys out (no
  scheduler), DB2 deferred. CLI `.jobs`/`.job [--history]`, MCP
  `list_jobs`/`describe_job`/`job_history`. Not live-verifiable here (PG has no
  scheduler); pure MSSQL date/status/duration formatters are unit-tested.
- **Phase 20 (Security read-only) is DONE** — `AdapterSecurity`
  (`ListPrincipals`/`DescribePrincipal`) + `CapSecurity`, implemented for Postgres
  (`pg_roles`, live-verified), SQL Server (`sys.database_principals`), Oracle (`dba_*`
  views), and MySQL (`mysql.user` + `SHOW GRANTS`, roles best-effort); DB2 deferred
  (OS-based auth). CLI `.users`/`.roles`/`.user`/`.role`, MCP
  `list_principals`/`describe_principal`.
- **Phase 21 (Security editing — guarded DCL) is DONE** — `CapSecurityEdit` +
  pure dialect-aware builders (`GrantStatement`/`CreateUserStatement`/
  `DropUserStatement`) with injection-rejecting validation. Builders only BUILD; every
  generated GRANT/REVOKE/CREATE/DROP executes through the **one** guarded path
  (`GuardStatement`), so read-only blocks and prod confirms — no second unguarded path.
  CLI `.grant`/`.revoke`/`.createuser`/`.dropuser`, MCP `grant`/`create_user`/
  `drop_user`. Comprehensive DB-free builder tests + live-PG CREATE→GRANT→REVOKE→DROP
  round-trip (skips without CREATEROLE). **This completes the Security area and all of
  Phases 16–21 (the four-area capability model).**
- **Phase 22 (Lineage flow chart) is DONE.** Real one-hop `GetPreLineage`/`GetPostLineage`
  per engine (PG `pg_depend`/`pg_rewrite`, MSSQL `sys.sql_expression_dependencies`,
  Oracle `all_dependencies`, MySQL `VIEW_TABLE_USAGE`; DB2 deferred), `CapLineage`
  advertised. The **graph is assembled in the core** (`Lineage` walks one-hop edges
  breadth-first, cycle-safe, depth/size-bounded, edges normalized to data-flow
  direction) so all front-ends render the same edge set. CLI `.pre-lineage`/
  `.post-lineage <object> [depth]` (indented tree), MCP `get_lineage`. Pure BFS unit
  tests + live-PG table→view→view round-trip. **The four-area roadmap (16–22) is now
  fully implemented.** **Next up: Phase 15** (GUI assist renderer) — the only deferred item.
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
- **Output coloring polish:** `.help` is now grouped into colored sections (magenta
  headings, blue command names, dim argument syntax, aligned descriptions) with a
  faint alternating stripe on every other row; the simple object tables (`.server
  list`, `.ai providers`) and query/`.describe` results get a bold header, a dim
  rule, and the same subtle row striping, plus a dimmed result summary. All of it is
  gated on the existing `color_prompt` setting (plain text when off). The stripe
  background adapts to the terminal's light/dark background (queried at startup via
  `tea.RequestBackgroundColor`); styles live in `internal/tui/style.go`.
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
- **Last updated:** 2026-06-27 (output coloring polish: grouped/colored `.help`,
  colored table headers + subtle row striping, background-adaptive stripe)
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

---

# GUI extension (Phases 12–15)

Adds a native graphical front-end and an AI guidance channel. Design: `docs/mcli-design.md`
§25 (GUI front-end), §26 (assist channel / live AI session), §27 (object finder),
§28 (front-end parity). Decisions taken: **native toolkit (Fyne recommended), bound
directly to `core`**, shipped as a **separate `-tags gui` build artifact** so the
default binary stays pure-Go / no-CGo. Build order is core-first: each phase is
useful on its own and earlier phases need no GUI.

## Phase 12 — Unified object finder (core + adapters) ✅
- [x] `adapter.ObjectKind` + `AllObjectKinds()`; `adapter.SearchObjects(ctx, kinds, substr)` added to the interface, covering tables, views, **procedures, functions**; `ListTables`/`ListViews` are now thin wrappers over it (§27)
- [x] Per-adapter catalog queries: postgres (`information_schema.routines`), mysql (`ROUTINES`, scoped to `DATABASE()`), mssql (`INFORMATION_SCHEMA.ROUTINES`), oracle (`all_objects`, `UPPER()`), db2 behind `-tags db2` (`syscat.routines`, `UCASE()`); unknown kinds contribute nothing; empty substr matches all
- [x] `core.SearchObjects` (safety-neutral catalog read; `ErrNotConnected` guard)
- [x] TUI `.objects` / `.find` command: kind tokens in any order (singular/plural/short forms, deduped) + one name substring; typed result table (`type`/`object`) reusing the styled renderer; completion + `.help` entry
- [x] MCP `search_objects` tool (`kinds[]` enum, `substring`, both optional)
- [x] Tests: adapter live (`TestLiveSearchObjects` vs `gbasic`), TUI `parseObjectArgs` table test, MCP tools/list + no-connection. Full `go test ./...` green; **end-to-end live-verified** via `mcli mcp serve` → connect gbasic → `search_objects` (7 tables; `"user"` substring → `gbasic_site_users`)
- [ ] GUI checkbox+search panel — deferred to Phase 14 (needs the GUI shell)

## Phase 13 — Assist channel + live AI session (§26)
- [x] `internal/core/assist`: fan-out event `Bus` + vocabulary (`Highlight`/`Focus`/`Prefill`/`Annotate`/`Demo` + `Step`) keyed by semantic target ids; non-blocking Publish, drop-on-backpressure, `HasSubscribers`. Unit-tested
- [x] Core wiring: `Core.Assist()` (the bus), `Core.Guide(event)` (publishes; `ErrNoLiveSession` when nothing attached), `Core.LiveSession()`
- [x] `ui_describe_screen` / `ui_highlight` / `ui_focus` / `ui_prefill` / `ui_annotate` / `ui_demo` MCP tools (report no live session when nothing attached; non-destructive). Tested: tools/list, no-live-session error, delivery to an in-process subscriber
- [x] Live-session transport (`internal/mcp/http.go`): the running app hosts an **MCP Streamable HTTP** endpoint on a loopback port bound to the **live** core, reusing the existing JSON-RPC dispatch. Security: 127.0.0.1-only, per-session bearer token, `Origin` validation; discovery via `~/.mcli/session.json` (0600). Headless `mcli mcp serve` (stdio, own core) unchanged. Chosen for heterogeneous agents (user's own **Conatus** + others); matches the `2025-06-18` version the server already advertises
- [x] `.assist on|off|status` TUI command — **opt-in** (off by default, since mcli may be on prod). `on` starts the endpoint + subscribes the renderer and prints url/token; `off`/quit close it and remove session.json
- [x] TUI assist renderer: subscribes to the bus, stages `prefill` on the input line **without submitting**, prints highlight/focus/annotate notes and numbered `demo` walkthroughs. Re-arms after each event
- [x] Tests (all under `-race`): HTTP auth/Origin/method rejection, initialize+tools/list, 202-on-notification, session.json write/remove, concurrent requests; **end-to-end TUI test** — `.assist on` → agent POSTs `ui_prefill` over HTTP → text lands on the input line unsubmitted
- [ ] **Follow-up (tracked):** cross-goroutine core locking. The HTTP endpoint serializes its *own* dispatch, but data-mutating tools (connect/use/run) invoked over HTTP share `*core.Core` with the TUI's tea.Cmd goroutines. The guidance path is safe (the assist bus is synchronized); a coarse `Core` mutex around connection lifecycle is the planned hardening before the live session drives real DML concurrently with the user

## Phase 14 — Native GUI shell (§25) — DONE ✅
- [x] `internal/gui` behind `-tags gui`; `mcli gui` launch mode; default build stays pure-Go
      (`gui_stub.go`/`gui_run.go` split; verified with `CGO_ENABLED=0`)
- [x] Direct-core binding; paged result grid over `RowStream` (`widget.Table`, capped at
      `gridFetchCap`, column auto-width)
- [x] Object finder panel (Phase-12 finder: type checkboxes + search box over `core.SearchObjects`,
      Describe dialog), connect dialog (core password sources + `ErrPasswordRequired` password prompt),
      SQL editor, import/export dialogs
- [x] Safety guards inherited from core, rendered as GUI dialogs (Block → info, Confirm → yes/no)
      and a read-only toggle bound to core policy; the guard decision is `core.GuardStatement`, not GUI code
- [x] Headless `fyne/test` coverage (finder kinds, not-connected hint, grid model, env color, isQuery/toStrings)

  **Build note (Linux):** the `-tags gui` build needs a C toolchain plus X11 dev headers
  (`libxcursor-dev libxrandr-dev libxinerama-dev libxi-dev libxxf86vm-dev`, and their
  `Xrender`/`Xfixes` deps). Runtime `.so`s are the usual desktop libs. The default pure-Go
  binary needs none of this.

  **Deferred within 14 (nice-to-have):** schema/database tree navigator (the finder covers
  object discovery for now); per-query cancel button (`context` plumbing exists, no UI yet);
  native `.grid`-style full-cell inspector.

## Phase 15 — GUI assist renderer + AI-guided demos (§26) — deferred behind core primitives
- [ ] GUI registry mapping semantic target ids → widgets
- [ ] Canvas overlay for pulse/highlight; programmatic focus/prefill; step-through coachmarks
- [ ] Same `ui_*` tools drive the GUI — AI can blink buttons, prefill fields, walk the user through a task live

## Capability-area expansion (Phases 16–22)

Four shared areas — **Data / Processing / Scheduling / Security** — surfaced in the
GUI as a nav dropdown and in the CLI as command groups. Each area is a *core*
capability first; the GUI is a later consumer. Sequenced core-first (decision:
before Phase 15).

## Phase 16 — Capability layer (foundation) ✅
- [x] `adapter.Capability` + `CapabilitySet` (`Has`/`Sorted`/`Caps`/`AllCapabilities`)
- [x] `Capabilities()` on the `Adapter` interface, implemented by all five adapters
      (PG/MySQL/DB2 → `explain`; MSSQL/Oracle → none yet)
- [x] `adapter.ErrUnauthorized` — "you lack privileges" vs. `ErrUnsupported` "engine can't"
- [x] Core surface: `Capabilities`, `Supports`, and the previously-hidden `Explain` /
      `PreLineage` / `PostLineage`
- [x] CLI `.caps` (+ help + completion); MCP `get_capabilities`
- [x] Tests: set algebra, disconnected-empty, per-adapter advertisement, TUI + MCP faces

## Phase 17 — Source retrieval + body search (Data-design, Processing-code) ✅
- [x] `AdapterSource` optional interface (`Source` + `SearchRoutines`) + `CapSource`
- [x] `Source(name)` for view/procedure/function text across all five adapters —
      PG `pg_get_functiondef`/`pg_views`, MySQL `*_DEFINITION`, MSSQL `sys.sql_modules`
      (untruncated), Oracle `DBMS_METADATA.GET_DDL`, DB2 `syscat.views`/`syscat.routines`
- [x] Search-within-bodies for routines (`SearchRoutines`, per-engine body column)
- [x] Core surface (type-asserts `AdapterSource`, `ErrUnsupported` fallback); CLI
      (`.source`, `.grep`) + MCP (`get_source`, `search_routines`)
- [x] Tests: core not-connected, per-adapter `CapSource`, MCP tool list + no-conn,
      TUI usage; **live Postgres** (create view → read `Source`; create fn → body-find)
      verified green against `gbasic_site_dev`. Tables intentionally excluded (no stored
      definition — use `.describe`).

## Phase 18 — Table-valued functions (Data area completion) ✅
- [x] `KindTableFunction` (additive — NOT in AllObjectKinds, so the name finder is
      untouched); `AdapterTableFunctions.SearchTableFunctions` + `CapTableFunctions`
- [x] Classification: PG `proretset`, MSSQL `IF/TF/FT`, DB2 `functiontype='T'`;
      MySQL has none, Oracle deferred (fuzzy to classify) — both documented in their
      Capabilities()
- [x] `adapter.TabularQuery(dialect, ref)` pure builder — `SELECT * FROM f(...)` vs
      Oracle/DB2 `SELECT * FROM TABLE(f(...))`; core `SearchTableFunctions` +
      `TabularQuery`
- [x] CLI `.tablefuncs [substr]` / `.tvf` (lists TVFs + query template); MCP
      `search_table_functions` (returns refs + dialect-correct query)
- [x] Tests: `TabularQuery` dialect matrix, PG/MySQL capability advertisement, core
      not-connected; **live Postgres** — `RETURNS TABLE` classified as table-valued,
      scalar excluded

## Phase 19 — Scheduling (jobs / agents) ✅
- [x] `AdapterJobs` optional iface (`ListJobs` / `DescribeJob` / `JobHistory`) +
      `JobRef`/`Job`/`JobStep`/`JobRun` types + `CapJobs`; core probes the iface,
      returns `ErrUnsupported` off it
- [x] SQL Server Agent (msdb `sysjobs`/`sysjobsteps`/`sysjobschedules`/`sysjobhistory`,
      step 0 = job outcome, packed-int date/time/duration formatted in Go), Oracle
      DBMS_SCHEDULER (`all_scheduler_jobs` + `..._job_run_details`), MySQL Events
      (`information_schema.EVENTS`; no run history → empty, not an error). **PG greys
      out** (no scheduler — does not implement the iface); **DB2 deferred** (Admin
      Task Scheduler often unconfigured, can't live-test)
- [x] Core + CLI (`.jobs [substr]`, `.job <name>` design / `.job <name> --history [N]`) +
      MCP (`list_jobs`/`describe_job`/`job_history`); help + completion + `.caps` row
- [x] Tests: pure MSSQL formatters (status/date/duration), capability advertisement
      (MSSQL/Oracle/MySQL have CapJobs; PG must not + must not implement AdapterJobs),
      core not-connected, MCP tools-in-list + no-connection. Not live-verifiable here
      (PG has no scheduler); SQL/Oracle paths follow the existing catalog idioms

## Phase 20 — Security read-only (users / roles / grants) ✅
- [x] `AdapterSecurity` optional iface (`ListPrincipals` / `DescribePrincipal`) +
      `PrincipalRef`/`Principal`/`Grant` types + `PrincipalKindUser`/`Role` consts +
      `CapSecurity`; core probes the iface, returns `ErrUnsupported` off it.
      `ErrUnauthorized` reserved for logins lacking catalog privilege (natural driver
      error propagates)
- [x] Postgres (`pg_roles` — user = canlogin, role = !canlogin; `pg_auth_members`;
      `role_table_grants`), SQL Server (`sys.database_principals` types S/U/G=user,
      R=role; `sys.database_role_members`; `sys.database_permissions`), Oracle
      (`dba_users`/`dba_roles`/`dba_role_privs`/`dba_sys_privs`/`dba_tab_privs` — needs
      catalog priv), MySQL (`mysql.user` as user@host + `SHOW GRANTS`; roles fuzzy →
      best-effort, kind=role empty). **DB2 deferred** (OS-based auth, different model)
- [x] Core + CLI (`.users [substr]`, `.roles [substr]`, `.user`/`.role <name>`) + MCP
      (`list_principals`/`describe_principal`); help + completion + `.caps` already
      lists CapSecurity
- [x] Tests: pure helpers (MSSQL principalKind/typeLabel, MySQL splitAccount/escape),
      capability advertisement (all four have CapSecurity), core not-connected, MCP
      tools-in-list + no-connection. **Live Postgres** — lists roles, filters users,
      describes the connecting login as a LOGIN user, missing principal errors

## Phase 21 — Security editing (guarded DCL) ✅
- [x] `CapSecurityEdit` (advertised by PG/MSSQL/Oracle/MySQL; DB2 deferred). Pure,
      dialect-aware builders in `adapter/securityedit.go`: `GrantStatement`
      (privilege grant with ON, or role grant without; grant/revoke), `CreateUserStatement`
      (PG `CREATE USER ... PASSWORD`, MySQL `CREATE USER 'u'@'h' IDENTIFIED BY`, MSSQL
      `CREATE LOGIN ... WITH PASSWORD`, Oracle `CREATE USER ... IDENTIFIED BY`),
      `DropUserStatement`. Identifier/privilege validation + password literal escaping
      reject injection
- [x] **Builders only build**; execution routes back through the ONE guarded path
      (`GuardStatement` + `RunStatement`) — read-only **blocks** GRANT/CREATE (non-read
      writes), prod **confirms**, DROP is **dangerous** (confirm / block-on-prod). No
      second unguarded execution path
- [x] Core `BuildGrant`/`BuildCreateUser`/`BuildDropUser` (gate on conn +
      `CapSecurityEdit`); CLI `.grant`/`.revoke`/`.createuser`/`.dropuser` (echo the
      generated statement, then `guardedSQL`); MCP `grant`/`create_user`/`drop_user`
      (build then `runSQL` with confirm) — help + completion
- [x] Tests: comprehensive pure-builder matrix across all dialects + injection
      rejection + password escaping (fully DB-free); guard-contract test
      (`safety.Classify` on generated GRANT/CREATE/DROP); core not-connected; MCP
      tools-in-list + no-connection; TUI `parseGrantArgs` + usage. **Live Postgres**
      end-to-end CREATE→GRANT→REVOKE→DROP round-trip (skips gracefully when the login
      lacks CREATEROLE — the `ErrUnauthorized` boundary)

## Phase 22 — Lineage flow chart ✅
- [x] Real `GetPreLineage`/`GetPostLineage` per engine (`CapLineage`): PG
      (`pg_depend`/`pg_rewrite`), MSSQL (`sys.sql_expression_dependencies`), Oracle
      (`all_dependencies`), MySQL (`VIEW_TABLE_USAGE`, views only); DB2 deferred
- [x] Edges assembled in the core: `Lineage(name, dir, depth)` walks the one-hop
      accessors breadth-first — cycle-safe, depth- and node-bounded (`Truncated`),
      edges normalized to data-flow direction; `Children` helper for tree rendering
- [x] CLI `.pre-lineage`/`.post-lineage <object> [depth]` (indented tree, cycle-marked);
      MCP `get_lineage` (direction + depth → graph JSON); `.caps` already lists lineage
- [x] Tests: pure BFS matrix (multi-level, cycles, depth bound, node cap, direction,
      error propagation, `Children`), gating, MCP tool-in-list/no-conn, TUI usage +
      render, live-PG table→view→view round-trip (skips without CREATE)
