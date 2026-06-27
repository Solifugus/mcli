# mcli

A multi-database command-line workbench for SQL development, data exploration,
import/export, and AI-assisted database work. One keyboard-first TUI, one
self-contained binary, many databases.

The central idea is the **workspace**: a named, task-oriented working context.
Enter a workspace and it restores your current server, current database, SQL
files, import/export folders, and history log — so you can switch between
unrelated tasks without losing your place.

> **Status:** feature-complete and usable. The interactive TUI, all default
> database adapters, import/export, AI assistance, the MCP server, the built-in
> SQL editor, and the SQL linter are all implemented. The only deferred item is
> optional live-table grid editing. See [`PLAN.md`](PLAN.md) for phase-by-phase
> progress and [`docs/mcli-design.md`](docs/mcli-design.md) for the full design.

## Highlights

- **Multiple databases, one interface.** PostgreSQL, MySQL/MariaDB, SQL Server,
  and Oracle work out of the box; DB2 is available behind a build tag. Every
  database implements one common adapter interface, so behavior is uniform.
- **Pure-Go by default.** The standard build needs no C toolchain and
  cross-compiles cleanly to Linux, macOS, and Windows. CGo-only drivers stay
  quarantined behind build tags.
- **Workspaces.** Per-task context: current server/database, SQL files,
  import/export folders, and a history log, all restored on entry.
- **Keyboard-first REPL.** Single-line input where **Enter executes**; a
  scrollable alt-screen grid for result sets; multi-line editing in your
  `$EDITOR` or the optional built-in SQL editor.
- **Built-in SQL editor** (`"editor": "builtin"`). An alt-screen editor with
  syntax highlighting, insert/overwrite, keyboard selection, OSC 52 copy — and
  the point of it: run the statement under the cursor against the live
  connection (through the same safety guard) without leaving the editor.
- **Import/export.** CSV, TSV, pipe-delimited, Excel `.xlsx`, and fixed-width
  flat files — implemented once against a streaming row interface, so every
  database gets the same formats.
- **Safety guardrails.** Dangerous-SQL detection, read-only mode, and
  production write guards live in the core, so the TUI and the MCP server
  inherit identical protection.
- **SQL linter.** `.lint` checks statements for safety, syntax, and style
  without running them; with a connection it also validates queries against the
  live schema. Available in the TUI and as an MCP tool.
- **AI assistance.** Ask questions, explain or fix SQL via any
  OpenAI-compatible endpoint (hosted or local). The AI never auto-executes SQL.
- **MCP server.** Expose the same workspace, schema, query, and import/export
  capabilities to AI agents over stdio — under the same safety controls.

## Install

Requires Go 1.25 or newer. With `GOTOOLCHAIN=auto`, the toolchain is fetched
automatically; no system install or `sudo` is needed.

```sh
go build -o mcli ./cmd/mcli
```

Cross-compile (pure Go, no CGo):

```sh
GOOS=windows GOARCH=amd64 go build -o mcli.exe ./cmd/mcli
GOOS=darwin  GOARCH=arm64 go build -o mcli      ./cmd/mcli
```

The optional DB2 adapter is built with a tag:

```sh
go build -tags db2 -o mcli ./cmd/mcli
```

## Quick start

```sh
mcli                 # launch the interactive TUI (default)
mcli mcp serve       # run the headless stdio MCP server
mcli help            # usage
```

Inside the TUI, commands are dot-prefixed (e.g. `.connect`); bare input is SQL
run against the current connection. A typical first session:

```text
.server add               # register a server (interactive wizard)
.connect myserver         # connect
use mydb                  # switch database
.list tables              # explore
select * from customers   # run SQL — Enter executes
.grid                     # open the last result in a scrollable grid
.edit report              # edit a SQL file in $EDITOR
.run report               # run it
.export query report to out.csv   # export results
```

## Commands

| Command | What it does |
| --- | --- |
| `.workspace` / `.enter <name>` | Manage and switch workspaces |
| `.server add/edit/remove/test` | Manage server definitions and passwords |
| `.connect <name>` / `.disconnect` | Open or close a connection |
| `use <db>` | Switch the current database |
| `.list databases\|schemas\|tables\|views` | Browse schema objects |
| `.describe <table>` | Show columns, types, and keys |
| `<sql>` | Run SQL on the connection (Enter executes) |
| `.grid` | Open the last result in a scrollable grid |
| `.files` / `.edit` / `.run` / `.cat` / `.copy` / `.rename` / `.delete` | Workspace SQL files |
| `.lint <file\|current> [live]` | Check SQL for safety, syntax, and style issues |
| `.import` / `.export` | Load and save delimited / Excel / fixed-width files |
| `.readonly [on\|off]` | Toggle the session read-only guard |
| `.ai ask\|explain\|fix\|providers` | AI assistance |
| `.mcp serve` | Run the MCP server on this terminal's stdio |
| `.clear` | Clear the screen |
| `.help` | Full command reference |

`Ctrl-C` cancels a running query (it does not quit the app); `Ctrl-D` quits.

## Safety

Because `mcli` may connect to production, the safety layer lives in the core and
applies to both front-ends:

- **Dangerous-SQL detection** flags statements like `DROP`, `TRUNCATE`, and
  `DELETE`/`UPDATE` without a `WHERE`, and asks for confirmation.
- **Read-only mode** (`.readonly on`) refuses any write.
- **Production guards** can require confirmation for, or outright block,
  dangerous statements on servers marked as production.
- Over MCP — where there is no human to prompt — a flagged statement is refused
  unless the caller explicitly passes `confirm: true`.

Passwords are never stored in plaintext by default. A server records only its
password *source*: `prompt`, `env:VAR`, or the OS keyring (with `prompt`/`env:`
always available as fallbacks on headless systems).

## Linting

`.lint <file|current> [live]` checks SQL without running it. The static pass needs
no connection and covers three areas:

- **Safety & correctness** — `DELETE`/`UPDATE` without a `WHERE`, `DROP`,
  `TRUNCATE`, and a `JOIN` with no `ON`/`USING` (an accidental cross join).
- **Syntax** — unbalanced parentheses, unterminated strings/comments, and
  unrecognized leading keywords.
- **Style** — `SELECT *`, trailing whitespace, tab indentation, and (optionally)
  keyword casing. Configurable under `lint` in `settings.json`.

Add `live` to also validate each query against the connected database — it asks
the engine to `EXPLAIN` the statement, catching deep syntax errors and unknown
tables or columns, dialect-correctly and without executing anything. The same
checks are available to AI agents through the `lint_sql` MCP tool.

## AI assistance

`mcli` talks to any OpenAI-compatible `/chat/completions` endpoint — hosted
(OpenAI, Anthropic's compat endpoint) or local (e.g. Ollama). Configure
providers in `ai.json`; reference API keys by environment variable, never by
storing the secret on disk. The model can be grounded with the current dialect,
environment, and schema, but it only ever *suggests* SQL — you review and run it.

## MCP server

`mcli mcp serve` (or `.mcp serve` from the TUI) runs a stdio
[Model Context Protocol](https://modelcontextprotocol.io) server. Each tool is a
thin wrapper over a core function — listing workspaces and servers, browsing and
searching schema, reading/writing workspace SQL files, running queries, and
importing/exporting — all under the same safety controls as the TUI.

## Architecture

One UI-agnostic core, two front-ends. The TUI and the MCP server are both thin
clients of `internal/core`, which owns all domain logic — workspaces, the server
registry, query execution, import/export, history, the SQL linter, and the safety
guardrails — so both front-ends inherit identical behavior.

```
cmd/mcli/            entry point; selects TUI vs `mcp serve`
internal/core/       UI-agnostic domain (workspace, server, adapter, query,
                     transfer, history, safety, lint, config)
internal/adapters/   one package per database (postgres, mysql, mssql, oracle,
                     db2 [tagged])
internal/tui/        Bubble Tea v2 front-end
internal/mcp/        stdio MCP server
internal/ai/         AI provider clients
```

Configuration lives under `~/.mcli` (`settings.json`, `servers.json`,
`ai.json`, plus a directory per workspace).

## Development

```sh
go build ./cmd/mcli                       # build the TUI
go test ./...                             # all tests
go test ./internal/core/...               # one package tree
go test -run TestName ./internal/mcp      # a single test
go build -tags db2 ./...                  # include the DB2 adapter
```

## License

Released under the [GNU General Public License v2](LICENSE).
