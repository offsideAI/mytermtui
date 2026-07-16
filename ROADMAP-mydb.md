# mydb — Roadmap

A working breakdown of [SPEC-mydb.md](SPEC-mydb.md) into **Epics → Stories → Tasks**. Epics map **one-to-one onto the spec's milestones** (§8), so "Epic 2 done" means "M2 shipped". Every epic ends with a runnable, useful binary.

| Milestone | Epic | Theme | Status |
|---|---|---|---|
| M1 | [E1](#epic-e1--skeleton-registry-sqlite-browse-m1-) | Skeleton, registry, SQLite browse | 🟢 verified on-device (2026-07-15) |
| M2 | [E2](#epic-e2--data-grid--postgresql-m2-) | Data grid + PostgreSQL | 🟢 verified on-device (2026-07-15) — one refinement open (E2.S1.T2) |
| M3 | [E3](#epic-e3--sql-runner-m3-) | SQL runner | 🟢 verified on-device (2026-07-15) |
| M4 | [E4](#epic-e4--admin--safety-m4-) | Admin + safety | ⬜ not started |
| M5 | [E5](#epic-e5--jobs-backup--restore-m5-) | Jobs: backup & restore | ⬜ not started |

> Status legend: ⬜ not started · 🟡 in progress · ✅ done · ⏸️ blocked/deferred · 🟢 verified on-device · 🚧 needs refining

**Conventions.** Stories are user-visible capabilities ("as a user I can…"); tasks are the implementation items that deliver them, named with the files/packages from the spec's architecture (§4). Every epic, story, and task carries a status from the legend above — update them as work lands, and keep the spec section references so the two documents stay in lockstep. ✅ means implemented with tests green; promote to 🟢 after exercising the flow on-device in a real terminal session.

---

## Epic E1 — Skeleton, registry, SQLite browse (M1) 🟢

> Fork the myconsole foundation into `mydb-src/`, stand up the master registry, and browse a SQLite file's schema end-to-end. *(Spec §3.2, §3.4, §4.1–§4.4 as they apply to SQLite.)*
>
> 🟢 2026-07-15: verified on-device — pty-driven session (isolated `HOME`) launched the binary, connected to a seeded SQLite connection, expanded Tables to the fixture schema with live server info, and quit cleanly through the menu.

### Story E1.S1 — Project skeleton ✅
*A `mydb` binary exists with the family's config/theme/key conventions.*
- ✅ E1.S1.T1 — New Go module `github.com/offsideai/mydb` in `mydb-src/` (repo fork-the-tree convention), pure Go, no cgo
- ✅ E1.S1.T2 — `main.go`: flags (`-config`, `-version`) → config → registry → `ui.New` → `tea.Run`
- ✅ E1.S1.T3 — `internal/config`: TOML loader with `[general]`, `[query]`, `[backup]`, `[theme]`, `[keys]` (§4.7), paths `~/.config/mydb/` + `~/.local/state/mydb/`
- ✅ E1.S1.T4 — Port generic UI machinery: `format.go` (verbatim), `theme.go` (styles re-domained to node kinds, three themes), `keys.go` (new action set, remappable), `menu.go`, `hints.go`, modal system (`Confirm`, `TypedConfirm`, `Help`, `Info`)

### Story E1.S2 — Master registry ✅
*My saved connections live in one private SQLite database.* (§4.4)
- ✅ E1.S2.T1 — `internal/registry`: open/create at `~/.local/state/mydb/registry.db`, file mode 0600
- ✅ E1.S2.T2 — Numbered-migration runner (`schema_migrations`), full spec schema: `connections` (locality, `secret_ref`), `query_history`, `jobs_history`, `settings`
- ✅ E1.S2.T3 — Connection CRUD + `last_used_at` stamping; `plain:` secret encoding behind the tagged `secret_ref` design
- ✅ E1.S2.T4 — Unit tests: file mode, idempotent migrations, CRUD round-trip (incl. password), duplicate-name rejection

### Story E1.S3 — Connection management UI ✅
*I can create, edit, and delete saved connections from the tree.* (§3.4)
- ✅ E1.S3.T1 — `connform.go`: multi-field form modal — name, engine (sqlite/postgres), **user-assigned Local/Remote section**, per-engine fields, masked password, path completion
- ✅ E1.S3.T2 — `B` new / `E` edit anywhere in a connection's subtree
- ✅ E1.S3.T3 — `X` delete with **typed confirmation** (connection name), closing any open handle
- ✅ E1.S3.T4 — Headless model tests for the form and typed-delete flows

### Story E1.S4 — SQLite driver ✅
*mydb introspects any SQLite file without ever mutating it.* (§1.1–§1.3, §4.2)
- ✅ E1.S4.T1 — `internal/dbx`: `Driver`/`Conn`/`Introspector`/`Capabilities` interfaces + `Node` tree type (stable paths, per-kind metadata)
- ✅ E1.S4.T2 — `internal/drivers/sqlite` over `modernc.org/sqlite`: relations via `sqlite_master`, columns/indexes via PRAGMAs, `ServerInfo`
- ✅ E1.S4.T3 — Safety rails: missing file errors instead of being created; row counts bounded (1M cap); views never counted while browsing
- ✅ E1.S4.T4 — Driver tests against fixture files (incl. quoted identifiers)

### Story E1.S5 — Browse tree ✅
*I see Local/Remote sections and can walk a connection down to its columns.* (§3.2)
- ✅ E1.S5.T1 — Tree state: lazy loading via commands/messages, child caches keyed by stable node paths, flatten with cursor keeping
- ✅ E1.S5.T2 — Connect-on-expand with `○ ◐ ● ✗` status glyphs and spinner; `c` connect / `ctrl+c` disconnect (drops the subtree)
- ✅ E1.S5.T3 — Per-kind row details (engine·target, row counts, column types with PK/NN, index columns)
- ✅ E1.S5.T4 — Passive right info panel (server info, column summaries, connect errors) — no I/O from rendering
- ✅ E1.S5.T5 — Filter that keeps ancestor rows as context for matches; `←` collapse/parent-jump; `ctrl+r` refresh
- ✅ E1.S5.T6 — Headless end-to-end model tests (launch → connect → columns, collapse, filter, disconnect)

---

## Epic E2 — Data grid + PostgreSQL (M2) 🟢

> Page through table data read-only, and bring the second engine online. *(Spec §1.4–§1.5, §1.7, §3.3 Info/Data tabs, §4.2–§4.3.)*
>
> 🟢 2026-07-15: milestone shipped and verified on-device — pty sessions paged a SQLite table in the Data tab and browsed a live local PostgreSQL 16 server (databases → public schema → Tables, Roles group, server info). Postgres integration tests were run against the real server, not just gated. One refinement remains open (E2.S1.T2).

### Story E2.S1 — Tabbed workspace 🚧
*The right panel becomes a focusable workspace with tabs.* (§3.3)
- ✅ E2.S1.T1 — `workspace.go`: `tab` moves focus tree ↔ workspace, `[`/`]` switch tabs, each side keeps its cursor
- 🚧 E2.S1.T2 — **Info** tab: summary + server info shipped; still missing reconstructed object DDL and long-content scrolling
- ✅ E2.S1.T3 — Hint-bar and menu updates for workspace focus

### Story E2.S2 — Read-only data grid ✅
*I can page through any table or view without risk of writes.* (§1.7, §3.3)
- ✅ E2.S2.T1 — `dbx` `ReadPage` contract + `PageReq` (offset paging now, keyset-ready shape)
- ✅ E2.S2.T2 — Stable ordering in drivers: SQLite PK → `rowid` → first column; Postgres PK falling back to `ctid`
- ✅ E2.S2.T3 — `grid.go`: pinned header, `J`/`K` paging, `h`/`l` cell cursor with viewport-following column scroll, footer `rows 11–20 of ~25 · 0.4 ms`
- ✅ E2.S2.T4 — Cell rendering rules: NULL `␀`, bytea `0x…` previews (4 KB cap), sanitized control chars, `enter` full-value popup, `y`/`Y` copy cell/row via pbcopy
- ✅ E2.S2.T5 — Row-estimate strategy (`reltuples` on pg, capped counts on SQLite) so huge tables never hang selection

### Story E2.S3 — PostgreSQL driver ✅
*I can browse a Postgres server the same way as a SQLite file.* (§1.4–§1.5, §4.2)
- ✅ E2.S3.T1 — `internal/drivers/postgres` over `jackc/pgx/v5` used natively (pgxpool, no `database/sql`); per-database sub-pools for cross-database browsing
- ✅ E2.S3.T2 — Introspection via `pg_catalog`: databases (+CONNECT-gated sizes), schemas, relations (+sizes/estimates), columns, indexes, roles
- ✅ E2.S3.T3 — Capability-shaped tree: database and schema levels appear; Roles group under the connection
- ✅ E2.S3.T4 — Registry-stored passwords wired to connect; failure surfaces on the node (`✗`) with `E` re-opening the form
- ✅ E2.S3.T5 — Integration tests gated on `MYDB_TEST_PG_DSN` (skipped when absent); executed green against a local PostgreSQL 16

### Story E2.S4 — Password reveal 🟢
*Passwords stay masked until I explicitly reveal them.* (§3.4) — 🟢 verified on-device 2026-07-15 (pty session: masked dots → `p` reveals with warning → second `p` re-masks).
- ✅ E2.S4.T1 — Connection form: `ctrl+r` on the focused password field toggles masked ↔ plain; always opens masked
- ✅ E2.S4.T2 — Info panel: `p` on a connection reveals the saved password with a status-bar warning, auto-hiding after 10 s / second `p` / connection deletion (also in the Database menu)
- ✅ E2.S4.T3 — Headless model tests for both reveal paths, stale-tick safety, delete-clearing, and the no-secret no-op

### Story E2.S5 — Scoped connections & Annex sections 🟢
*A saved connection represents ONE database; sibling databases on its server surface in their own sections.* (§3.2 — fixes the M2 tree that nested every server database under a saved connection) — 🟢 verified on-device 2026-07-15 against a live local PostgreSQL 16: the connection expanded to `public` + Roles only, siblings rendered in the Annex.
- ✅ E2.S5.T1 — Saved Postgres connections expand to their own database's schemas + the Roles group only; the databases level disappears from under connections
- ✅ E2.S5.T2 — New **Local (Annex)** / **Remote (Annex)** sections list databases discovered on connected servers (per the source connection's locality), flat, sorted, deduped across connections, excluding the source's own database and any database that already has a saved connection
- ✅ E2.S5.T3 — Annex databases browse with the source connection's credentials (schemas → tables → data grid); discovery refreshes with `ctrl+r` and clears on disconnect/delete
- ✅ E2.S5.T4 — Headless tests with a fake multi-database driver: scoped expansion, annex population/dedup/exclusion, disconnect clearing

### Story E2.S6 — Connection-string field ✅
*I can paste one connection string instead of filling six fields.* (§3.4)
- ✅ E2.S6.T1 — `URL` field at the top of the New/Edit Connection form; `enter` parses it into the individual fields (setting engine) which stay visible and editable; parse also runs on save if the field is non-empty
- ✅ E2.S6.T2 — Accepted forms: `postgres://user:pass@host:port/db`, `key=value` DSNs, plain SQLite paths, `sqlite://`/`file:` URLs; malformed input errors inline without touching the fields
- ✅ E2.S6.T3 — Unit tests for the parser + headless form tests (fill-on-enter, save-with-URL, bad-URL stays)

---

## Epic E3 — SQL runner (M3) 🟢

> Write and run SQL in the embedded vim editor, with cancellation, EXPLAIN, and persistent history. *(Spec §3.3 SQL tab, §4.3.)*
>
> 🟢 2026-07-15: milestone shipped and verified on-device — a pty session typed a query in the vim editor, ran it with `ctrl+r`, showed the results grid (`2 row(s) · 1 stmt(s)`), and the run landed in `query_history`. Driver query/cancel paths tested against SQLite and a live PostgreSQL 16 (server-side cancel < 1s).

### Story E3.S1 — Embedded editor ✅
*The myconsole vim editor lives in the SQL tab.*
- ✅ E3.S1.T1 — Ported `editor.go` (+ its tests); `sql_wire.go` replaces file I/O. Deviations: redo is `U` (`ctrl+r` runs), `:q` parks focus (the buffer persists in its session)
- ✅ E3.S1.T2 — One buffer per connection, surviving tab switches; editor above results (55/45 split); focus cycles editor → results → tree via `tab`

### Story E3.S2 — Execution & cancellation ✅
*Queries run off the update loop and can always be stopped.* (§4.3 concurrency lanes)
- ✅ E3.S2.T1 — `runSession` → `queryDoneMsg`; `ctrl+r` (any mode), `:w`, and `f5` run the whole buffer (no visual mode yet, so no selection runs — spec updated)
- ✅ E3.S2.T2 — Per-connection runner: one interactive query per connection, per-session cancel, `esc` on results cancels (pg server-side cancel / sqlite interrupt), "cancel and run" confirm
- ✅ E3.S2.T3 — Multi-statement scripts via `dbx.SplitStatements` (SQLite) / simple-protocol multi-result (pg): last result set + statement count + total affected

### Story E3.S3 — Results ✅
*I see rows, timing, and precise errors.*
- ✅ E3.S3.T1 — Results reuse the shared `gridView`; elapsed time, affected rows, `max_rows` cap with truncated marker
- ✅ E3.S3.T2 — Inline errors with line:col position, detail, and hint from `pgconn.PgError`
- ✅ E3.S3.T3 — `e`/`E` EXPLAIN via capability prefixes (`EXPLAIN QUERY PLAN` / `EXPLAIN (ANALYZE, BUFFERS)`, ANALYZE confirms first; absent on SQLite)

### Story E3.S4 — Query history ✅
*Everything I run is searchable later.* (§4.4)
- ✅ E3.S4.T1 — Every finished run (success or failure) appends to `registry.query_history`, pruned to `history_limit`
- ✅ E3.S4.T2 — `ctrl+h` history modal: fuzzy subsequence search, `ctrl+t` connection/all scope toggle, `enter` loads into the editor

### Story E3.S5 — Screenshot harness ✅
*Docs and golden frames come from the real model.*
- ✅ E3.S5.T1 — `internal/dbx/fake` in-memory multi-database driver (also powers the annex/SQL headless tests)
- ✅ E3.S5.T2 — `cmd/screenshot` drives the real Model headlessly over a temp fixture + fake driver; five scenes with light golden assertions in `go test`

---

## Epic E4 — Admin + safety (M4) ⬜

> Every UI-generated write flows through one guarded choke point; the repetitive incantations become templates. *(Spec §3.5–§3.6, §4.6.)*

### Story E4.S1 — The safety plan ⬜
*Destructive operations can't happen by accident.* (§4.6)
- ⬜ E4.S1.T1 — `dbx/plan.go`: `sqlPlan{Stmts, Danger, Summary, ConfirmToken, Tx}` built at the action layer (never SQL parsing)
- ⬜ E4.S1.T2 — Danger tiers: High → typed confirmation showing full SQL + blast radius; Medium → y/n preview (`confirm_medium`); reads free
- ⬜ E4.S1.T3 — Transaction wrapping where `TransactionalDDL`; raw editor SQL stays exempt
- ⬜ E4.S1.T4 — Per-connection read-only option greys out mutating actions

### Story E4.S2 — Common-commands menu ⬜
*CREATE USER / CREATE DATABASE / GRANT are three dialogs, not typing.* (§3.5)
- ⬜ E4.S2.T1 — `dbx/templates.go` catalog (params, danger levels) — adding a template is adding an entry
- ⬜ E4.S2.T2 — `dbx/dialect.go`: `QuoteIdent`/`QuoteLiteral` with identifier validation; masked password params
- ⬜ E4.S2.T3 — `C` menu → parameter dialogs → **SQL preview modal** → danger-appropriate confirm → execute
- ⬜ E4.S2.T4 — v1 catalog: create user, create database with owner, grant all, grant read-only on schema, change password, drop user/database

### Story E4.S3 — Roles & maintenance ⬜
*Postgres roles and routine upkeep from the tree.*
- ⬜ E4.S3.T1 — Roles view: memberships and grants in the Info tab; role nodes actionable
- ⬜ E4.S3.T2 — Maintenance ops per capability: `VACUUM`, `ANALYZE`, `REINDEX`, `PRAGMA integrity_check`, surfaced contextually (`M`)

---

## Epic E5 — Jobs: backup & restore (M5) ⬜

> Long operations move to a background queue with live progress; v1 is complete. *(Spec §1.2, §1.6, §4.5, plus docs.)*

### Story E5.S1 — Jobs queue engine ⬜
*Long work never blocks browsing.* (§4.5)
- ⬜ E5.S1.T1 — `internal/jobs`: queue adapted from the iCloud download queue — states, pause/cancel/reorder, snapshot-for-render, tick-driven UI
- ⬜ E5.S1.T2 — Concurrency: global limit 2, max 1 job per connection; no cross-restart resume (history instead)
- ⬜ E5.S1.T3 — Jobs tab + transient job bar above the status line

### Story E5.S2 — SQLite backup ⬜
- ⬜ E5.S2.T1 — `VACUUM INTO` runner with a real percentage (dest size vs source size)
- ⬜ E5.S2.T2 — Restore = open-the-copy guidance + file placement; integrity check before/after

### Story E5.S3 — Postgres backup & restore ⬜
- ⬜ E5.S3.T1 — Startup probe for `pg_dump`/`pg_restore` (record version; grey out with hint when missing)
- ⬜ E5.S3.T2 — `pg_dump -Fc -v` / `pg_restore -v` runners: bytes-written progress + stderr phase lines (no fake percentage)
- ⬜ E5.S3.T3 — Built-in plain-SQL fallback dumper with visible fidelity warning (`prefer_pg_dump = false` forces it)
- ⬜ E5.S3.T4 — Version-skew warning when the server is newer than the tool

### Story E5.S4 — History & docs ⬜
- ⬜ E5.S4.T1 — Completed/failed jobs logged to `registry.jobs_history`, visible in the Jobs tab
- ⬜ E5.S4.T2 — Docs: README install/overview section for mydb, USAGE walkthrough, DEPLOY build entry, golden screenshots via the harness
- ⬜ E5.S4.T3 — Manual acceptance checklist from spec §7 executed against a real Postgres + SQLite pair

---

*Cross-cutting, every epic:* `go test ./... && go vet ./...` green, headless model tests for new UI flows, and SPEC-mydb.md updated if implementation diverges from the design.
