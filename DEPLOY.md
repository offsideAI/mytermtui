# Deploying myterm & myconsole

How to rebuild and install the binaries after pulling or making changes.

## Rebuild and install

```sh
cd ~/repos/offsideai/githubrepos_workspace_active_1/mytermtui

go -C myterm-src    build -o myterm .
go -C myconsole-src build -o myconsole .

install myterm-src/myterm myconsole-src/myconsole /opt/homebrew/bin/   # or your install location
```

Notes:

- Each app is its own Go module in its `-src` folder — run `go` commands
  from inside it, or use `go -C <dir>` as above.
- `install` copies the binary and marks it executable (`755`) in one
  step. `/opt/homebrew/bin` is on `PATH` and user-writable on Apple
  Silicon Macs; for `/usr/local/bin` you likely need `sudo install …`.
- A running app keeps executing the **old** binary — quit (`ctrl+q`)
  and relaunch to pick up the new one. In-flight downloads are
  unaffected (fileproviderd owns the transfers, and each app's queue
  resumes from its persisted state).

## Verify

```sh
which myterm myconsole
myterm --version && myconsole --version
```

Before deploying a change, run the checks:

```sh
(cd myterm-src    && go test ./... && go vet ./...)
(cd myconsole-src && go test ./... && go vet ./...)
```

## Alternatives

- `go install` from a `-src` folder places that binary in `~/go/bin`
  (add it to `PATH` once) without touching system directories.
- Uninstall: `rm /opt/homebrew/bin/myterm /opt/homebrew/bin/myconsole`.
