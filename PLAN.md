# Plan

  What you have now:

  mytermtui/                     (repo name unchanged)
  ├── myterm-src/      → myterm 0.1.0    — explorer style: enter expands the tree
  │                                        in place (▸/▾), detail panel on by default
  ├── myconsole-src/   → myconsole 0.1.0 — the original navigator: enter steps into
  │                                        folders (restored from the last commit)
  ├── screenshots/               myterm's 8 shots (+ screenshots/myconsole/ set)
  ├── scripts/ansi2png.py        shared; now takes an app-name for the window title
  └── README / USAGE / DEPLOY / GETSTARTED / SPEC — all rewritten for two apps

  Isolation details worth knowing: each app is a fully independent Go module (github.com/offsideai/myterm / …/myconsole) with its own binary name, menu-bar/help branding, and — importantly —
  its own config and state (~/.config/myterm/, ~/.local/state/myterm/, likewise for myconsole), so their download queues can't clobber each other and you can run both simultaneously. Your
  existing ~/.config/mytermtui/config.toml is now orphaned — copy it into one or both new paths if you had customizations. Both apps pass their full test suites and go vet, and print their
  proper names for --version.

  Docs: README leads with a "Choosing an app" comparison table and both hero screenshots, then shared reference sections with per-app differences flagged inline; USAGE gained a "The tree and
  detail panel (myterm)" section; DEPLOY covers building/installing both; GETSTARTED documents generating both screenshot sets; SPEC has a short historical note. The one nuance: show_preview
  is a myterm-only config key (myconsole is the pre-tree code), which the config docs call out.

  To test both:

  cd ~/repos/offsideai/githubrepos_workspace_active_1/mytermtui
  ./myterm-src/myterm .        # tree behavior + detail panel
  ./myconsole-src/myconsole .  # original behavior
