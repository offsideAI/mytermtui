# Deploying mytermtui

How to rebuild and install the binary after pulling or making changes.

## Rebuild and install

```sh
cd ~/repos/offsideai/githubrepos_workspace_active_1/mytermtui/mytermtui-src
go build -o mytermtui . && install mytermtui /opt/homebrew/bin/   # or your install location
```

Notes:

- The Go module lives in `mytermtui-src/` — build from there, not the
  repo root.
- `install` copies the binary and marks it executable (`755`) in one
  step. `/opt/homebrew/bin` is on `PATH` and user-writable on Apple
  Silicon Macs; for `/usr/local/bin` you likely need
  `sudo install mytermtui /usr/local/bin/`.
- Any running mytermtui keeps executing the **old** binary — quit
  (`ctrl+q`) and relaunch to pick up the new one. In-flight downloads
  are unaffected (fileproviderd owns the transfers, and the queue
  resumes from its persisted state).

## Verify

```sh
which mytermtui       # the path you installed to
mytermtui --version   # expected version
```

Before deploying a change, run the checks from the repo root:

```sh
cd mytermtui-src && go test ./... && go vet ./...
```

## Alternatives

- `go install` from `mytermtui-src/` places the binary in `~/go/bin`
  (add it to `PATH` once) without touching system directories.
- Uninstall: `rm /opt/homebrew/bin/mytermtui` (or wherever it lives).
