# LendSpare

LendSpare is a small program for Windows, macOS and Linux. It lets you offer two things from your own computer to
the services you choose: **folders** (for the websites you trust) and **spare compute** (your GPU or CPU, for
permitted tasks). It is open source (MPL-2.0): https://github.com/LendSpare/lendspare

Installing or starting LendSpare does not create an account, contact any service, or share anything.

## What it does with no connection

- Runs in the tray (Windows, macOS) or in the console, and says: **LendSpare is ready**.
- Sends nothing anywhere. It makes no network request to any service until you connect one.
- Lets you set up **shared folders** on its local settings page (`lendspare --settings`, or the tray menu). That works
  without any account; a folder is reachable by nobody until you connect a service and grant the folder to it.
- Creates this installation's identity (a random UUID and an Ed25519 key pair) on first start. It identifies the
  installation; it grants nothing.

## Connecting

You connect from the website where you use LendSpare, or from LendSpare itself. In both cases you confirm on this
computer, seeing who operates the service, its host, the service and what it asks for, before any credential exists.

- **From a website**: press *Connect this computer* there. The page offers **Open LendSpare** (a `lendspare://connect?...`
  link) or a short code. LendSpare shows the confirmation; **Allow connection** is the only action that creates a
  credential.
- **By code**: Settings page - Connect - choose the provider, type the code. Or `lendspare connect --provider <host> --code XXXX-XXXX`.
- **From LendSpare**: Settings page - Connect - choose the provider, leave the code empty; you approve on the provider's
  page. Or `lendspare connect`.

LendSpare talks only to providers on its allow-list: `lendspare.com` and `share.brainybrain.com`. A website never
names a provider's address; it can only select from that list. Development origins need `--dev-origin <origin>`.

A device can be connected to several providers at once. Each connection has its own token, scopes and switches, and
all of them are always listed. Removing or pausing one never touches another.

```
lendspare                      run (tray on a desktop)
lendspare connect [--provider <host>] [--code XXXX-XXXX]
lendspare connections          list connections (never prints a token)
lendspare disconnect <id>      ask the provider to end it, then delete its token
lendspare --settings           open the settings page
lendspare --version
```

## Folders

Choose a folder on the settings page (never from a website). By default it is read-only, with a quota, and offered
to nobody. For each folder you tick the connections it is offered through. A site must be approved by you, by its
key fingerprint, before it can read; writing, deleting and replacing are separate switches. Every file operation runs
inside the folder (`os.Root`): `..`, absolute paths and links pointing out are refused. Everything a site did is in
the activity list.

## Compute

A connection with the `compute:offer` scope can run work on this computer only when you switch computing on for that
connection (off by default). LendSpare runs **one job at a time** across all connections and asks them in turn, so two
providers cannot overcommit the GPU. While you use the GPU yourself, LendSpare steps aside. Kinds of work: ComfyUI image
workflows, local language models you already run (Ollama, LM Studio) and sandboxed containers (no host mounts but a
per-job directory, egress only through an allow-list proxy). Docker or Podman is needed for the container kinds.

## Security model

- A token is issued only to the device that proved possession of its key, after you confirmed on the device.
- A connection's token goes to its own origin and nowhere else; redirects to another origin are refused.
- Secrets (the device private key, tokens) live in the OS key store where LendSpare can use it (Windows Credential
  Manager, macOS Keychain via `security`, Linux Secret Service via `secret-tool`), otherwise in a `0600` file in a
  `0700` directory. `LENDSPARE_SECRETS=file` forces the file. They never appear in config.json, logs, URLs or the console.
- The settings page listens on `127.0.0.1` only, on a random port, behind a random token; the Host header must be
  that address and changes need JSON with a matching Origin.
- A `lendspare://` link can only open the confirmation. It never changes folders, files or settings.
- Folder permissions and site keys live only on this computer; the provider cannot add a folder or let a site in.
- `lendspare disconnect`, or *Disconnect* on the page, tells the provider and deletes that token. If the provider
  revokes you, the connection shows as disconnected and only its worker stops.

## Upgrading from the mesh-daemon

On first start LendSpare copies the old program's files into its own directory and migrates them once: the old
server/token pairing becomes one connection with exactly the rights it had (`compute:offer`, `files:serve`, computing
on), and its shared folders are granted to that connection only. Originals are kept under `backup-v1/` (and the old
directory is left untouched). A migrated connection to `https://ai.artur.work` is kept but no longer served there (the marketplace moved to
lendspare.com): connect the computer to lendspare.com instead. No new connection can be
made to that origin.

## Building

```
make build                   # Go 1.25+, writes bin/lendspare
make test                    # go test ./...  (make race: with the race detector)
make lint                    # golangci-lint v2, see .golangci.yml
make dist                    # release binaries for Linux (amd64, arm64), Windows, macOS (arm64, amd64) in dist/
GO_DOCKER=1 make dist        # in golang:1.25; the macOS tray needs a Mac (cgo)
make dist-signed             # same, Windows exe signed via Azure Artifact Signing (scripts/sign-windows.sh)
SIGN_CMD="signtool ..." make dist   # or any other signing command, run before the exe is hashed
make msix                    # Microsoft Store package in dist/ (scripts/pack-msix.sh, packaging/msix)
```

Code layout: `cmd/lendspare` is the entry point, `internal/` holds the packages (see
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)). Standard library only; no third-party module. Docker:
`docker compose up -d`, then `docker compose exec lendspare /lendspare connect`.

## Environment variables

`LENDSPARE_IMAGE`, `LENDSPARE_ENGINE`, `LENDSPARE_SELF_IMAGE`, `LENDSPARE_CONFIG_DIR`, `LENDSPARE_SECRETS`. The old `MESH_IMAGE`,
`MESH_ENGINE` and `MESH_SELF_IMAGE` are still read as fallbacks and are **deprecated**. `MESH_SERVER` and
`MESH_TOKEN` (and the `--server`, `--token`, `--pair`, `--forget` flags) are gone: LendSpare has no default server and
takes no token from the environment; it prints a message saying so.

The protocol with providers is in [docs/PROTOCOL.md](docs/PROTOCOL.md); the code layout in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
