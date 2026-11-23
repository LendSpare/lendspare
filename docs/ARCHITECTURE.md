# Architecture

One Go module (`github.com/LendSpare/lendspare`), standard library only.

## Layout

| Path | Contents |
|---|---|
| `cmd/lendspare` | `main`: calls `app.Main` |
| `internal/app` | the program: daemon, connections, pairing, shared folders, compute kinds, settings page, tray |
| `internal/secrets` | key stores: OS store (Credential Manager, Keychain, Secret Service) layered over a private file |
| `internal/engine` | the container CLI (docker or podman), GPU probes |
| `internal/egress` | the allow-list proxy for pipeline containers |
| `internal/proc` | starts helper programs without a console window |
| `scripts/build.sh` | release binaries with checksums (`make dist`) |

## Files in `internal/app`

| Area | Files |
|---|---|
| Entry, flags, compute loop | `main.go` (`Main`, flags, `daemon`, `step`, capability reports), `scheduler.go`, `cli.go` (`connect`, `connections`, `disconnect`), `app.go` (`app`, link hand-off) |
| Identity and config | `config.go` (schema 2, device key, v1 migration); secrets in `internal/secrets` |
| Connections | `connections.go` (`manager`: add/update/remove/pause, one `apiClient` per (connection, origin)), `origins.go` (allow-list), `api.go` (bearer client, same-origin redirects) |
| Pairing | `connect.go` (provider description, intent / code / device-code flows, confirmation text, deep-link parsing) |
| Front ends | `settings.go`, `settings_connections.go`, `settings.html` (loopback page), `tray_windows.go`, `tray_darwin.go|.m|.h`, `tray_menu.go` (shared by both trays), `dialog_*.go`, `autostart_*.go`, `link_*.go` (lendspare:// registration) |
| Shared folders | `files_*.go` (store and grants, sealed protocol, relay, direct, mailbox, quota, activity) |
| Compute | `comfy*.go`, `llm.go`, `container.go`, `curated_image.go`; engine and egress proxy in `internal/` |

## One device, many connections

`manager` owns `config.json`. A `connection` is (provider origin, account label, service, scopes, token) plus local
switches: `paused` (stops that connection's workers), `compute` (the local computing switch, off for new connections)
and, in `files.json`, which folders are granted to it. The manager reloads the file when it changes on disk, so the
command line and a running tray agree.

Eligibility is computed, never stored:

- compute worker: `state=connected` and origin allowed and not paused and `compute` and scope `compute:offer`;
- files: `state=connected` and origin allowed and not paused and scope `files:serve`, and per folder: granted to it.

`reportAll` sends each connection only what it may know: compute facts with scope and switch, folder names for folders
granted to it. A connection with nothing to be told gets no request.

## The compute scheduler

There is one compute loop (`daemon.step`). Each pass gates on the GPU once (foreign use, free VRAM, container engine),
then asks the eligible connections for a job in round-robin order (`scheduler.order`: the one after the last served
goes first), runs the first job it gets **to its end**, and returns. So at most one compute job runs at a time across
all connections, and a busy connection cannot starve another. `d.api` is the client of the connection whose work is
running; it is written only by this loop. A 401/403 on a claim, report or files poll marks only that connection
disconnected (token deleted, record kept). File sessions are served concurrently (up to four) by a separate poller that
walks the files-eligible connections.

## Secrets

`secrets.Store` has Get/Set/Delete by name (`device-key`, `token-<connection id>`). `secrets.Open` returns a
`secrets.Layered`: the OS store when present, falling back to `secrets.json` (0600 in the 0700 config directory) if the
store is missing or refuses a write. Windows uses Credential Manager through advapi32 syscalls; macOS the `security`
tool (`-i`, secret on stdin, hex-encoded); Linux `secret-tool` when a session bus exists. The fallback is what tests use.
`files.key` (the shared-folder seal key) stays a private file beside the config, outside any folder that can be shared.

## Migration

`loadConfig` runs before anything reads `files.json`. If `config.json` is absent but the previous program's directory
exists, the known files are copied (not moved) into the new directory. A schema-1 `config.json` is backed up to
`backup-v1/`, becomes one connection (same scopes, computing as it was, legacy flag for `https://ai.artur.work`), and
`files.json` is stamped schema 2 with each folder granted to that connection only (also backed up). The stamp makes the
step idempotent. A migrated origin that is not allowed (a development address) is kept but does not run until
`--dev-origin` names it.

## Deep link and loopback server

`lendspare://connect?origin=<host>&intent=<nonce>` is parsed strictly (exactly those two parameters). Windows registers it
under HKCU, Linux through a `.desktop` file; macOS needs an app bundle and does not register it yet. The second process
posts the link to the running instance's loopback server with the token from the private `settings-url` file and exits;
if none runs, LendSpare starts and confirms the link once up. The handler creates a pending confirmation and nothing else.
