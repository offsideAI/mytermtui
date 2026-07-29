# Demos

Scripted tours of the apps, generated headlessly (no PTY, no live app)
from the real Bubble Tea models — the same technique as the screenshots.

- **`myterm-demo.gif`** / `.mp4` — the explorer-style file browser:
  expanding the tree in place, the detail panel following the cursor,
  **dual independent panels** (`→` opens a folder in its own panel),
  filtering, menus, sort, Get Info, and the keyboard-reference overlay.
- **`myconsole-demo.gif`** / `.mp4` — the navigator-style file browser:
  the same in-place tree and detail panel, live filtering, the menu bar,
  sort, the keyboard-reference overlay, and the embedded vim-style editor.
- **`mydb-demo.gif`** / `.mp4` — the database admin TUI: connecting a
  SQLite database and browsing its schema, the read-only data grid,
  running SQL, switching to PostgreSQL (one active connection, the
  discovered-database Annex, cluster Roles), the common-commands
  templates, maintenance, and a backup.

Each app has a `cmd/film` storyboard harness that drives its model and
writes a true-color ANSI frame per beat; `scripts/ansi2png.py` renders
the frames to PNGs; `ffmpeg` assembles the GIF and MP4.

## Regenerate

`render()` (shared by both) turns a directory of PNG frames into a GIF + MP4:

```sh
render() {   # $1=out-name  $2=gif-width  $3=png-dir
  local FPS=10
  ffmpeg -y -framerate $FPS -i "$3/f%04d.png" -c:v libx264 -pix_fmt yuv420p \
    -vf "scale=trunc(iw/2)*2:trunc(ih/2)*2" "_demo/$1.mp4"
  ffmpeg -y -i "_demo/$1.mp4" \
    -vf "fps=$FPS,scale=$2:-1:flags=lanczos,palettegen=stats_mode=diff" /tmp/pal.png
  ffmpeg -y -i "_demo/$1.mp4" -i /tmp/pal.png \
    -lavfi "fps=$FPS,scale=$2:-1:flags=lanczos[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=3" \
    "_demo/$1.gif"
}
```

### myterm

```sh
# from the repo root
cp -r _demo/sample /tmp/acme                     # short breadcrumb path
go -C myterm-src run ./cmd/film -dir /tmp/acme -out "$PWD/_demo/frames"
ANSI2PNG_TITLE="myterm — file browser" \
  python3 scripts/ansi2png.py _demo/frames _demo/png myterm
render myterm-demo 900 _demo/png
rm -rf _demo/frames _demo/png /tmp/acme
```

### myconsole

```sh
# from the repo root
cp -r _demo/sample /tmp/acme                     # short breadcrumb path
go -C myconsole-src run ./cmd/film -dir /tmp/acme -out "$PWD/_demo/frames"
ANSI2PNG_TITLE="myconsole — file browser" \
  python3 scripts/ansi2png.py _demo/frames _demo/png myconsole
render myconsole-demo 900 _demo/png
rm -rf _demo/frames _demo/png /tmp/acme
```

### mydb

```sh
# from the repo root (uses a temp SQLite fixture + the fake Postgres driver)
go -C mydb-src run ./cmd/film -out "$PWD/_demo/mydb-frames"
ANSI2PNG_TITLE="mydb — database admin" \
  python3 scripts/ansi2png.py _demo/mydb-frames _demo/mydb-png mydb
render mydb-demo 1000 _demo/mydb-png
rm -rf _demo/mydb-frames _demo/mydb-png
```

- `sample/` — the small project tree the myterm and myconsole tours browse.
- `../myterm-src/cmd/film/`, `../myconsole-src/cmd/film/`, and `../mydb-src/cmd/film/` — the storyboard harnesses.
- `scripts/ansi2png.py` honors `ANSI2PNG_TITLE` to keep the window title
  constant across frames (so it doesn't flicker); unset for the normal
  per-scene screenshots.
