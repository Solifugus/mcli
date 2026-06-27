# mcli Design

> **Status (2026-06-26):** This document is the design record and the contract for
> *intent*. The project is now implemented and usable — Phases 1–10 of §24 are done,
> plus a SQL linter (`.lint`); only the optional live-table grid (Phase 11) remains
> deferred. Two deltas from the original text: (1) commands are **`.`-prefixed**
> (e.g. `.connect`), updated from the `\` shown historically here and now swept
> throughout; (2) a few exploration/lineage commands described below
> (`.search-views`, `.pre-lineage`, …) are currently exposed through the core and
> MCP tools rather than as TUI commands. See `README.md` for usage and `PLAN.md` for
> phase status.

## 1. Purpose

`mcli` is a multi-database command-line workbench for SQL development, data exploration, import/export tasks, and AI-assisted database work.

The core idea is simple: a user enters a workspace, and that workspace restores the working context needed for a task. This includes the current server, current database, SQL files, import/export folders, and history log.

`mcli` is meant to be fast, practical, and durable across interruptions. It should make it easy to switch between unrelated tasks without losing context. The interaction model is keyboard-first: a user should be able to keep their fingers moving with no mouse and no tricky key combinations.

## 2. Goals

* Support multiple SQL database systems from one CLI.
* Provide named workspaces for task-oriented work.
* Store SQL files per workspace.
* Remember the current server and database per workspace.
* Automatically reconnect when entering a workspace, when configured.
* Provide simple database exploration commands.
* Support import/export for delimited files, Excel files, and flat files.
* Maintain a per-workspace history log.
* Provide configurable AI assistance.
* Expose MCP tools so AI agents can use the same capabilities safely.
* Run as a single self-contained binary that cross-compiles cleanly to Linux (multiple architectures), macOS, and Windows.

## 3. Non-Goals

* `mcli` is not intended to be a full graphical IDE.
* `mcli` is not intended to replace database-native administration tools.
* `mcli` should not hide database-specific behavior when it matters.
* `mcli` should not require AI features to be useful.
* `mcli` should not store plaintext passwords by default.
* `mcli` does not, in its first cut, edit live database tables through a grid. Table edits are made by editing and running SQL, which keeps destructive operations visible and auditable (see §17, §12).

## 4. Technology Stack

| Concern | Choice | Notes |
| --- | --- | --- |
| Language | Go (1.25+) | Required floor is set by Bubble Tea v2. |
| TUI framework | Bubble Tea v2 (`charm.land/bubbletea/v2`) | Elm-architecture (Model/Update/View). Supports inline and full-window (alt-screen) rendering in one program. |
| UI components | Bubbles v2 (`charm.land/bubbles/v2`) | `textinput`, `textarea`, `table`, `viewport`, `spinner`, etc. |
| Styling | Lip Gloss v2 | Color downsampling is automatic; styling works across color depths. |
| Syntax highlighting | Chroma v2 (`github.com/alecthomas/chroma/v2`) | Pure Go. SQL / Transact-SQL lexers; terminal (ANSI) formatters. Used for the REPL prompt and, later, the built-in editor. |
| Secrets | `zalando/go-keyring` | Cross-platform keyring with documented fallbacks (see §7). |

Note on Bubble Tea v2 specifics that affect this build: `View()` returns a `tea.View` struct (not a string), keys arrive as `tea.KeyPressMsg`, native clipboard is available via OSC 52 (`tea.SetClipboard` / `tea.ReadClipboard`, works even over SSH), and bracketed paste is surfaced as a distinct event. Do not follow v1 examples; the import path and API differ.

### Cross-platform and Windows

The binary targets Linux (amd64/arm64 and other architectures), macOS, and Windows. Because every default-build database driver is pure Go (see §6), cross-compilation is a plain `GOOS`/`GOARCH` build with no C toolchain.

On Windows, the relevant variable is the **terminal host**, not the shell. The shell (`cmd.exe`, Windows PowerShell 5.1, or PowerShell 7 / `pwsh`) is not involved in rendering once the program is running. The host matters: Windows Terminal (default on Windows 11) gives truecolor and correct wide-character handling; the legacy console (`conhost`) is 16-color and weaker on Unicode. Bubble Tea v2 detects capabilities and downsamples color automatically, so the program works on all of these — the experience is simply best on Windows Terminal. The one place to code defensively is the external-editor handoff (§11), which must not assume a Unix editor exists.

## 5. Architecture Overview

`mcli` is layered as a **UI-agnostic core** with two front-ends that consume it. The TUI and the MCP server must expose the same capabilities, so neither owns domain logic — the core does.

```text
+-----------------------------+      +-----------------------------+
|        TUI front-end        |      |       MCP front-end         |
|  (Bubble Tea v2: REPL,      |      |  (stdio MCP server:         |
|   grid viewer, prompt)      |      |   tools over core)          |
+--------------+--------------+      +--------------+--------------+
               |                                    |
               +-----------------+------------------+
                                 |
                       +---------v----------+
                       |        Core        |
                       |  workspace manager |
                       |  server registry   |
                       |  adapter interface |
                       |  query execution   |
                       |  import / export    |
                       |  history logging   |
                       |  safety / guardrails|
                       +---------+----------+
                                 |
                    +------------v-------------+
                    |   Database adapters      |
                    | (pgx, mysql, mssql,      |
                    |  go-ora, db2, ...)       |
                    +--------------------------+
```

The core has no dependency on Bubble Tea or on the MCP layer. Safety settings (§17) live in the core so both front-ends inherit them rather than re-implementing them.

### Run modes

One binary, selected by invocation:

* `mcli` — launch the interactive TUI (default).
* `mcli mcp serve` — run the headless MCP server (no TUI; stdio). Equivalent to the `.mcp serve` command from inside the TUI.

### Suggested module layout

```text
cmd/mcli/                 main; selects run mode
internal/core/            UI-agnostic domain
  workspace/              workspace manager, workspace.json
  server/                 server registry, servers.json
  adapter/                adapter interface + registry
  query/                  execution, row streaming, result model
  transfer/               import / export (csv, tsv, pipe, xlsx, fixed)
  history/                per-workspace history log
  safety/                 dangerous-SQL detection, read-only, prod rules
  config/                 settings.json, ai.json, path resolution
internal/adapters/        one package per database, build-tagged
  postgres/  mysql/  mssql/  oracle/  db2/
internal/tui/             Bubble Tea v2 front-end
  model/                  root model + mode state machine
  repl/                   single-line input, history ring, completion, highlight
  grid/                   alt-screen table viewer / flat-file editor
  editor/                 .edit handoff (external now; built-in later)
  prompt/                 context + environment color
internal/mcp/             MCP server exposing core as tools
internal/ai/              AI provider clients, context assembly
```

## 6. Supported Databases

Target database systems and their drivers. The default build uses pure-Go drivers only, which is what makes cross-compilation trivial. CGo-requiring adapters are isolated behind Go build tags and are opt-in for the one machine that needs them.

| Database | Driver | Pure Go? | Build |
| --- | --- | --- | --- |
| PostgreSQL | `jackc/pgx` | Yes | default |
| MySQL / MariaDB | `go-sql-driver/mysql` | Yes | default |
| SQL Server | `microsoft/go-mssqldb` | Yes | default |
| Oracle | `sijms/go-ora` | Yes (no Instant Client) | default |
| DB2 | `obaydullahmhs/go-db2` (pure-Go DRDA) **or** `ibmdb/go_ibm_db` (CGo) | Choice | tagged |

Driver notes:

* Oracle uses `go-ora`, not `godror`. `godror` is CGo and requires Oracle's native client; avoid it unless a specific feature forces it.
* DB2 is the one real decision. `obaydullahmhs/go-db2` is a pure-Go DRDA driver (cross-compiles cleanly, but young and less battle-tested). `ibmdb/go_ibm_db` is mature but CGo, requires IBM's clidriver, may need a db2connect license for z/OS and i-series targets, and its tested Go-version range has lagged the 1.25 floor. Whichever is chosen, the DB2 adapter sits behind a build tag so it never blocks the default cross-platform build. DB2 is intentionally last in the plan (§24).

The adapter interface (§22) is identical regardless of driver, so the messy platform reality stays quarantined inside one package.

## 7. Core Concepts

### Workspace

A workspace is a named working context. It contains a distinct name, a current server, a current database, an auto-connect setting, default import/export directories, SQL files, notes or supporting files, and a history log.

A workspace is task-oriented, not database-oriented. Users often work across multiple databases during the same task, so the workspace remembers the current database but is not modeled as belonging to one database.

### Default Workspace

A default workspace always exists. On first run, `mcli` creates `~/.mcli/workspaces/default/`. Unless configured otherwise, `mcli` starts in the default workspace.

### Server

A server is a globally configured database connection target. Servers are shared across all workspaces and are not stored inside them. A server may represent a PostgreSQL, SQL Server, MySQL/MariaDB, Oracle, or DB2 instance (and a local SQLite database if supported later).

### Current Database

Each workspace remembers its current server and current database. When the user enters a workspace, `mcli` restores that context. If `auto_connect` is true and the saved server/database is not already connected, `mcli` connects automatically.

### SQL Files

SQL files live inside the current workspace, e.g. `~/.mcli/workspaces/consumer-lending/funded-refresh.sql`. Commands such as `.edit`, `.run`, and `.delete` operate relative to the current workspace.

### History Log

Each workspace has its own history log, e.g. `~/.mcli/workspaces/consumer-lending/history.log`, recording important actions in order:

```text
2026-06-24 15:42:11 ENTER workspace consumer-lending
2026-06-24 15:42:12 CONNECT sqlprod01 database ETLDB
2026-06-24 15:44:03 RUN funded-refresh.sql
2026-06-24 15:45:18 EXPORT query funded-refresh to exports/funded_june.xlsx
```

### Secrets and the keyring

`go-keyring` wraps the macOS Keychain, Windows Credential Manager, and the Linux Secret Service. On headless Linux without D-Bus / Secret Service, keyring access fails, so `prompt` and `env:` must always be available as fallbacks. `mcli` never stores plaintext passwords by default.

### Path resolution

`~/.mcli` is the documented home. `~` is resolved via `os.UserHomeDir()` (which is correct on Windows, where `~` is not a literal). This deliberate dotfile-in-home layout is chosen for predictability across platforms over the platform-specific config dirs.

## 8. Directory Layout

```text
~/.mcli/
  servers.json
  ai.json
  settings.json
  workspaces/
    default/
      workspace.json
      scratch.sql
      notes.md
      history.log
      imports/
      exports/
    consumer-lending/
      workspace.json
      funded-refresh.sql
      checking-findings.sql
      notes.md
      history.log
      imports/
      exports/
```

## 9. Global Configuration

### servers.json

Stores known database servers and connection profiles.

```json
{
  "servers": {
    "local_pg": {
      "type": "postgres",
      "environment": "dev",
      "host": "localhost",
      "port": 5432,
      "default_database": "postgres",
      "user": "mathew",
      "password_source": "keyring"
    },
    "etl_sqlserver": {
      "type": "sqlserver",
      "environment": "prod",
      "connection_string": "Server=sqlprod01;Database=ETLDB;",
      "password_source": "prompt"
    }
  }
}
```

Passwords are not stored directly in this file unless the user explicitly chooses an insecure mode. Supported password sources: `prompt`, `env:VARIABLE_NAME`, `keyring`, and database-native password files where applicable.

### ai.json

Stores AI provider configuration. AI features are optional.

```json
{
  "providers": {
    "local": {
      "base_url": "http://localhost:11434/v1",
      "model": "qwen2.5-coder:14b",
      "api_key_source": "none"
    },
    "openai": {
      "model": "gpt-5.5-thinking",
      "api_key_source": "env:OPENAI_API_KEY"
    }
  },
  "default_provider": "local",
  "send_schema_context": true,
  "send_sample_rows": false,
  "max_sample_rows": 20
}
```

### settings.json

Stores general CLI preferences.

```json
{
  "startup_workspace": "default",
  "color_prompt": true,
  "max_rows_default": 500,
  "confirm_dangerous_sql": true,
  "editor": "auto"
}
```

The `editor` key controls the `.edit` handoff (§11). `"auto"` resolves an external editor; `"builtin"` is reserved for the future internal editor. Defining the key now keeps the later swap a config change rather than a code change.

## 10. Workspace Configuration

Each workspace has a `workspace.json`, intentionally small — it stores durable working context, not every state detail.

```json
{
  "name": "consumer-lending",
  "current_server": "etl_sqlserver",
  "current_database": "ETLDB",
  "auto_connect": true,
  "import_dir": "imports",
  "export_dir": "exports"
}
```

## 11. Interaction Model

`mcli` is a hybrid of three surfaces. The inline REPL is home base; the other two are reached only when the task calls for them. The guiding rule is *don't rebuild what already exists well, do build what doesn't exist anywhere else.*

```text
                 +------------------------+
                 |          REPL          |   inline mode, default
                 |  keyboard-driven line  |
                 +-----------+------------+
                             |
            .edit / multi-line paste   |   run query / .view
                 +-----------+------------+-----------------+
                 |                                          |
     +-----------v-----------+              +---------------v---------------+
     |    External editor    |              |     Grid view / edit          |
     |  (your $EDITOR,       |              |  (alt-screen, paged,          |
     |   program suspended)  |              |   result sets / flat files)   |
     +-----------------------+              +-------------------------------+
```

### REPL (default surface)

* **Enter executes the current line.** No terminating semicolon is required. The REPL input is a single line; there is no statement-accumulation buffer.
* **Multi-line lives behind `.edit`.** Anything longer than a single line is written and edited in the editor, then run with `.run`.
* **Single-line syntax highlighting.** The current input line is highlighted live via Chroma, using the lexer for the connected database's dialect (Transact-SQL for SQL Server, generic SQL or PL/pgSQL elsewhere). The prompt is rendered by `mcli` over its own input; `textinput` styles uniformly, so highlighting is applied by rendering the highlighted string with the cursor overlaid.
* **History ring.** Up/Down arrows walk previously entered commands. `Ctrl-R` reverse search is available but not load-bearing.
* **Tab completion.** Context-aware: commands, then table names from the live connection (after `.describe`, `use`, etc.), then workspace file names (after `.edit`, `.run`, etc.).
* **Bracketed paste routing.** A paste is delivered as a distinct event. A single-line paste lands in the prompt like typing. A paste containing newlines opens `.edit` pre-filled with the pasted content instead of executing — which also prevents a multi-line paste from firing as several partial executions under the Enter-executes rule.
* **Live prompt.** The prompt returns immediately when a command completes. There is no "press any key to continue."

### Keyboard and mouse philosophy

Everything is either typed or a single unmodified key. No mouse is ever required; the mouse is supported in the grid but never necessary.

* Typed: commands and SQL, no modifiers.
* Up/Down: history. Tab: complete. Enter: execute.
* One consistent exit: `Esc` (or `q`) always returns from the grid to the REPL — the same key every time.
* The only chord that matters is `Ctrl-C`, which cancels a running query (via context cancellation) without quitting the app.

### Editor strategy

`.edit` opens *an* editor and returns control to the REPL on exit. The contract is stable regardless of which editor is behind it.

* **Now (external editor).** `.edit` performs a `tea.ExecProcess` round-trip: it suspends the whole TUI, hands the terminal fully to the child editor, and resumes the REPL when the editor exits. Editor resolution order: the `editor` setting in `settings.json`, then `$VISUAL`, then `$EDITOR`, then a platform default (`notepad` on Windows; `nano` or `vi` elsewhere). This delivers excellent multi-line editing on day one with no editor to build.
* **Later (built-in editor).** A future phase replaces the handoff with an internal alt-screen editor behind the same `.edit` entry point and the `"editor": "builtin"` setting. It adds Chroma syntax highlighting, insert/overwrite modes (with a cursor-shape cue), OSC 52 copy/paste (works over SSH), a keyboard selection model, and — the reason to build it at all — SQL-aware execution: run the selected statement against the current connection from inside the editor. Until that capability is wanted, the external editor is the right call; building a general text editor otherwise is unnecessary complexity.

### Grid view / edit (alt-screen surface)

* **Result sets and tables** render in a full-screen, paged grid (`bubbles/table` + `viewport`), with horizontal scroll for wide rows and paging tied to `max_rows_default` (§17). When inline output is truncated by the row cap, the REPL offers to open the full result in the grid.
* **Flat-file editing** is in scope: load a delimited or fixed-width file into the grid, edit cells, write the file back.
* **Live database-table editing is out of scope for the first cut** (and may stay a non-goal). Editing a live table through a grid means generating `UPDATE ... WHERE pk = ...` DML, which requires primary-key awareness and runs straight into the dangerous-SQL guardrails (an editable grid is an `UPDATE`-without-`WHERE` generator if careless). The database-native path — `.edit` an `UPDATE` and `.run` it — keeps the operation visible and logged.

### Concurrency and UI threading

Bubble Tea's `Update` is single-threaded; blocking it freezes the UI. Every query, connection, and import/export runs as a `tea.Cmd` returning a `tea.Msg` on completion, with a spinner while it is in flight. Each carries a `context.Context` whose cancel is wired to `Ctrl-C`, so a runaway statement on production is cancelled without killing the app.

### Root model / state machine

The root Bubble Tea model is a small mode state machine holding a `mode` field (`repl`, `grid`), a sub-model per surface, and a handle to the UI-agnostic core. `Update` routes messages by mode; `View` renders by mode and sets `AltScreen = true` only in `grid` mode. The external editor is **not** a mode — it is a `tea.ExecProcess` round-trip that suspends the program and returns to `repl` mode on exit. (When the built-in editor lands, it becomes an additional alt-screen mode behind the same entry point.)

## 12. Workspace Commands

```text
.workspace list
.workspace create myworkspace
.workspace rename oldname newname
.workspace delete myworkspace
.workspace status
.enter myworkspace
```

`.enter myworkspace` changes the current working context:

1. Load the workspace configuration.
2. Set it as the current workspace.
3. Start logging to that workspace history file.
4. Restore the saved current server/database.
5. If `auto_connect` is true, connect automatically.
6. Update the prompt.

Example prompt:

```text
consumer-lending:etl_sqlserver:ETLDB>
```

## 13. Server Commands

```text
.server list
.server show servername
.server add
.server edit servername
.server remove servername
.server test servername
.connect servername
```

`.connect servername` connects to a configured global server from within the current workspace. On success the workspace updates its `current_server`.

## 14. Database Commands

```text
use database_name
.list databases
.list schemas
.list tables
.list views
.describe table_name
.search-views text
.search-column column_name
```

`use database_name` changes the current database for the active connection and updates the current workspace.

## 15. SQL File Commands

SQL files are relative to the current workspace. The `.sql` extension may be omitted for convenience.

```text
.files
.edit name
.run name
.cat name
.copy oldname newname
.rename oldname newname
.delete name
```

`.edit name` opens the file via the editor handoff (§11). `.run name` executes the file against the current connection. Example: `.edit funded-refresh` then `.run funded-refresh` refers to `~/.mcli/workspaces/current-workspace/funded-refresh.sql`.

## 16. Import and Export

Import and export use explicit paths. The workspace provides default import/export folders, but the command still states the file involved.

```text
.import imports/members_june.csv into staging.members
.import imports/members.xlsx sheet "June" into staging.members
.export query funded-refresh to exports/funded_june.xlsx
.export current to exports/check_results.csv
.export table dbo.Customer to exports/customer.csv
```

Initial formats: CSV, TSV, pipe-delimited, Excel `.xlsx`, fixed-width flat files. Possible later formats: JSON, NDJSON, Parquet. Import/export profiles may be added later; the first version keeps commands simple and explicit.

## 17. Safety and Guardrails

Safety matters because `mcli` may connect to production databases. Guardrails live in the core so both the TUI and the MCP server inherit them.

* Color-coded prompt by environment (§18).
* Confirmation before dangerous SQL.
* Read-only mode option.
* Maximum default row count (`max_rows_default`).
* Current workspace/server/database always visible in the prompt.
* No plaintext passwords stored by default.
* Extra confirmation for production write operations.
* Optional blocking of dangerous commands on production.

Dangerous SQL (configurable list):

```text
DROP
TRUNCATE
ALTER
DELETE without WHERE
UPDATE without WHERE
MERGE
INSERT
CREATE INDEX
```

## 18. Prompt Color

Prompt color reflects environment risk.

```text
dev      green
test     yellow
stage    yellow
prod     red
unknown  gray
```

The text may be identical across environments; color gives a fast visual warning. Color depth is downsampled automatically per terminal, so this degrades gracefully on the legacy Windows console.

## 19. Lineage Commands

Lineage commands help users understand dependencies between tables, views, and queries.

```text
.pre-lineage view_name
.post-lineage table_name
```

* `.pre-lineage view_name`: show objects used by this view or query.
* `.post-lineage table_name`: show objects that depend on this table.

Lineage support varies by adapter.

## 20. AI Commands

AI commands are available from any workspace and are optional. They run through the same core as the rest of `mcli`.

```text
.ai ask "why is this query slow?"
.ai explain current
.ai explain funded-refresh
.ai fix current
.ai generate import for imports/members.csv
.ai lineage customer_balance
```

AI context may include the current database type, server environment, schema metadata, current SQL file, and error messages — plus optional sample rows only if configured. AI never executes SQL automatically; execution is always explicit.

## 21. MCP Integration

`mcli` exposes an MCP server so AI agents can use the same database and workspace capabilities. Because the front-ends share the core, each MCP tool is a thin wrapper over a core function, and the safety settings (§17) apply identically.

```text
.mcp serve          # from inside the TUI
mcli mcp serve      # headless, same server
```

Potential MCP tools:

```text
list_workspaces      enter_workspace      get_workspace_status
list_servers         connect_server       list_databases
list_tables          describe_table       search_columns
search_views         read_workspace_file  write_workspace_file
run_saved_sql        run_query            export_query
import_file
```

MCP tool access respects safety settings (read-only mode, dangerous-SQL rules, production guards).

## 22. Database Adapter Model

Each database type implements a common adapter interface; database-specific differences are handled inside the adapter. Sketch:

```go
type Adapter interface {
    Connect(ctx context.Context, profile ServerProfile) error
    Disconnect() error

    ListDatabases(ctx context.Context) ([]string, error)
    UseDatabase(ctx context.Context, name string) error
    ListSchemas(ctx context.Context) ([]string, error)
    ListTables(ctx context.Context) ([]ObjectRef, error)
    ListViews(ctx context.Context) ([]ObjectRef, error)
    DescribeObject(ctx context.Context, name string) (ObjectDetail, error)

    RunQuery(ctx context.Context, sql string) (RowStream, error)   // streamed rows
    RunStatement(ctx context.Context, sql string) (Result, error)  // affected rows, etc.
    ExplainQuery(ctx context.Context, sql string) (Plan, error)

    SearchColumns(ctx context.Context, name string) ([]ColumnRef, error)
    SearchViews(ctx context.Context, text string) ([]ObjectRef, error)

    GetPreLineage(ctx context.Context, name string) ([]ObjectRef, error)
    GetPostLineage(ctx context.Context, name string) ([]ObjectRef, error)

    Dialect() Dialect   // selects the Chroma lexer and quoting rules
}
```

Import/export is implemented once in `core/transfer` against `RowStream` and `RunStatement`, not per adapter, so format support is uniform across databases. Adapters register themselves into a registry keyed by `type`; CGo adapters (e.g. a DB2 build) register only when their build tag is present.

## 23. Design Principles

* **Keep the surface simple.** A workspace is just the room you are working in. Servers and AI are global utilities. SQL files, imports, exports, and history belong to the current workspace. Changing workspace changes the working context.
* **One core, two front-ends.** The TUI and the MCP server are clients of the same core. The core is the contract.
* **Don't rebuild what exists; build what doesn't.** Borrow the user's editor for multi-line text; build the grid viewer, because nothing else gives an aligned view of result sets and flat files.
* **Pure Go by default.** The common cross-platform build has no C toolchain; CGo lives behind build tags.
* **Unnecessary complexity is bad; necessary complexity is immature.** The built-in editor and live-table grid editing are deferred until the capability they unlock (in-editor execution; PK-aware DML) is actually wanted.

## 24. Implementation Plan

### Phase 1 — Core and configuration

* Create the `~/.mcli` layout with `os.UserHomeDir()` path resolution.
* Load `settings.json`, `servers.json`, `ai.json`.
* Implement the workspace manager, default workspace, and `workspace.json`.
* Implement per-workspace `history.log`.
* No UI dependencies in `internal/core`.

### Phase 2 — REPL shell (TUI)

* Bubble Tea v2 root model and mode state machine (`repl` mode).
* Single-line input with **Enter executes**.
* Chroma single-line syntax highlighting (dialect by connection).
* History ring (Up/Down) and Tab completion (commands and files first).
* Prompt context and environment color.
* Bracketed-paste routing (multi-line paste opens `.edit`).
* `.enter` and the workspace commands.

### Phase 3 — First database adapter (pure Go)

* Define the adapter interface and registry.
* Implement PostgreSQL (`pgx`) or SQL Server (`go-mssqldb`) first.
* `.connect` / `use` / `.list` / `.describe` / query execution.
* Run queries as `tea.Cmd`s with `context` cancel on `Ctrl-C`.
* `max_rows_default` guardrail and basic inline result display.

### Phase 4 — Grid surface, SQL files, external editor

* Alt-screen grid mode (`bubbles/table` + `viewport`, paging, horizontal scroll); "open full result in grid" from a truncated inline result.
* `.files`, `.edit` (external editor via `tea.ExecProcess`, resolution order per §11), `.run`, `.cat`, `.copy`, `.rename`, `.delete`.
* Log file operations.

### Phase 5 — Import / export

* CSV export, then CSV import.
* TSV and pipe-delimited.
* Excel `.xlsx`.
* Fixed-width flat files (with flat-file grid editing).

### Phase 6 — Additional adapters

* MySQL / MariaDB (`go-sql-driver/mysql`).
* Oracle (`go-ora`).
* DB2 last, behind a build tag — decide pure-Go (`obaydullahmhs/go-db2`) vs CGo (`ibmdb/go_ibm_db`) at that point.

### Phase 7 — Server management and safety hardening

* `.server add/edit/remove/test`; password sources including keyring with prompt/env fallback.
* Dangerous-SQL confirmation, read-only mode, production write guards, optional command blocking on prod.

### Phase 8 — AI assistance

* `ai.json` providers; `.ai ask`; explain/fix current SQL; schema-context support; configurable providers.

### Phase 9 — MCP server

* `mcli mcp serve` / `.mcp serve` exposing workspace, server, schema, query, import/export, and file tools over the core, with safety controls applied.

### Phase 10 — Built-in editor (deferred)

* Internal alt-screen editor behind the same `.edit` entry point and `"editor": "builtin"`: Chroma highlighting, insert/overwrite, OSC 52 copy/paste, keyboard selection, and SQL-aware execution against the current connection.

### Phase 11 — Live-table grid editing (optional / later)

* PK-aware editable grid that generates DML through the safety layer, if and when it proves worth the cost over `.edit` + `.run`.
