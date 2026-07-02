# mcli Roadmap (post-implementation)

This document captures **intended future work that lies beyond the 11-phase plan**
in [`mcli-design.md`](mcli-design.md). Nothing here is scheduled or committed — it
is a durable record of direction so ideas aren't lost between sessions. The current
build is feature-complete and usable (Phases 1–10 + the SQL linter; see
[`../PLAN.md`](../PLAN.md)).

The guiding constraint for all of it: keep the load-bearing idea intact — **one
UI-agnostic `internal/core`, thin front-ends on top.** New capabilities land in the
core (and its safety layer) so every front-end inherits them; new front-ends stay
thin.

---

## Feature backlog

### Lineage (surface an existing core capability)

The adapter interface already implements `GetPreLineage` / `GetPostLineage`, and the
design (§ exploration) describes `.pre-lineage <view>` / `.post-lineage <table>`.
These are currently reachable only through the core (and could be exposed as MCP
tools); they are **not yet wired as TUI commands**. Work:

- Add `.pre-lineage` / `.post-lineage` REPL commands with formatted output.
- Add matching MCP tools (`pre_lineage` / `post_lineage`).
- Same gap exists for column/view search: `SearchColumns` / `SearchViews` exist in
  the core and as MCP tools, but there are no `.search-columns` / `.search-views`
  TUI commands yet.

Low risk — the core logic exists; this is front-end wiring plus output formatting.

### Analysis features (to be scoped)

A category for richer exploration/profiling, exact surface TBD. Candidate ideas
(not commitments — each needs design and a per-adapter check, since some require
dialect-specific SQL):

- Table/column profiling: row count, null %, distinct count, min/max/typical values.
- Quick plan/`EXPLAIN` summarization (the adapter already has `ExplainQuery`; the
  linter's live path uses it).
- Lightweight data-quality checks (duplicates, orphaned FKs) — overlaps with lint.

These belong in `internal/core` (e.g. an `analysis` package) so the TUI and MCP both
get them, with the safety/read-only guards applying.

### Additional import format (TBD)

One more `internal/core/transfer` input format beyond the current CSV / TSV /
pipe-delimited / `.xlsx` / fixed-width. Candidates to choose from:

- JSON / JSON-Lines (pure-Go, easy, good fit).
- Database-to-database copy (read via one adapter's `RowStream`, write via another).
- Parquet — powerful but most ecosystems pull CGo; would need a pure-Go reader to
  respect the no-CGo default, or live behind a build tag like DB2.

Implemented once against `RowStream` / `RunStatement`, never per adapter (per §
transfer).

### Already-deferred

- **Phase 11 — live-table grid editing.** PK-aware DML generated through the safety
  layer. Deferred until it's clearly worth it over `.edit` + `.run`.

---

## GUI frontend (architecture note)

> **Decided (2026-06-30).** The GUI is now a scheduled extension, not just a backlog
> note. Direction chosen: a **native Go toolkit (Fyne recommended), bound directly to
> `core`** and shipped as a **separate `-tags gui` build artifact**; an **assist
> channel + live-session MCP transport** lets an AI guide the user in either the GUI
> or the CLI. See `mcli-design.md` §25–§28 and PLAN.md Phases 12–15. The trade-off
> analysis below stands as the rationale; "Durable options" is resolved in favor of a
> direct-binding native GUI (option 2's spirit, native rather than Wails/webview),
> with MCP kept for the AI-assist channel only — not as the GUI's data pipe.

A likely larger future direction: a graphical front-end. The original thought was to
have it drive the CLI **through MCP** under the hood. Feasibility is high; the design
already anticipates extra front-ends. The notes below capture the trade-offs so the
transport decision is made deliberately.

### Bottom line

Very feasible and low-risk. `internal/core` is already a complete, UI-agnostic API
with all domain logic, safety guards, workspaces, and adapters behind it, and there
are already two thin front-ends (TUI + MCP) proving the pattern. A GUI is just a
third client. The real decision is **not "can MCP do it"** (it roughly can) but
**"is the GUI a third front-end on `core`, or a client of the agent-shaped MCP
API."**

### MCP-under-the-hood: great on-ramp, awkward as the permanent seam

MCP exists today, so a GUI could spawn `mcli mcp serve`, speak JSON-RPC to it, and
have a working schema browser + query runner quickly — an excellent way to validate
the UX. But MCP is shaped for **LLM agents**, and a few GUI needs strain that shape:

- **Large result sets.** `run_query` returns a single capped JSON blob — fine for an
  agent, wrong for a data grid that wants virtual scroll / pagination / streaming.
  MCP's request/response tools don't naturally provide a cursor or stream.
- **Confirmation flow.** Dangerous SQL is refused unless `confirm:true`, signaled via
  an *error string*. A GUI would have to pattern-match that string to know when to
  show a confirm dialog, then re-call. Brittle; a GUI wants a structured
  `requires_confirmation` response.
- **Live events.** Connection dropped, query progress, cancellation — a GUI wants
  push. MCP has notifications, but the current server is minimal/request-response.
- **Secrets map fine**, though: the GUI collects a password in a dialog and passes it
  to `connect_server`, exactly as the MCP model already expects.

Pushing GUI needs into MCP would mean adding GUI-shaped tools that muddy its
"for agents" purpose.

### Durable options

1. **`internal/api` — a small local HTTP/WebSocket front-end over `core`**, designed
   for GUIs (pagination, streaming, push events, structured confirmation). A sibling
   to `internal/tui` and `internal/mcp`; keeps the GUI language-agnostic (any
   web/Tauri/Electron frontend) and preserves process isolation. **Cleanest
   long-term answer.**
2. **Direct binding.** If the GUI is Go-based — **Wails** (Go backend + web frontend)
   is the natural fit — it can call `core` directly, with full streaming `RowStream`
   access and zero RPC translation. Most powerful; ties the GUI to Go.
3. **MCP subprocess.** Quickest to stand up; agent-shaped (see frictions above).

**Recommendation:** prototype on MCP to validate the UX, then graduate to
`internal/api` or Wails-direct. Keep MCP alive in parallel for the agent use case it
is actually good at.

### Trade-off to decide early

mcli's identity is *one self-contained, pure-Go, no-CGo, cross-compiles-everywhere*
binary. Every GUI route compromises that to some degree — webviews bring an OS
webview dependency and usually some CGo (Wails, Tauri); Electron bundles a runtime.
Not a blocker, but it likely means a **separate GUI build artifact** while the
CLI/MCP binary stays pure-Go. Decide consciously that the GUI is a different
distribution target, not a flag on the existing one.

### Suggested first slice

A read-only **schema browser + paged query grid**. It exercises the
streaming/pagination question immediately — and that question is what actually
decides the transport.
