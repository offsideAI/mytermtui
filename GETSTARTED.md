# Getting started: capturing the UI screenshots

The README embeds seven PNGs from `screenshots/`. They are not committed
by default — generate them yourself against a folder whose contents are
safe to publish, and **review every image before committing**.

## Option A — the built-in generator (recommended)

Renders the real UI headlessly (no window opens) and rasterizes to PNG.
Run from the repo root:

```sh
go -C mytermtui-src run ./cmd/screenshot -dir "$PWD" -filter md -out ../screenshots/ansi
python3 scripts/ansi2png.py screenshots/ansi screenshots   # needs Pillow

open screenshots     # ← review before committing
git add screenshots && git commit
```

- `-dir` is the folder shown in the shots. The repo root itself is a safe
  choice (README.md, SPEC.md, mytermtui-src/, …).
- `-filter` is what gets typed in the filter scenes (02/03/05) — pick a
  string that matches files in `-dir` (`md` matches the markdown files at
  the repo root; `go` matches nothing there since the sources moved into
  `mytermtui-src/`).
- The intermediate `screenshots/ansi/*.ansi` files are gitignored but sit
  on disk; delete them if the folder you shot is sensitive.

### Showing the iCloud states (☁ / ⇣ / ✓)

In a plain folder nothing is evicted, so `03-preview` and `05-get-info`
won't show the cloud glyphs. Build a safe demo folder in iCloud Drive:

```sh
DEMO=~/Library/Mobile\ Documents/com~apple~CloudDocs/mytermtui-demo
mkdir -p "$DEMO" && cd "$DEMO"
mkfile 200m demo-video-1.mov
mkfile 120m demo-walkthrough.mov
echo "hello" > notes.txt
```

Wait for iCloud to upload (the cloud icon in Finder disappears), evict
the `.mov` files — easiest with mytermtui itself: run it there, select
with `space`, press `e` — then regenerate:

```sh
cd ~/repos/offsideai/githubrepos_workspace_active_1/mytermtui
go -C mytermtui-src run ./cmd/screenshot -dir "$DEMO" -filter demo -out ../screenshots/ansi
python3 scripts/ansi2png.py screenshots/ansi screenshots
```

## Option B — real screenshots by hand

1. Resize the terminal to roughly 120×35.
2. Run `mytermtui-src/mytermtui <safe folder>`.
3. Set up each scene, then press `⌘⇧4`, then `Space`, then click the
   window (saves a PNG to the Desktop):

| File name | Scene keys |
|---|---|
| `01-browser.png` | navigate with `j`, select two files with `space` |
| `02-filter.png` | `f`, type a query, `enter` |
| `03-preview.png` | `F3` with the cursor on an evicted file |
| `04-file-menu.png` | `m` |
| `05-get-info.png` | `I` on a file |
| `06-help.png` | `?` |
| `07-theme-dracula.png` | set `[theme] name = "dracula"` in `~/.config/mytermtui/config.toml`, relaunch |
| `08-dual-panel.png` | `→` on a folder, then `j` |

4. Rename the captures exactly as above and move them into
   `screenshots/` — the README links expect those names.
