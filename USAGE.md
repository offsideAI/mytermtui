# mytermtui — Usage Guide

A hands-on manual for day-to-day use. For installation, architecture,
and configuration reference, see the [README](README.md).

- [Launching](#launching)
- [Reading the screen](#reading-the-screen)
- [Moving around](#moving-around)
- [Selecting files](#selecting-files)
- [Everyday file operations](#everyday-file-operations)
- [Using the menus](#using-the-menus)
- [Finding things](#finding-things)
- [iCloud workflows](#icloud-workflows)
- [Getting help inside the app](#getting-help-inside-the-app)
- [Making it yours](#making-it-yours)
- [Recipes](#recipes)

---

## Launching

```sh
mytermtui                    # start in ~ (or your configured start_dir)
mytermtui ~/Downloads        # start in a specific folder
mytermtui --version
mytermtui --config /path/to/config.toml
```

First launch over iCloud paths: if the status bar reports *operation not
permitted*, grant your **terminal app** Full Disk Access (System
Settings → Privacy & Security), then restart the terminal.

Quit with `ctrl+q`. If downloads are running you'll be asked to confirm
— the transfers themselves continue in macOS's `fileproviderd` either
way, and mytermtui picks the progress tracking back up on relaunch.

## Reading the screen

![The browser: menu bar, breadcrumb, file list, shortcut bar, status bar](screenshots/01-browser.png)

Top to bottom:

1. **Menu bar** — `File Edit View Go iCloud Help`, with the app name at right.
2. **Breadcrumb** — the current path (`~` abbreviated). A `☁` prefix means you are inside iCloud Drive; a spinner appears while a directory loads.
3. **File list** — one row per entry:
   - `●` in the first column marks *selected* entries;
   - `▸` marks folders, `@` symlinks;
   - name, size (`--` for folders), modified date;
   - the **iCloud column** at far right: `✓` local · `☁` cloud-only · `◌` queued · `⇣` downloading · `·` iCloud folder.
   - Colors: folders blue, media magenta, archives yellow, executables green, broken symlinks red, hidden files dimmed. The cursor row is highlighted; selected rows get a tinted background.
4. **Download bar** — appears only while the queue is active: files done/total, bytes, a progress bar, and the current file's percentage.
5. **Shortcut bar** — the boxed, nano-style cheat sheet. It swaps its contents when a menu, dialog, or the filter is focused, and always shows your actual key bindings. `H` hides it.
6. **Status bar** — selection count and size, item count, operation results (green = success, red = error with a reason), sort order.

## Moving around

| Want to… | Press |
|---|---|
| Move the cursor | `↑`/`↓` or `k`/`j` (`g`/`G` first/last, `pgup`/`pgdn` pages) |
| Open a folder / reveal a file | `enter` (also `→`/`l`). Folders are entered; files are revealed in their enclosing folder in Finder (set `enter_opens_file = "app"` to open them instead — `o` always opens in the app) |
| Go to the parent | `backspace` (also `←`/`h`) — cursor lands on the folder you left |
| Go back / forward | `[` / `]` (also `alt+←`/`alt+→`), like a browser |
| Jump home / root / iCloud | `~` / `/` / `i` |
| Type a path | `:` — tab-completes, `~` expands, relative paths allowed |
| Refresh | `ctrl+r` |

Failed navigations (deleted folder, no permission) leave you where you
were, with the reason in the status bar.

## Selecting files

Most operations act on the **selection**, or on the cursor row if
nothing is selected.

- `space` — toggle the cursor row and step down (tap it repeatedly to select a run of files).
- `v` — range mode: press once to anchor, move the cursor to stretch the highlighted range, press `v` (or `esc`) to commit it into the selection.
- `a` — select everything visible; `A` — clear.
- `esc` — clears range → selection → filter, in that order.

The status bar always shows how many items and bytes are selected.

## Everyday file operations

**Copy / move:** `c` copies the selection to mytermtui's internal
clipboard, `x` cuts. Navigate anywhere and `p` pastes. If names collide
you choose once for the whole batch:

- **Keep both** — incoming files get ` 2`, ` 3`, … suffixes;
- **Replace** — the existing files are moved to the **Trash** (recoverable, and `u` restores them);
- **Skip** — conflicting items are left alone.

On the same APFS volume copies are instant (copy-on-write clones);
otherwise a progress readout appears in the status bar. Permissions,
timestamps, and extended attributes are preserved.

**Rename:** `r` opens an input pre-filled with the current name.

**Trash:** `D` (confirmation on by default). Items go to the real macOS
Trash with Finder "Put Back" metadata.

**Undo:** `u` reverses the last operation — paste, rename, trash,
duplicate, compress, or new file/folder. One level.

**Create:** `n` new folder, `N` new empty file.

**Duplicate:** `ctrl+d` makes `name copy.ext` next to the original.

**Compress:** `Z` zips the selection (`ditto`, Finder-compatible,
resource forks preserved). One item → `name.zip`; several → `Archive.zip`.

**Inspect:** `I` shows Get Info — kind, logical vs on-disk size,
permissions, timestamps, and iCloud state:

![Get Info: a 2.91 GB movie occupying 0 bytes on disk — evicted](screenshots/05-get-info.png)

**Hand off to macOS:** `o` open in the default app, `q` Quick Look (floating preview window), `O`
open with a named app, `R` reveal in Finder, `T` open Terminal here,
`.` copy the full path(s) to the system clipboard.

## Using the menus

Press `m` (or `F10`; hold `fn` if Mission Control steals it). The File
menu opens first; `←`/`→` switch menus, `↑`/`↓` walk items, `enter`
runs, `esc` closes. Every item displays its shortcut, so the menus are
also how you *learn* the shortcuts:

![The File menu, each item labeled with its key](screenshots/04-file-menu.png)

## Two-panel browsing

Press `→` (or `l`) on a folder and it opens in a **right panel** taking
70% of the width, with the listing you came from docked on the left:

![Two panels: parent listing left, opened folder focused on the right](screenshots/08-dual-panel.png)

- **`tab`** switches focus between the panels. Each panel remembers its
  own cursor position, selection, filter, and back/forward history —
  and every operation (copy, trash, download, …) acts on the focused
  panel.
- **`←`** in the right panel steps focus back to the left panel
  (`backspace` still goes to the parent folder). Pressing `→` on a
  folder *inside* the right panel cascades: the right panel becomes the
  left one and the new folder opens on the right.
- **`<` / `>`** shrink/widen the left panel in 5% steps (0.15–0.85);
  the starting share is `split_ratio` in the config (default 0.30).
- **`ctrl+w`** closes the split, keeping the left panel.
- A handy pattern: select files in one panel, `tab` to the other, `p`
  to paste them there.

The preview panel (`F3`) is available in single-panel mode.

## Finding things

**Filter the current folder** — press `f` and type; the list narrows as
you type. `enter` keeps the filter active while you work (a `filter:`
line stays visible), `esc` clears it:

![Typing "md" narrows the list to the three markdown files](screenshots/02-filter.png)

**Search recursively** — `F` opens fuzzy find. Type a query (subsequence
match, so `dwn` finds `download.png`), `enter` to search, `↑`/`↓` to
pick, `enter` to jump to the file's folder with the cursor on it.

**Sort** — `s`: name, size, modified, or kind; `enter` on the current
key flips ascending/descending; `d` toggles folders-first.

**Hidden files** — `z` shows/hides dotfiles and Finder-hidden items.

## iCloud workflows

### See what's local vs cloud-only

The right-hand column answers it per file. For a whole folder, press
`S`: counts and byte totals for local vs evicted files.

### Download ("sync down")

1. Select what you want — folders are fine, they're scanned recursively (listings only).
2. Press `d`. The status bar reports how many files and bytes were queued; glyphs flip to `◌`.
3. Watch the download bar; each file goes `◌ → ⇣ → ✓`.
4. `Q` opens the queue manager: `c` cancel one (partial data is evicted again), `C` cancel all, `p` pause new starts, `K`/`J` reorder, `x` clear the finished list.

Quitting mid-download is safe: transfers continue in the system daemon,
the queue file remembers your marks, and the next launch resumes
tracking.

### Evict ("free up space")

Select local iCloud items, press `e`, confirm. Local bytes are freed,
the file stays in iCloud showing `☁`, and you can re-download any time.

### Browsing never downloads by accident

Evicted files are downloaded **only** when you ask — via `d`, or after
an explicit confirmation when an operation requires the bytes (copying
or compressing cloud-only items tells you how much would be pulled).
The preview panel is hard-blocked from materializing files at the OS
level; on an evicted file it shows this instead of the content:

![Preview panel on an evicted file: cloud-only, 0 bytes local, press d to download](screenshots/03-preview.png)

Quick Look and Open With likewise refuse evicted files and point you to
`d` first.

## Getting help inside the app

`?` opens the searchable keyboard reference — grouped like this guide,
scrollable with `j`/`k`, and always reflecting your actual bindings:

![The help overlay](screenshots/06-help.png)

Between the help overlay, the menus, and the shortcut bar, every
feature is discoverable without leaving the app.

## Making it yours

All customization lives in `~/.config/mytermtui/config.toml` (see the
[README](README.md#configuration--themes) for the full annotated file).
The quick hits:

- **Rebind keys** — `[keys]` table, action names from `mytermtui-src/internal/ui/keys.go`. Menus, help, and the shortcut bar update to match.
- **Theme** — `[theme] name = "dracula"` (or `solarized`; `default` follows your terminal palette):

  ![The dracula theme](screenshots/07-theme-dracula.png)

- **Behavior** — start directory, hidden files on launch, trash confirmation, folders-first sorting, shortcut bar visibility, download concurrency and poll rate.

## Recipes

**Free up 20 GB fast** — open the big iCloud folder, `s` → sort by
size descending, `v` and stretch a range over the giants, `e`, confirm.
`S` before and after to see the reclaimed bytes.

**Take a project offline before a flight** — cursor on the project
folder, `d`, then `Q` to watch it fill in. Everything shows `✓` when
you're safe to disconnect.

**Archive and hand off** — select the files, `Z` to zip, `R` to reveal
the archive in Finder for drag-and-drop into Mail or Slack.

**Clean screenshots of *this* app** — see [GETSTARTED.md](GETSTARTED.md)
for regenerating the images used in these docs.
