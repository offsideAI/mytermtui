# mydb

**A keyboard-driven terminal UI for browsing and administering local and remote databases** — SQLite files and PostgreSQL servers — built on the same Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea) foundation as its sibling apps (myterm, myconsole). One binary, no cgo.

mydb shows a database's schema and data as a navigable tree, runs SQL from an embedded vim-style editor, manages users/roles/permissions, performs maintenance (VACUUM/ANALYZE/REINDEX/integrity checks), and backs up / restores databases with a live background job queue — the things you'd otherwise juggle across `sqlite3`, `psql`, and `pg_dump`, minus the context switching.

- **Design spec:** [SPEC-mydb.md](SPEC-mydb.md) · **Roadmap:** [ROADMAP-mydb.md](ROADMAP-mydb.md) · **Build/install:** [DEPLOY.md](DEPLOY.md)

---

## Install

Requires **Go 1.22+**. Pure Go — no cgo, no external build tools.

```sh
git clone git@github.com:offsideAI/mytermtui.git
cd mytermtui
go -C mydb-src build -o mydb .
install mydb-src/mydb /opt/homebrew/bin/   # or sudo … /usr/local/bin/
```

**Optional (for faithful PostgreSQL backups):** `pg_dump` / `pg_restore` on your PATH. Without them, mydb falls back to a data-only plain-SQL dump with a clear fidelity warning.

## Quick start

```sh
mydb            # opens the tree; nothing connects until you ask
mydb --version
```

1. Press **`B`** to add a connection. Paste a whole connection string into the URL field (`postgres://user:pass@host:5432/db`, a `key=value` DSN, or a SQLite path) and `enter` to fill the fields, or type them directly. Choose the **Local/Remote** section and read-write vs read-only **Access**.
2. Put the cursor on the connection and press **`c`** (or `enter`) to connect. The status bar shows a green `●` with the name; a red `●` means nothing is connected. Only one connection is open at a time — connecting a new one releases the previous.
3. Expand the tree to schemas → Tables → a table. Press **`tab`** (or `ctrl+w`/`ctrl+o`) to focus the right workspace, then `]` to reach the **Data** tab and page through rows.
4. `]` again for the **SQL** tab: write a query in the vim editor, `ctrl+r` to run it. `ctrl+h` for history.
5. **`b`** backs up the selected connection; watch it in the **Jobs** tab (`Q`). `?` for the full key reference.

## The layout

```
┌ File  View  Database  Commands  Help ───────────────────────────────┐
│ Local ▸ prod-pg ▸ public ▸ users            │ Info ┊ Data ┊ SQL ┊ Jobs│ ← tabbed workspace
│ ▾ Local                                     │ id │ email      │ …    │
│   ▾ prod-pg   postgres · localhost:5432/app │ 1  │ a@x.com    │ …    │
│     ▾ public                                │ …                      │
│       ▸ Tables (34) / Views (6)             │                        │
│ ▸ Local (Annex)   3                         │                        │
│ ▸ Remote / Remote (Annex)                   │                        │
│ ▾ Roles                                     │                        │
│   ▾ localhost                               │                        │
│     admin      login · super                │                        │
│ ⣷ backup prod-pg → app.dump · dumping table │ public.orders · 14 MB  │ ← job bar
│ ┌ b backup ┊ C commands ┊ M maint ┊ Q jobs ┐                         │ ← hint bar
│ 2 connection(s)                       ● prod-pg · ? help             │ ← status bar
└──────────────────────────────────────────────────────────────────────┘
```

- **Tree sections**: **Local** / **Remote** hold your saved connections. A saved connection *is one database* — expanding it shows that database's schemas, not the whole server. Other databases discovered on a connected server appear in **Local (Annex)** / **Remote (Annex)**. Cluster roles live in the top-level **Roles** section, grouped by server host.
- **Workspace tabs** (`tab`/`ctrl+w` to focus, `[`/`]` to switch): **Info** (object summary + server details), **Data** (read-only paged grid), **SQL** (vim editor + results), **Jobs** (the background queue).

## Working with SQL

The **SQL** tab is the myconsole vim editor over a results grid. Write a query, run it with `ctrl+r`, `:w`, or `f5` — the whole buffer runs against the tab's connection. `esc` on the results cancels a running query (server-side on Postgres, interrupt on SQLite). `e` explains the query (`E` runs `EXPLAIN ANALYZE`, which confirms first). Every run is recorded; `ctrl+h` opens a fuzzy-searchable history you can reload into the editor.

Multi-statement scripts run as one script and report the last result set plus the statement count and total rows affected.

## Administration & safety

Reads run freely. Every write mydb *generates* for you flows through one guarded path: **Medium**-danger operations (CREATE/ALTER/GRANT, maintenance) show the exact SQL and a yes/no confirm; **High**-danger ones (DROP/TRUNCATE/role deletion) require typing the object's name. Raw SQL you type in the editor runs as-is — that's the expert path.

- **`C` — Common commands**: templated CREATE USER / CREATE DATABASE … OWNER / GRANT ALL / grant-read-only / change-password / drop-user / drop-database. Fill the parameters, preview the exact SQL, confirm.
- **`M` — Maintenance**: VACUUM / ANALYZE / REINDEX / integrity check, scoped to the selected table or the whole database per engine.
- **Read-only connections**: set Access = read-only in the form; the connection opens read-only, shows a `⏸` glyph, and every generated write is refused before its confirmation.

## Backup & restore

**`b`** backs up the selected connection into a background job; **`r`** restores (Postgres). Watch progress in the **Jobs** tab (`Q`) or the transient job bar. Jobs run at most two at a time, one per connection; a running job cancels with `c`, finished ones clear with `x`. Completed and failed jobs are logged (the queue itself doesn't survive a restart — a killed dump can only be restarted).

- **SQLite** — `VACUUM INTO`: an online, consistent, defragmented copy (better than copying the file). Restore is just adding the copy as a new connection (`B`).
- **PostgreSQL** — `pg_dump -Fc` when the tool is on your PATH (restore via `pg_restore`); otherwise a built-in **data-only** plain-SQL dump with a fidelity warning. Set `prefer_pg_dump = false` in config to always use the fallback.

## Keyboard reference

Press `?` for the live version (it reflects your remaps).

| Group | Keys |
|---|---|
| **Move** | `↑↓`/`kj` cursor · `enter`/`→` expand (connects first) · `←`/`h` collapse / parent · `g`/`G` top/bottom · `pgup`/`pgdn` page |
| **Connections** | `c` connect (disconnects the previous) · `d` disconnect · `B` new · `E` edit · `X` delete · `p` reveal password (10s) |
| **Panels** | `tab`/`ctrl+w`/`ctrl+o` tree ↔ workspace · `[` `]` tabs · `<`/`>` resize · `F3`/`P` toggle panel |
| **SQL** | `ctrl+r`/`:w`/`f5` run · `esc` cancel (results) · `e`/`E` explain · `ctrl+h` history |
| **Admin** | `C` commands · `M` maintenance · `b` backup · `r` restore · `Q` jobs · `I` info |
| **Data grid** | `J`/`K` page · `h`/`l` columns · `enter` full cell · `y`/`Y` copy cell/row |
| **App** | `f`/`filter` · `ctrl+r` refresh · `m`/`F10` menus · `?`/`F1` help · `H` hint bar · `ctrl+q` quit |

## Configuration

`~/.config/mydb/config.toml` (same loader/overlay and `[keys]` remapping as the sibling apps):

```toml
[general]
show_panel     = true
split_ratio    = 0.50        # tree's share of the width
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

[keys]                       # action = [keys…] — see mydb-src/internal/ui/keys.go
run_query   = ["ctrl+r"]
disconnect  = ["d"]
```

State (the master registry of connections, query and job history) lives in `~/.local/state/mydb/registry.db`, created private (`0600`) — your saved passwords are in there.

## Architecture

An independent Go module (`github.com/offsideai/mydb`) in `mydb-src/`, forked from myconsole and re-domained from files to databases:

```
mydb-src/
  main.go                 flags → config → registry → ui.New → tea.Run
  internal/ui/            Bubble Tea model: tree, tabbed workspace, SQL editor,
                          command templates, jobs view, modals, theme, keys
  internal/dbx/           engine-agnostic layer: Driver/Conn/Introspector,
                          Node tree, Result/paging, dialect, plan, templates, fake/
  internal/drivers/       sqlite (modernc.org/sqlite) · postgres (jackc/pgx/v5)
  internal/registry/      master SQLite DB: connections + query/job history
  internal/jobs/          background queue + backup/restore runners
  internal/config/        TOML config
  cmd/screenshot/         headless frame dumper for docs
```

Design notes: the update loop never touches a database — all I/O runs in commands and returns as messages; the tree builder (not the drivers) decides which levels exist, from each engine's capability flags; every UI-generated write is a `Plan` with a static danger tier; jobs run in goroutines and report progress through a locked callback while browsing continues.

## Development

```sh
cd mydb-src
go test ./... && go vet ./...
```

PostgreSQL integration tests are skipped unless you point them at a server:

```sh
MYDB_TEST_PG_DSN="host=localhost port=5432 user=me dbname=postgres" go test ./...
```
