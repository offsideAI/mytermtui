# mydb — Specification

> **Note:** mydb is the third sibling in this repo, following myterm and myconsole. It reuses the myconsole user experience — the Elm-architecture Bubble Tea model, the tree-plus-right-panel layout, the modal system, the embedded vim-style editor, the keymap/config/theme machinery — but points it at databases instead of the filesystem.

**mydb** is a keyboard-driven terminal UI for browsing and administering local and remote databases. It connects to SQLite files and PostgreSQL servers, shows their schemas and data as a navigable tree, runs SQL from an embedded vim-style editor, manages users/roles/permissions, performs maintenance (VACUUM, ANALYZE, REINDEX, integrity checks), and backs up / restores databases with a live job queue — the things you'd otherwise juggle across `sqlite3`, `psql`, and `pg_dump`, minus the context switching.

- **Language/stack:** Go 1.22+, [Bubble Tea](https://github.com/charmbracelet/bubbletea) + lipgloss. Pure Go — no cgo.
- **Target platform:** macOS first-class, but fully cross-platform (Linux/BSD/Windows-terminal). Nothing in mydb requires macOS.
- **Binary name:** `mydb`, source in `mydb-src/` (independent Go module `github.com/offsideai/mydb`, per repo convention).

---

## 1. Background: what administering these engines actually requires (verified findings)

The design rests on how the two v1 engines really behave; future engines slot in behind the same capability flags.

1. **SQLite is a file, not a server.** A "connection" is a path. There is exactly one database per file, no schemas (beyond `main`/`temp`/attached), and no users, roles, or GRANTs. Introspection is `sqlite_master` plus PRAGMAs (`table_info`, `index_list`, `index_info`, `foreign_key_list`). Logical size = file size; row counts need `COUNT(*)` (cheap for most local DBs, capped/estimated for huge ones).
2. **SQLite backup is `VACUUM INTO 'dest'`** (3.27+): an online, consistent, defragmented copy — strictly better than copying the file (which can tear a hot WAL database). Integrity check is `PRAGMA integrity_check` (or `quick_check`). Maintenance is `VACUUM`, `ANALYZE`, `REINDEX`.
3. **SQLite queries cancel via interrupt.** `modernc.org/sqlite` honors `context.Context` cancellation (sqlite3_interrupt semantics), so a runaway query is stoppable from the UI.
4. **PostgreSQL is a server with real structure.** One server → many databases → many schemas → tables/views/indexes, plus cluster-wide roles. Introspection is `pg_catalog` (`pg_class`, `pg_namespace`, `pg_attribute`, `pg_index`, `pg_roles`, `pg_database`); `pg_table_size`/`pg_total_relation_size` give sizes and `pg_class.reltuples` gives free row estimates. A connection is bound to one database — browsing a different database on the same server needs a second connection under the hood.
5. **PostgreSQL supports true server-side query cancellation** (a cancel request on a separate channel). `jackc/pgx` exposes this through context cancellation; `database/sql` obscures it along with multi-statement scripts and rich error positions (`pgconn.PgError` carries position/hint/detail for inline display). **Drivers therefore use their native libraries directly, not `database/sql`** — the shared abstraction is mydb's own interface (§4.2).
6. **Faithful Postgres backups require `pg_dump`.** Custom-format archives, large objects, ACLs, and version-skew handling are not reproducible with plain queries. But `pg_dump` may be missing from PATH — so mydb wraps `pg_dump`/`pg_restore` when available and falls back to a **built-in plain-SQL dumper** (schema + data via queries) with an explicit fidelity warning. `pg_dump` emits no percentage; progress is proxied by bytes written plus its `-v` phase lines on stderr.
7. **Paging a huge table needs a stable order.** `LIMIT … OFFSET …` without ORDER BY returns nondeterministic pages. The driver picks a stable key: SQLite `rowid` (or the PK for `WITHOUT ROWID` tables); Postgres the primary key, falling back to `ctid`. Offset paging is O(offset) but fine for an admin tool reading hundreds of rows at a time; the page API is shaped so keyset paging can be added later without interface changes.
8. **Reading data must never mutate it.** Unlike the file browsers' "no accidental download" rail, the hazard here is accidental writes: every browsing query mydb generates is a plain `SELECT`/PRAGMA/catalog read. All UI-generated writes flow through one choke point (§3.6) — there is no code path where navigation executes DML/DDL.

## 2. Goals and non-goals

### Goals

- Browse **saved connections** — grouped into **Local** and **Remote** sections (a user-assigned flag per connection, never guessed) — down to databases, schemas, tables, views, indexes, columns, and roles, with lazy loading and live status.
- A **master registry**: one SQLite database owned by mydb that stores every connection (and its password), query history, and job history.
- **Read-only data grid**: paged, both-axes scrolling, pinned header, NULL/bytea-safe rendering.
- **SQL runner**: the myconsole vim-style editor writing SQL; run buffer or selection; cancel mid-flight; timing, affected-rows, EXPLAIN; persistent fuzzy-searchable history.
- **Administration**: Postgres roles and GRANTs, maintenance ops (VACUUM/ANALYZE/REINDEX/integrity check), object DDL views.
- **Common-commands menu**: templated frequent commands (e.g. Postgres `CREATE USER … WITH PASSWORD '…'`, `CREATE DATABASE … OWNER …`, `GRANT ALL PRIVILEGES ON DATABASE … TO …`) filled via dialogs, previewed as SQL, then confirmed.
- **Backup & restore** with a background job queue (live progress, cancel), SQLite via `VACUUM INTO`, Postgres via `pg_dump`/`pg_restore` with the plain-SQL fallback.
- **Safety**: UI-driven destructive operations always confirm (typed confirmation for the worst); raw SQL the user types runs as-is.
- **Pluggable drivers**: SQLite + PostgreSQL in v1 behind an engine-agnostic interface designed for MySQL/DuckDB/etc. later.
- Same look, feel, keys, menus, themes, and config format as myterm/myconsole.

### Non-goals (v1)

- Editing data from the grid (inline cell/row editing) — writes go through the SQL editor in v1.
- Engines beyond SQLite and PostgreSQL (the driver interface anticipates them).
- Keychain/OS-keyring secret storage — passwords live in the registry (§4.4); the schema reserves room to add keyring/prompt/pgpass modes later.
- Per-statement result tabs for multi-statement scripts (v1 shows the last result set + statement count + total affected).
- Resuming jobs across restarts (finished/failed jobs are logged; a killed `pg_dump` can only be restarted, not resumed).
- SSH tunnels, ER diagrams, schema diff/migration tooling, CSV import.

## 3. UX design

### 3.1 Screen layout

myconsole's chrome, re-domained: tree on the left, a **tabbed workspace** on the right (replacing the contents panel), same menu/hint/status furniture.

```
┌ File  Edit  View  Go  Database  Commands  Help ──────────────────────────────┐
│ prod-pg ▸ appdb ▸ public ▸ users                                   postgres ●│ ← breadcrumb
│ ▸ Local                          │ ┌ Info ┊ Data ┊ SQL ┊ Jobs ┐              │
│   ▾ app.db        sqlite  ●      │ │ id │ email          │ created_at      │ │
│     ▾ Tables (12)                │ │ 501│ ada@example.com│ 2026-03-01 09:12│ │
│       ▸ orders    ~8.4k  1.2 MB  │ │ 502│ ␀              │ 2026-03-01 09:15│ │
│       ▸ users     ~1.2k   84 KB  │ │ …                                     │ │
│   ▸ Local (Annex)  3             │ │                                       │ │
│   ▾ Remote                       │ │                                       │ │
│   ▾ prod-pg       postgres ●     │ │                                       │ │
│     ▾ public                     │ │                                       │ │
│       ▸ Tables (34)              │ │                                       │ │
│       ▸ Views (6)                │ │                                       │ │
│     ▸ Roles (5)                  │ │                                       │ │
│   ▸ Remote (Annex) 7             │ └ rows 501–1000 of ~12,340 · 8 ms ──────┘ │
│ ⣷ backup appdb → appdb.dump   14 MB written · dumping table public.orders    │ ← job bar
│ ┌ ^R run ┊ e explain ┊ ^H history ┊ d backup ┊ B new conn ┊ ? help ┐         │ ← hint bar
│ 2 running · sort name · appdb: 34 tables                                     │ ← status bar
└───────────────────────────────────────────────────────────────────────────────┘
```

- **Menu bar** (`m`/`F10`): File, Edit, View, Go, **Database** (connect, disconnect, backup, restore, maintenance, roles), **Commands** (the template catalog, §3.5), Help. Every item shows its live shortcut.
- **Breadcrumb** — the selected node's path (`connection ▸ database ▸ schema ▸ object`) plus engine and connection-status glyph.
- **Tree** (left) and **workspace** (right) — split by `split_ratio` (default **0.50**), resized with `<`/`>`, toggled with `F3`/`P`; `tab`, `ctrl+w`, or `ctrl+o` moves focus between them, each side keeping its cursor. The tree's Details column sizes to its visible content, so a connection row's full `engine · host:port/db` target renders untruncated (no wrapping) whenever the pane can fit it.
- **Job bar** — appears only while the job queue is active (mirrors the download bar).
- **Hint bar** (`H` toggles) and **status bar** — as in myconsole; the status bar's right side always carries the connection indicator (green `●` + name / red `●` disconnected) beside running-query count and the selected scope's object counts.

### 3.2 The tree

Top level is four fixed sections. **Local** and **Remote** hold the saved connections (the registry's user-assigned `locality` flag); each is followed by an **Annex** section holding databases *discovered* on connected servers of that locality:

```
▸ Local                            saved connections (locality = local)
  ▸ app.db        sqlite   ●       connection — engine, status glyph, path/host
    ▸ Tables (12)                  group
      ▸ users     ~1.2k  84 KB     table — est. rows, size
        ▸ Columns (6)              group → column leaves: name, type, PK/NOT NULL marks
        ▸ Indexes (2)              group → index leaves
    ▸ Views (2)
▸ Local (Annex)                    databases discovered on connected local servers
  ▸ blueberry_demo   postgres · 8.2 MB
  ▸ convoy_app       postgres · 1.0 GB
▸ Remote
  ▸ prod-pg       postgres ● host:5432/appdb
    ▸ public                       schema of the connection's OWN database
      ▸ Tables (34) / Views (6) / Indexes
▸ Remote (Annex)                   same, for connected remote servers
▸ Roles                            cluster roles, grouped per connected server
  ▸ prod-pg       host:5432
    admin         login · super
    app_reader    login
```

- **A saved connection is one database.** Expanding a Postgres connection shows *its configured database's* schemas — never the server's other databases. That keeps the mental model honest: `prod-pg` *is* `appdb`, not the whole server.
- **Roles are a top-level view.** Roles belong to the cluster, not to any one connection string, so they never nest under a connection. The **Roles** section lists one entry per connected server (deduplicated by host:port, **labeled by the server's host** — e.g. `localhost` — never by a connection-string name; engines with the `Roles` capability only); expanding it loads the server's role list. Entries clear on disconnect/delete and refresh with `ctrl+r`.
- **One active connection.** Connecting (`c`, or expanding a closed connection) first disconnects the previously connected database, releasing its server resources; its Annex and Roles entries and caches go with it (SQL buffers persist in their sessions). `d` disconnects explicitly. The status bar always carries a **prominent indicator**: green `●` plus the connection name while connected, red `●` `disconnected` otherwise.
- **Annex sections** — connecting to a server with multiple databases (the `MultipleDatabases` capability) also enumerates its other databases into the matching Annex section: flat, sorted, deduplicated across connections to the same server, and excluding any database that already has its own saved connection. Annex databases browse with the source connection's credentials (schemas → tables → data grid), refresh with `ctrl+r`, and disappear when their source connection disconnects or is deleted. Promoting an annex database to a saved connection is just `B` with the same server details.
- **Capability-shaped levels**: the tree builder consults the driver's capability flags (§4.2) — SQLite connections skip the schema/roles levels entirely and contribute nothing to the Annex. Future engines describe themselves the same way.
- **Lazy loading**: expanding a node fires a command; children arrive as a message (generation-tagged so stale loads are dropped). Expanding a closed connection connects first (status `○`→`◐`→`●`, or `✗` with the error on the node).
- **Right-hand columns switch on node kind**: connection → engine + status + target; table/view → `~rows` + human size; column → type + `PK`/`NN`; role → `login`/`super` marks; index → backing columns.
- Sorting (`s`), filtering (`f`, live at every expanded level), and fuzzy find (`F`, across all loaded nodes and lazily fetched names) behave exactly as in myconsole.

**Connection status glyphs**

| Glyph | State |
|:---:|---|
| `○` (dim) | Saved, not connected |
| `◐` (yellow) | Connecting |
| `●` (green) | Connected |
| `✗` (red) | Connection failed (error shown on the node and in the status bar) |
| `⏸` (blue) | Read-only option set on the connection |

### 3.3 The workspace (right panel)

Focusable (`tab`), tabbed (`[`/`]` switch tabs), context follows the selected tree node:

- **Info** — scrollable summary: object DDL (`CREATE TABLE …` reconstructed or fetched), column/index/FK details, size and row estimates; for connections, server version and settings; for roles, memberships and grants.
- **Data** (tables/views) — the read-only grid. Pages of `page_size` rows (default 500) with a **mandatory stable order** (§1.7); `J`/`K` (or scrolling past the edge) page forward/back; `h`/`l` scroll columns horizontally with the header row pinned; `g`/`G` first/last loaded row. Cells render NULLs as a dim `␀`, byte arrays as `0x…` previews, control characters sanitized, wide cells truncated with `…` (full value in a popup on `enter`). Footer: `rows 501–1000 of ~12,340 · 8 ms`.
- **SQL** — the myconsole vim editor (modes, motions, counts, undo, search; redo is `U` since `ctrl+r` runs, and `:q` parks focus on the tree — the buffer persists in its session) on top, a results grid below. One buffer per connection, surviving tab switches.
  - `ctrl+r` (any mode), `:w`, or `f5` runs the **whole buffer** against the node's connection. (This vim subset has no visual mode; run-selection arrives with it.)
  - `esc` on a running query offers cancellation (server-side cancel on Postgres, interrupt on SQLite); starting a new query while one runs on the same connection offers "cancel and run".
  - Results show columns/rows (same grid), elapsed time, statements executed, and total rows affected; errors render inline with position/hint when the engine provides them. Result sets are capped at `max_rows` (default 10,000) with a `truncated` marker.
  - `e` wraps the buffer/selection in the engine's EXPLAIN form (`EXPLAIN QUERY PLAN` / `EXPLAIN (ANALYZE, BUFFERS)` — ANALYZE variant confirms first, since it executes).
  - `ctrl+h` opens query history (fuzzy-searchable, scoped to the connection with a toggle for all); `enter` loads the entry into the editor.
- **Jobs** — the queue manager (§3.7 keys; queue semantics in §4.5).

### 3.4 Connections and the registry sections

- `B` (or *File → New Connection*) opens a **form modal**: an optional **URL field**, then name, engine, **locality (Local/Remote — the user chooses; mydb never guesses)**, then per-engine fields — SQLite: path (with tab completion); Postgres: host, port, database, user, password (masked); plus an **Access** choice (read-write / read-only) for both engines. Saving writes to the registry and the connection appears in its section.
- **Connection-string field** — paste a whole connection string into the URL field and press `enter`: `postgres://user:pass@host:5432/db`, a `key=value` DSN (`host=… port=… dbname=…`), a plain SQLite path, or a `sqlite://`/`file:` URL. mydb parses it into the individual fields (setting the engine) which stay visible and editable before saving; a malformed string errors inline without touching the fields. The parse also runs on save when the URL field is non-empty. The URL itself is not stored — the parts are.
- **Password reveal** — passwords are masked by default everywhere. In the form, `ctrl+r` on the focused password field toggles it to plain text (for typing or verifying) and back; the form always opens masked. On a connection node, `p` reveals the saved password in the Info panel with a status-bar warning; it re-masks automatically after 10 seconds, on a second `p`, or when the connection is deleted. Nothing else ever displays a stored password.
- `E` edits, `X` deletes (typed confirmation — deleting a saved connection also drops its history rows), on connection nodes.
- The registry itself is a first-class citizen: *Database → Open Registry* opens mydb's own registry database as a SQLite connection for power users.

### 3.5 Common-commands menu

The **Commands** menu (`C`) is a catalog of templated, engine-aware commands — the repetitive incantations, dialog-driven:

1. Pick a template (filtered to the selected connection's engine and capabilities).
2. Fill each parameter in sequence via input dialogs — identifiers are validated and quoted through the engine dialect; passwords are masked and literal-quoted.
3. A **preview modal shows the exact SQL** that will run.
4. The danger-appropriate confirmation (§3.6), then execution with the result in the status bar.

v1 catalog (Postgres unless noted):

| Template | SQL shape |
|---|---|
| Create user | `CREATE USER <user> WITH PASSWORD '<password>';` |
| Create database with owner | `CREATE DATABASE <db> OWNER <user>;` |
| Grant all on database | `GRANT ALL PRIVILEGES ON DATABASE <db> TO <user>;` |
| Grant read-only on schema | `GRANT USAGE ON SCHEMA <s> TO <u>; GRANT SELECT ON ALL TABLES IN SCHEMA <s> TO <u>; ALTER DEFAULT PRIVILEGES …` |
| Change password | `ALTER USER <user> WITH PASSWORD '<password>';` |
| Drop user / database | `DROP USER <user>;` / `DROP DATABASE <db>;` |
| Maintenance (both engines) | `VACUUM` / `ANALYZE` / `REINDEX` / `PRAGMA integrity_check` per capability |

The catalog lives in one data file (§4.1 `dbx/templates.go`), so adding a template is adding an entry, not UI code.

### 3.6 Safety model

- **Reads are free.** Navigation, grids, Info tabs, EXPLAIN (non-ANALYZE) never confirm.
- **Every UI-generated write is a plan** — the action layer (never SQL parsing) builds `{statements, danger, summary, transactional}`:
  - **High** (DROP anything, TRUNCATE, role deletion): a **typed confirmation** — the dialog shows the full SQL, the target, and the blast radius when cheaply known (`≈1.2k rows`), and requires typing the object's name.
  - **Medium** (CREATE/ALTER/GRANT/REVOKE, maintenance that rewrites data): a yes/no confirm previewing the SQL.
  - Multi-statement plans run in a transaction where the engine supports transactional DDL.
- **Raw SQL typed in the editor runs as-is** — the editor is the expert path; feedback is the affected-row count, not a gate.
- Connections can be marked **read-only** via the form's Access field; the driver then opens read-only (SQLite `mode=ro`) or sets the session read-only (Postgres), the tree shows the `⏸` glyph, and every UI-generated plan is refused before its confirmation with a status-bar note. Raw SQL in the editor still reaches the read-only session (the engine rejects writes).

### 3.7 Keybindings

Same dual philosophy as the siblings (arrow keys + vim letters, everything rebindable via `[keys]`), inherited navigation, new database domain keys. Press `?` for the live version.

| Group | Keys |
|---|---|
| **Move** | `↑↓`/`kj` cursor · `enter`/`→` expand (connects if needed), `←` collapse / parent row · `g`/`G` top/bottom · `pgup`/`pgdn` page · `bksp` parent |
| **Panels** | `tab`/`ctrl+w`/`ctrl+o` tree ↔ workspace (the ctrl combos work in the editor's insert mode too; `ctrl+o` is the fallback where a terminal/tmux eats `ctrl+w`) · `[` `]` workspace tabs · `<`/`>` resize · `F3`/`P` toggle panel |
| **Find** | `f` filter · `F` fuzzy find · `s` sort · `:` go to node path · `ctrl+r` refresh node |
| **Connections** | `B` new · `E` edit · `X` delete · `c` connect (disconnects the previous) · `d` disconnect · `p` reveal password (10s) · `ctrl+r` reveal in form |
| **SQL tab** | `ctrl+r`/`:w` run · `esc` cancel running · `e` explain · `ctrl+h` history · vim keys per the editor |
| **Data tab** | `J`/`K` next/prev page · `h`/`l` column scroll · `enter` full cell value · `y` copy cell · `Y` copy row |
| **Admin** | `C` commands menu · `M` maintenance · `I` info · backup/restore land in M5 |
| **Jobs** | `Q` jobs tab · `c` cancel item · `C` cancel all · `p` pause · `K`/`J` reorder · `x` clear finished |
| **App** | `m`/`F10` menus · `?`/`F1` help · `H` hint bar · `ctrl+q` quit |

### 3.8 Color & theming

The three themes (`default`, `dracula`, `solarized`) carry over unchanged; the per-kind row coloring re-maps from file classes to node kinds — connected-connection green, table cyan, view teal, index/column dim, role magenta, destructive menu items and High-danger dialogs red. Truecolor with 256-color adaptation, as in the siblings.

## 4. Architecture

Per repo convention, `mydb-src/` is a full copy-the-tree fork of `myconsole-src/` that swaps the domain layer: `fsx` → `dbx` + `drivers`, `icloud` → `jobs` + `registry`. The UI chrome, modal system, editor, keymap, themes, format helpers, and screenshot harness are carried over.

### 4.1 Package layout

```
mydb-src/                          module github.com/offsideai/mydb
  main.go                          flags → config → open registry → driver set → ui.New → tea.Run
  internal/ui/                     Bubble Tea model (from myconsole)
    model.go                       state, messages, update loop (never touches DB/network)
    tree.go                        node tree state: expand/flatten/childCache (from model.go tree logic)
    workspace.go                   tabbed right panel (re-domained panes.go)
    grid.go                        data/results grid (replaces preview.go)
    editor.go                      vim editor — copied verbatim from myconsole
    sql_wire.go                    editor I/O: run-query commands (replaces editor_wire.go)
    connform.go                    multi-field connection form modal
    modals.go                      Modal interface + Confirm/TypedConfirm/Input/Info/Help/Sort/Find
    actions.go, render.go, menu.go, keys.go, theme.go, format.go, hints.go
  internal/dbx/                    engine-agnostic domain layer (replaces fsx)
    driver.go                      Driver / Conn / Introspector interfaces, Capabilities
    node.go                        Node + NodeKind (replaces fsx.Entry), tree building
    result.go                      Result, Column, cell Value rendering rules
    page.go                        PageReq / stable-order paging contract
    dialect.go                     QuoteIdent / QuoteLiteral / ExplainPrefix per engine
    templates.go                   common-commands catalog (params, danger levels)
    plan.go                        sqlPlan — the safety choke point (§3.6)
    sort.go, find.go               copied from fsx (already generic)
    fake/                          in-memory driver for tests + screenshots
  internal/drivers/
    sqlite/                        dbx.Driver over modernc.org/sqlite (sqlite_master + PRAGMAs)
    postgres/                      dbx.Driver over jackc/pgx/v5 (pg_catalog)
  internal/registry/               master SQLite DB: connections, history; migrations
  internal/jobs/                   background queue (adapted from icloud/queue.go) + runners
  internal/config/                 TOML config (from myconsole; ~/.config/mydb/)
  cmd/screenshot/                  headless frame dumper, driven by dbx/fake
```

Provenance map (what each myconsole piece becomes):

| myconsole | mydb | fate |
|---|---|---|
| `internal/fsx` entry/ops | `internal/dbx` + `internal/drivers/*` | re-domained |
| `internal/fsx/sort.go`, `find.go` | `internal/dbx/` | copied (generic) |
| `internal/icloud` | — | dropped |
| `internal/icloud/queue.go` | `internal/jobs/queue.go` | adapted (worker pool, pause/cancel/reorder, snapshot-for-render) |
| `internal/ui/editor.go` | same | **verbatim** |
| `internal/ui/editor_wire.go` | `internal/ui/sql_wire.go` | replaced (`:w` runs SQL) |
| `internal/ui/panes.go` | `internal/ui/workspace.go` | re-domained |
| `internal/ui/preview.go` | `internal/ui/grid.go` | replaced |
| `internal/ui/{modals,keys,theme,format,hints,menu}.go` | same | mechanisms kept, tables re-domained |
| `cmd/screenshot` | same | kept, fake driver instead of a real dir |

Dependency direction: `ui → {dbx, registry, jobs}`; `drivers → dbx`; `jobs → dbx`; nothing imports `ui`.

### 4.2 The driver interface

mydb's own interface (not `database/sql` — see finding §1.5), engine differences expressed as data:

```go
type Driver interface {
    Engine() string                     // "sqlite" | "postgres"
    Capabilities() Capabilities
    Dialect() Dialect                   // QuoteIdent, QuoteLiteral, ExplainPrefix(analyze bool)
    Open(ctx context.Context, cfg ConnConfig) (Conn, error)
}

type Capabilities struct {
    MultipleDatabases bool          // pg: true — sqlite: false (connection == file)
    Schemas           bool          // pg: true — sqlite: false
    Roles             bool          // pg: true — sqlite: false (Roles group omitted)
    TransactionalDDL  bool
    ServerCancel      bool          // pg: cancel request — sqlite: interrupt
    Explain           ExplainStyle
    Backup            BackupStyle   // external tool vs in-process
    Maintenance       []MaintOp     // VACUUM, ANALYZE, REINDEX, INTEGRITY
}

type Conn interface {
    Ping(ctx context.Context) error
    Close() error
    Introspector                    // Databases, Schemas, Relations, Columns, Indexes, Roles
    Query(ctx context.Context, sql string, req QueryReq) (*Result, error)   // full scripts; MaxRows cap
    ReadPage(ctx context.Context, ref ObjectRef, pr PageReq) (*Result, error) // grid path, stable order
    Admin                           // role/grant/maintenance helpers behind capability checks
}

type Result struct {
    Columns   []Column      // name, type name
    Rows      [][]Value     // pre-rendered + kind tag (null/text/num/bytes/time)
    Affected  int64
    Stmts     int
    Elapsed   time.Duration
    Truncated bool
}
```

- The **tree builder, not the driver, decides levels** — it walks `Capabilities` to decide whether database/schema/roles nodes exist. A future MySQL driver is a new `internal/drivers/` package plus one registration line.
- **Libraries:** `modernc.org/sqlite` (pure Go — keeps `CGO_ENABLED=0` builds and also powers the registry, one dependency serving both) and `jackc/pgx/v5` natively (server-side cancel, simple-protocol multi-statement scripts, `PgError` position/hint for inline errors).
- **Paging:** offset-based in v1 with driver-chosen stable ordering (§1.7); `PageReq` is shaped to accept keyset cursors later without breaking the interface.

### 4.3 UI (Elm architecture)

The myconsole discipline is preserved verbatim: **the update loop never touches a database or the network**; all I/O runs in `tea.Cmd`s and returns as typed messages. Key routing priority is unchanged: modal → editor (if focused) → menu → filter input → esc → keymap dispatch.

- Commands/messages: `connectCmd → connectedMsg`, `childrenCmd → childrenLoadedMsg` (generation-tagged; stale generations dropped), `pageCmd → pageLoadedMsg`, `runQueryCmd → queryDoneMsg`, `adminCmd → adminDoneMsg`, plus the inherited tick/status-expiry messages.
- **Concurrency lanes** (relaxing myconsole's single in-flight op):
  1. *Introspection and page reads* run freely — correctness by generation tags.
  2. *Interactive queries*: at most **one per connection**; the model holds a `map[queryID]context.CancelFunc`; `esc` cancels, a new run on a busy connection offers "cancel and run".
  3. *Admin mutations* keep the strict single-in-flight `op` guard — they carry confirmations and must not interleave.
- The vim editor is untouched; only its wire layer changes: `:w`/`ctrl+r` dispatch `runQueryCmd` instead of a file save.

### 4.4 The master registry

One SQLite database owned by mydb at `~/.local/state/mydb/registry.db`, **created with mode 0600**, opened via the same modernc driver, with a numbered-migration runner (`schema_migrations`).

```sql
CREATE TABLE connections (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  engine      TEXT NOT NULL CHECK (engine IN ('sqlite','postgres')),
  locality    TEXT NOT NULL CHECK (locality IN ('local','remote')),  -- user-assigned; drives the tree sections
  path        TEXT,                          -- sqlite
  host        TEXT, port INTEGER, dbname TEXT, username TEXT,        -- postgres
  secret_ref  TEXT NOT NULL DEFAULT '',      -- v1: 'plain:<password>'; reserved: prompt/keychain/pgpass/env
  options     TEXT NOT NULL DEFAULT '{}',    -- JSON: sslmode, connect_timeout, read_only…
  created_at  TEXT NOT NULL,
  last_used_at TEXT
);
CREATE TABLE query_history (
  id INTEGER PRIMARY KEY,
  connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
  sql TEXT NOT NULL, started_at TEXT NOT NULL,
  duration_ms INTEGER, rows INTEGER, ok INTEGER, error TEXT
);
CREATE TABLE jobs_history (
  id INTEGER PRIMARY KEY,
  connection_id INTEGER, kind TEXT, target TEXT,
  started_at TEXT, finished_at TEXT, ok INTEGER, detail TEXT
);
CREATE TABLE settings ( key TEXT PRIMARY KEY, value TEXT );
CREATE TABLE schema_migrations ( version INTEGER PRIMARY KEY );
```

- **Passwords are stored in the registry** (v1 decision) as `secret_ref = 'plain:<password>'`, protected by the 0600 file mode. The tagged-reference design means keyring (`keychain:<account>`), `prompt`, `pgpass`, and `env:<VAR>` modes can be added later as new resolvers without a schema change — worth doing before the registry is ever synced or shared.
- Query history is appended fire-and-forget after every execution, pruned to `history_limit`.

### 4.5 Background jobs

`internal/jobs` adapts the iCloud download queue (states queued/running/done/failed/cancelled, pause, cancel, reorder, snapshot-for-render, tick-driven UI polling) with per-kind runners instead of the cloud bridge:

- **SQLite backup** — `VACUUM INTO 'dest'` in-process; progress = destination size vs source size (a real percentage).
- **Postgres backup/restore** — exec `pg_dump -Fc -v` / `pg_restore -v`; progress = bytes written to the artifact + current phase scraped from `-v` stderr (`dumping contents of table public.orders`); rendered as spinner + bytes + elapsed + phase (no fake percentage). Tool availability is probed at startup; when absent, backup falls back to the **built-in plain-SQL dumper** (schema + `INSERT`s via queries) with a visible fidelity warning, and restore of custom-format archives is greyed out with a hint.
- **Maintenance** ops (VACUUM/ANALYZE/REINDEX/integrity check) run as jobs too when the target is large, so the UI stays responsive.
- Concurrency: global limit 2, at most 1 job per connection. Jobs are **not persisted across restarts**; completed/failed jobs are logged to `registry.jobs_history` (visible in the Jobs tab).

### 4.6 Config

`~/.config/mydb/config.toml` (same loader/overlay as the siblings; `[keys]` overrides identical):

```toml
[general]
show_hints   = true
show_panel   = true          # workspace on launch
split_ratio  = 0.50
confirm_medium = true        # y/n confirms for Medium-danger ops (High is never disableable)

[query]
page_size     = 500          # data-grid page
max_rows      = 10000        # result-set cap per execution
history_limit = 1000

[backup]
dir            = "~/Backups/mydb"
prefer_pg_dump = true        # false forces the built-in plain-SQL dumper

[theme]
name = "default"             # default | dracula | solarized

[keys]                       # action = [keys…] — see internal/ui/keys.go
run_query = ["ctrl+r"]
quit      = ["ctrl+q"]
```

## 5. Dependencies

| Dep | Purpose |
|---|---|
| `charmbracelet/bubbletea` + `bubbles` + `lipgloss` | TUI runtime, text inputs, styling (inherited) |
| `BurntSushi/toml` | config (inherited) |
| `modernc.org/sqlite` | SQLite driver **and** the master registry — pure Go, no cgo |
| `jackc/pgx/v5` | PostgreSQL driver, used natively (cancel, scripts, rich errors) |
| `mattn/go-runewidth`, `charmbracelet/x/ansi` | cell-width-aware truncation (inherited) |
| *(external, optional)* `pg_dump` / `pg_restore` | faithful Postgres backup/restore; probed at startup, plain-SQL fallback when missing |

Build: `go -C mydb-src build -o mydb .` — `CGO_ENABLED=0` everywhere; unlike the siblings there is no cgo bridge, so cross-platform builds are first-class.

## 6. Edge cases & error handling

- **Huge tables** — grids only ever fetch one stable-ordered page; `COUNT(*)` is skipped in favor of estimates (`reltuples`, or a capped count) so selecting a billion-row table never hangs; result sets cap at `max_rows` with a visible `truncated` marker.
- **Connection loss mid-session** — a failed command marks the connection `✗` with the error, collapses live state, and offers reconnect; the tree keeps cached children (dimmed) so context isn't lost.
- **Wrong/changed password** — the connect error surfaces on the node and status bar; `E` reopens the form with the saved values.
- **Locked SQLite files** (another writer, stale WAL) — busy-timeout applied; persistent lock errors reported, never spun on.
- **Weird cells** — NULL (`␀`), bytea (`0x…` preview), multi-line and control characters sanitized for the grid, full value via `enter` popup; invalid UTF-8 replaced, never crashing the renderer.
- **`pg_dump` version skew** — the probe records the tool version; a server-newer-than-tool mismatch warns before the job starts.
- **Registry corruption** — integrity-checked on open; a corrupt registry is moved aside (`registry.db.broken-<ts>`) and recreated empty rather than blocking launch.
- **Terminal resize / narrow widths** — inherited responsive layout; the workspace collapses first, as in myconsole.

## 7. Testing

- `internal/dbx` — unit tests against `dbx/fake` (tree building per capability set, plan danger classification, dialect quoting, template rendering).
- `internal/drivers/sqlite` — real tests against temp files (introspection, paging stability, VACUUM INTO, interrupt-cancel).
- `internal/drivers/postgres` — integration tests gated on a reachable local server (`MYDB_TEST_PG_DSN`, skipped otherwise): introspection, cancel, roles, plain-SQL dumper round-trip.
- `internal/registry` — migration runner, CRUD, history pruning, 0600 mode.
- `internal/ui` — the inherited editor tests, plus golden-frame tests driven by `cmd/screenshot` + the fake driver.
- Manual acceptance: add a SQLite file and a local Postgres → browse both trees → page a big table → run/cancel a query → create user + database + grant via the Commands menu (preview shows exact SQL, High ops demand typed names) → back up both engines and watch the Jobs tab → restore → history survives restart.

## 8. Milestones

Each milestone ends with a runnable, useful binary. The working breakdown into Epics → Stories → Tasks (one Epic per milestone, with live status) is [ROADMAP-mydb.md](ROADMAP-mydb.md).

1. **M1 — Skeleton, registry, SQLite browse.** Fork myconsole-src; strip fs/iCloud; keep chrome/modals/editor/keys/themes. Registry with migrations + connection form (create/edit/delete). Tree with Local/Remote sections; SQLite driver introspection (tables/views/columns/indexes). *Runnable: save a SQLite connection, browse its schema.*
2. **M2 — Data grid + PostgreSQL.** `grid.go` with stable-order paging and the Info tab; Postgres driver (databases/schemas/relations/roles introspection); passwords from the registry. *Runnable: page through any table on either engine.*
3. **M3 — SQL runner.** Editor wired to `runQueryCmd`, per-connection cancellation, results grid with timing/affected, EXPLAIN, persistent history + history modal. Screenshot harness on the fake driver. *Runnable: a real query tool.*
4. **M4 — Admin + safety.** `sqlPlan` choke point, Medium/High confirmations (typed), Commands template catalog with parameter dialogs + SQL preview, roles/GRANT views, maintenance ops. *Runnable: the CREATE USER / CREATE DATABASE / GRANT flow end-to-end.*
5. **M5 — Jobs: backup & restore.** Jobs queue + runners (`VACUUM INTO`, `pg_dump`/`pg_restore`, plain-SQL fallback), Jobs tab with pause/cancel/reorder, `jobs_history`, docs + golden screenshots. *Runnable: v1 complete.*
