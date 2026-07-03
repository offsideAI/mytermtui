# mytermtui — Specification

A keyboard-driven, colored terminal UI for browsing the filesystem with Finder-level file operations, plus first-class iCloud Drive awareness: it shows which files are evicted (cloud-only), lets the user mark files/folders for download, runs a download queue with progress, and can evict local copies back to the cloud.

- **Language / stack:** Go + Bubble Tea (single static-ish binary; one small cgo bridge to Apple frameworks)
- **Target platform:** macOS 13+ (primary: macOS 26 "Tahoe"-era, FileProvider-based iCloud Drive). Non-macOS builds compile and run as a plain file browser with iCloud features disabled.
- **Binary name:** `mytermtui`

---

## 1. Background: how iCloud Drive looks on modern macOS (verified findings)

These were verified on the target machine (macOS 26.4, Darwin 25.4.0) and are the foundation of the iCloud feature set. **Do not** design around `.icloud` placeholder files or `brctl download` — both are legacy.

1. **iCloud Drive root:** `~/Library/Mobile Documents/com~apple~CloudDocs` (Desktop/Documents sync also lives under `~/Library/Mobile Documents`).
2. **Evicted ("cloud-only") files are dataless files, not placeholders.** They appear with their real name and full logical size in `ls`, but occupy zero blocks and carry the `SF_DATALESS` flag:
   ```
   size=247393316 blocks=0 flags=0x40000060   # SF_DATALESS = 0x40000000
   ```
   A fully local file has `blocks > 0` and no `SF_DATALESS` bit. Detection is therefore a plain `lstat`: `st_flags & SF_DATALESS != 0`. In Go: `syscall.Stat_t.Flags & 0x40000000`.
3. **Directories can be dataless too** (contents not yet enumerated locally). Reading such a directory materializes its *listing* (cheap), not its files' contents.
4. **`brctl download` / `brctl evict` no longer exist** on macOS 26 (`brctl` retains only diagnose/log/dump/status/accounts/quota/monitor). `fileproviderctl` exposes no materialize/evict either. There is **no supported CLI** for download/evict anymore.
5. **The supported programmatic APIs** (Foundation, via cgo):
   - Download: `-[NSFileManager startDownloadingUbiquitousItemAtURL:error:]` — asynchronous; returns immediately, fileproviderd downloads in background.
   - Evict: `-[NSFileManager evictUbiquitousItemAtURL:error:]`.
   - Status: `NSURL` resource values (`NSURLUbiquitousItemDownloadingStatusKey`, `NSURLIsUbiquitousItemKey`).
   - Trash: `-[NSFileManager trashItemAtURL:resultingItemURL:error:]`.
6. **Fallback download without cgo:** simply `open()` + `read()` a dataless file — the kernel materializes it on access (blocking). Usable but inferior (blocks a thread per file, no cancel). cgo path is the primary mechanism.
7. **Accidental-download hazard:** any tool that reads file contents (preview, checksum, grep) will silently trigger a download of a dataless file. The app must guard against this: a thread can opt out via `setiopolicy_np(IOPOL_TYPE_VFS_MATERIALIZE_DATALESS_FILES, IOPOL_SCOPE_THREAD, IOPOL_MATERIALIZE_DATALESS_FILES_OFF)`, after which reads on dataless files fail with `EDEADLK` instead of downloading. The preview pane MUST run under this policy or skip dataless files.
8. **Progress (revised after implementation):** fileproviderd stages downloads out of view — `st_blocks` stays 0 and `SF_DATALESS` remains set until the finished payload is swapped in — so lstat polling shows nothing. Spotlight hides ubiquitous attributes from non-entitled processes, and `NSProgress` file-progress subscriptions receive no publications (both verified empirically). The one accessible source is Apple's entitled `brctl status`, which prints per-item `downloading:NN.N%` lines with elided names (`first{middle-count}last.ext`) and exact sizes; the app polls it asynchronously and matches items by directory, size, and name pattern.
9. **Filenames contain exotic Unicode** (e.g. screen-recording names use U+202F narrow no-break space before "PM"). All paths are opaque bytes end-to-end; never round-trip through shell strings. Display through a sanitizer that preserves visual fidelity.

---

## 2. Goals and non-goals

### Goals
- Fast, keyboard-only navigation of the whole filesystem with a colorful, discoverable TUI (menus + status bar + help), usable by people who don't know vim.
- Finder-parity file management: open, rename, copy/move/duplicate, new folder/file, trash (recoverable, not `rm`), get info, compress, quick look, reveal in Finder, hidden files, sorting, search.
- iCloud: per-file sync status at a glance; mark any set of files/folders for download; queued, cancellable downloads with progress; evict local copies; never trigger downloads accidentally.
- Single runnable binary, `go build`-simple, config optional.

### Non-goals (v1)
- Not a file *transfer* tool (no SFTP/S3/MTP), no archives-as-virtual-dirs, no bulk rename engine, no plugin system.
- No Finder tags/labels editing, no Spotlight comment editing (read-only display is fine later).
- No management of non-iCloud FileProvider domains (Dropbox/GDrive) in v1 — the dataless detection will incidentally work for them; official support later.
- No mouse requirement (mouse clicks may work via Bubble Tea, but every feature must be reachable by keyboard).

---

## 3. UX design

### 3.1 Screen layout

```
┌ File  Edit  View  Go  iCloud  Help ──────────────────────── mytermtui ┐  ← menu bar (F-keys/Alt)
│ 📁 ~/Library/Mobile Documents/com~apple~CloudDocs/demo-docs           │  ← breadcrumb (editable)
├────────────────────────────────────────────┬───────────────────────────┤
│   Name                    Size     Modified│  Preview / Info panel     │
│ ▸ archived-recordings    --       May  7  ☁│  (toggle: F3 / p)         │
│ ▸ meeting-notes          --       Dec 22  ✓│                           │
│ ● demo-video-1.mov       2.9 GB   Aug 13  ☁│  demo-video-2.mov         │
│ ● demo-video-2.mov       247 MB   Jun 27  ⇣│  Kind: QuickTime movie    │
│   demo-video-3.mov       2.0 GB   Sep  4  ☁│  Size: 247.4 MB (0 local) │
│   screenshot.png         14 KB    Jan  3  ✓│  iCloud: ⇣ downloading 41%│
├────────────────────────────────────────────┴───────────────────────────┤
│ ⇣ Queue 2/5 · 1.2 GB of 5.6 GB · demo-video-2.mov 41%                 │  ← download bar (when active)
│ 3 selected (5.1 GB) · 23 items · sort: name ↑ · [?] help              │  ← status bar
└─────────────────────────────────────────────────────────────────────────┘
```

- **Menu bar** — real pull-down menus (File/Edit/View/Go/iCloud/Help) opened with `F10` or `Alt+letter`; navigable with arrows; every item shows its shortcut. Menus are the discoverability layer; shortcuts are the speed layer.
- **File list** — the main pane. Columns: selection dot, name (with type icon), size, modified, **iCloud status glyph**. Column set is configurable.
- **Preview/info panel** — optional right split: text-file head, directory summary, image dimensions, and full metadata (Get Info). Never materializes dataless files (§1.7); shows "☁ evicted — press d to download" instead.
- **Download bar** — appears while the queue is non-empty: overall progress, current file, speed; hidden otherwise.
- **Status bar** — selection count/size, item count, sort mode, filter state, transient messages/errors.
- **Modals** — centered overlays for confirm dialogs, rename/new-name input, go-to-path (with tab completion), search, help (full keymap), and the queue manager.

### 3.2 iCloud status glyphs (with colors)

| Glyph | State | Detection |
|---|---|---|
| `✓` (green) | Local & synced | not dataless, inside iCloud root |
| `☁` (blue) | Evicted, cloud-only | `SF_DATALESS` set |
| `⇣` (yellow, animated) | Downloading | in queue / blocks growing |
| `⇡` (yellow) | Uploading / not yet synced | `brctl status` parse (best-effort, v1.1) |
| `◌` (dim) | Marked for download, queued | app state |
| *(none)* | Outside iCloud | path not under an iCloud root |

Directory rows aggregate: `☁` if any descendant known-evicted (computed lazily on visit, never recursively scanned up front), `✓` otherwise.

### 3.3 Keybindings

Dual scheme, both always active: **arrows/Enter/F-keys** (Finder-like, discoverable) and **vim-ish** letters (fast). All rebindable in config.

**Navigation**
| Key | Action |
|---|---|
| `↑/↓` or `k/j` | move cursor |
| `→/l` or `Enter` | open: enter directory / open file with default app (`open`) |
| `←/h` or `Backspace` | go to parent |
| `g` / `G` | top / bottom · `PgUp/PgDn` page |
| `⌥←/⌥→` or `[ / ]` | history back / forward |
| `~` | home · `/` root · `i` iCloud Drive root |
| `⌘G` or `:` | go to path (input with tab-completion) |
| `z` | toggle hidden files · `s` sort menu (name/size/modified/kind, asc/desc, dirs-first) |
| `f` | filter-as-you-type in current dir · `F` recursive fuzzy find under cwd |

**Selection**
| Key | Action |
|---|---|
| `Space` | toggle select + move down |
| `v` | range-select mode · `a` select all · `A`/`Esc` clear |

**File operations** (act on selection, else cursor item)
| Key | Action |
|---|---|
| `c` / `x` / `p` | copy / cut / paste (app-internal clipboard) |
| `r` or `F2` | rename (inline input, pre-filled) |
| `D` or `F8`/`Delete` | move to Trash (native trash — recoverable; confirm) |
| `⌘D` | duplicate ("name copy.ext") |
| `n` / `N` or `F7` | new folder / new empty file |
| `o` | open · `O` open-with menu (apps from `LaunchServices`, fallback: prompt) |
| `Enter` on app/pkg | descend into bundle ("Show Package Contents") when `z`-hidden mode on |
| `q` or `F3` on file | Quick Look (`qlmanage -p`, suspends TUI) — **blocked on dataless files** |
| `I` or `⌘I` | Get Info panel (perms, dates, kind via UTI, size incl. on-disk vs logical, iCloud state) |
| `Z` | compress selection to `.zip` (`ditto -ck --sequesterRsrc`) |
| `R` | reveal in Finder (`open -R`) |
| `T` | open Terminal here · `.` copy path(s) to system clipboard (`pbcopy`) |
| `u` | undo last op (rename/move/trash restore — single-level, best-effort) |

**iCloud** (the differentiator)
| Key | Action |
|---|---|
| `d` | mark selection/cursor for download (dirs: recursive) → adds to queue |
| `e` | evict selection (remove local copy, keep in cloud; confirm; refuses if not yet uploaded) |
| `⌘d` | download entire current directory |
| `Q` | queue manager modal: reorder, pause/resume, cancel items, cancel all |
| `S` | iCloud summary for cwd: local vs evicted counts/bytes (computed on demand) |

**App**
| Key | Action |
|---|---|
| `?` or `F1` | help overlay (searchable keymap) · `F10`/`Alt+F` menus · `Ctrl+r` refresh dir · `Q`(menu)/`Ctrl+q` quit (confirm if queue active) |

### 3.4 Color & theming

- Truecolor with 256-color fallback (Lip Gloss adaptive profiles); respects light/dark terminal backgrounds.
- Row coloring by kind: directories bold blue, symlinks cyan (broken: red), executables green, images/media magenta, hidden dimmed; selected rows get accent background; cursor row inverted.
- Built-in themes: `default` (respects terminal palette), `dracula`, `solarized`; user themes in config.

---

## 4. Architecture

### 4.1 Package layout

```
mytermtui/
  main.go                 // flag parsing (start dir, --version), tea.NewProgram
  internal/ui/            // Bubble Tea: root model, panes, menus, modals, theming
  internal/fs/            // dir listing, watchers, file ops (copy/move/trash…), sorting, filtering
  internal/icloud/        // dataless detection, download/evict bridge, queue, progress polling
      bridge_darwin.go    // cgo → Foundation (startDownloading…, evict…, trashItem…)
      bridge_stub.go      // !darwin: everything returns ErrUnsupported
  internal/config/        // TOML config + keymap + themes (~/.config/mytermtui/config.toml)
```

### 4.2 UI (Elm architecture)

- Root `Model` holds: current dir state (entries, cursor, selection, sort/filter), history stack, panes' visibility, active modal, queue snapshot, theme, keymap.
- All I/O via `tea.Cmd`s — **the update loop never touches the disk**. Directory reads, file ops, and stat-polls run in goroutines and come back as messages (`dirLoadedMsg`, `opDoneMsg{err}`, `queueTickMsg`…).
- Large-directory strategy: `os.ReadDir` in a worker, entries streamed in batches (first paint < 50 ms even on 100k-entry dirs); lstat/iCloud-status enrichment done lazily for the visible window ± one page.
- Live refresh: FSEvents is overkill for v1 — refresh on focus/op-completion plus a 2 s lstat re-check of visible rows (cheap, also drives glyph updates while downloads land).

### 4.3 iCloud subsystem

- **Detection** (`icloud.Status(path)`): `lstat` → `Flags & SF_DATALESS`; plus `IsInICloud(path)` by prefix-matching resolved path against `~/Library/Mobile Documents/…`. Pure Go, no cgo.
- **Bridge** (cgo, darwin only): thin wrappers over `NSFileManager` `startDownloadingUbiquitousItemAtURL`, `evictUbiquitousItemAtURL`, `trashItemAtURL`. Each takes a path, returns error string. No Objective-C beyond one `.m`-free cgo block; links `-framework Foundation`.
- **Download queue** (`icloud.Queue`):
  - FIFO of items (file paths; directories expand to their dataless descendants via a background walk that reads only listings, never contents).
  - Concurrency: fileproviderd does the actual transfer, so "start" is cheap — but we cap in-flight materializations (default 3) to keep progress legible and disk pressure sane.
  - Per-item lifecycle: `queued → starting → downloading(pct) → done | failed(err) | cancelled`.
  - **Progress:** 500 ms tick per in-flight item; percent comes from a throttled async `brctl status` parse (see §1.8), never regressing between slow polls; overall bar aggregates bytes. Stalled > 30 s ⇒ marked `stalled` with hint (network/quota).
  - **Cancel:** best-effort — evict the partially-materialized item. Persisted queue (JSON in state dir) so an interrupted session can resume marks.
- **Safety rails:** the preview/info goroutines set `IOPOL_MATERIALIZE_DATALESS_FILES_OFF` (via the bridge) so *only* explicit queue items ever trigger downloads. Evict requires confirm and skips files with unsynced local changes (best-effort check via `brctl status` parse; on ambiguity, warn).

### 4.4 File operations engine

- Copy/move: `os.Rename` fast-path, cross-device fallback to streamed copy with progress (reuses the download-bar UI); preserves permissions, times, xattrs (`clonefile(2)` fast-path on APFS via `unix.Clonefile`).
- **Copying a dataless file requires materialization** — prompt: "3 items are cloud-only (5.1 GB). Download first?" (never silently pull gigabytes).
- Trash via bridge `trashItemAtURL` (correct Finder semantics incl. put-back); fallback `~/.Trash` move if the call fails.
- Every mutating op returns an `Undo` closure where feasible (rename back, move back, un-trash via saved put-back URL); single-level undo stack.
- Name collisions: modal — Replace / Keep both (` copy`, ` 2`) / Skip / Cancel, with "apply to all".

### 4.5 Config

`~/.config/mytermtui/config.toml` (all optional):

```toml
[general]
start_dir = "~"            # or "last"
show_hidden = false
confirm_trash = true
dirs_first = true

[icloud]
max_concurrent_downloads = 3
poll_interval_ms = 500

[theme]
name = "default"           # or table of custom colors

[keys]                     # action = ["key", "key"...]
download = ["d"]
quit = ["ctrl+q"]
```

---

## 5. Dependencies

| Dep | Purpose |
|---|---|
| `github.com/charmbracelet/bubbletea` | TUI runtime (Elm architecture) |
| `github.com/charmbracelet/bubbles` | list/viewport/textinput/progress/help components |
| `github.com/charmbracelet/lipgloss` | styling, layout, adaptive color |
| `golang.org/x/sys/unix` | lstat flags, `Clonefile`, iopolicy syscall |
| `github.com/BurntSushi/toml` | config |
| cgo + `-framework Foundation` (darwin) | download/evict/trash bridge |

Build: `go build ./...` → single binary (`CGO_ENABLED=1` on darwin; pure Go elsewhere). No runtime dependencies; `qlmanage`/`open`/`ditto`/`pbcopy` are macOS built-ins invoked opportunistically.

---

## 6. Edge cases & error handling

- **Permissions:** browsing `~/Library` and iCloud roots needs Full Disk Access for the *terminal app*. On `EPERM`/`EACCES` at an iCloud path, show a one-line hint: "Grant Full Disk Access to your terminal in System Settings → Privacy & Security."
- **Unicode filenames:** paths treated as bytes; rendering strips control chars and uses width-aware truncation (`…` middle-ellipsis, Finder-style). Never construct shell command strings from names — always exec with arg vectors.
- **Huge dirs:** streamed listing (§4.2); filter and fuzzy-find operate on the loaded snapshot.
- **Symlink loops:** never auto-resolve recursively; show target in info panel.
- **Disk-full during download:** surface fileproviderd stall as `stalled`, keep queue paused-able.
- **Non-macOS / no iCloud account:** iCloud menu items disabled with tooltip, everything else works.
- **Terminal resize** down to 60×15 gracefully (panels auto-collapse: preview → download bar → menu bar hints).

---

## 7. Testing

- `internal/fs`: table-driven unit tests on tmpdirs (ops, collisions, sorting, undo).
- `internal/icloud`: detection tested against fixture stat flags; queue state-machine tested with a fake bridge; bridge itself smoke-tested manually (documented script) since it needs a live iCloud account.
- UI: `teatest` golden-file tests for key flows (navigate, select, rename modal, queue render).
- Manual acceptance checklist including: evicted `.mov` in `com~apple~CloudDocs` shows `☁`, `d` + queue downloads it to `✓`, `e` returns it to `☁`, preview of evicted file does **not** download it.

---

## 8. Milestones

1. **M1 — Browser skeleton:** listing, navigation, sorting, hidden toggle, theming, status bar, help overlay.
2. **M2 — iCloud read-only:** dataless detection, status glyphs, iCloud summary, guarded preview (iopolicy).
3. **M3 — Download/evict:** cgo bridge, mark + queue + progress bar + queue manager, evict.
4. **M4 — File ops:** copy/cut/paste/rename/trash/duplicate/new/undo, collision dialogs, open/open-with/Quick Look/reveal.
5. **M5 — Polish:** menus, go-to-path, fuzzy find, config/keymap, compress, Get Info, persistence, docs.

Each milestone ends runnable; M1–M3 alone already solve the motivating use case (find `☁` cloud-only videos, mark, download).
