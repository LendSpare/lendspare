# Microsoft Store submission texts

> Draft, 2026-11-18. Paste-ready texts for Partner Center. Fill every `[BRACKET]` first. Keep them consistent with
> `docs/terms` (Compute Participation Terms 2.4, 2.5, 3.6; `lendspare-app-privacy-statement.md`) and with the app's own
> strings (`docs/terms/gui-permission-texts.md`, `connect.compute.own_use`). Check each field's character limit in
> Partner Center when pasting.

## Before submitting

- [ ] Store publisher decided and identity values copied into `scripts/msix.env` (`docs/terms/README.md` section 6).
- [ ] `lendspare-app-privacy-statement.md` published; its URL is the listing's privacy policy.
- [ ] Each operator's Customer-facing terms forbid cryptocurrency mining and the other excluded work (Compute
      Participation Terms 3.6), and pipeline/image review checks for it.
- [ ] MPL-2.0 entered as the app's licence terms.
- [ ] A reviewer test account exists on `[PROVIDER HOST]` and is allowed to connect a device.
- [x] Real logos in `packaging/msix/Assets` (from `brand/lendspare-256.png`, scaled by whole numbers without smoothing).
- [ ] `make msix` built with the real identity; the package installed and tested locally (`scripts/pack-msix.sh
      --register`, Developer Mode): tray, settings page, a `lendspare://` link, the `lendspare` command in a terminal, Startup apps.
- [ ] Windows App Certification Kit run on the package, no failures.
- [ ] Age rating questionnaire answered; support contact `[SUPPORT EMAIL]` set.

## 1. Notes for certification

```
LendSpare is a Win32 tray app (full trust) that lets the user offer two things from their own PC to services they connect: shared folders, and spare GPU/CPU capacity for AI tasks (image generation, local language models, sandboxed containers).

Nothing is shared and no network request is made until the user connects a service. Connecting requires an account on a supported service and an explicit confirmation on this PC.

How to test:
1. Start LendSpare from the Start menu. A tray icon appears. Right-click it and choose "Connections and folders..." to open the settings page (a local page on 127.0.0.1 in the browser).
2. Shared folders work without any account: add a folder on the settings page. It is read-only and offered to nobody until a service is connected and the folder is granted to it.
3. To connect: sign in at https://[PROVIDER HOST] with the test account [TEST ACCOUNT EMAIL] / [TEST ACCOUNT PASSWORD], press "Connect this computer", then press "Allow connection" in LendSpare. Alternatively use the code shown on the site in LendSpare's settings page (Connect).
4. Computing is off by default and is switched on per connection on the settings page. It needs Docker Desktop or Podman, and an NVIDIA GPU for image work. On a PC without them LendSpare shows "No Docker or Podman found" and runs no compute; this is expected, not an error.

Compute safeguards: off by default, per connection; at most one job at a time; LendSpare steps aside while the user uses the GPU; Pause in the tray menu. Containers run only from pinned image digests, without host mounts except a per-job folder, and reach the internet only through an allow-list proxy. Cryptocurrency mining is prohibited by the terms, never offered as a kind of work, and stated on the connection screen.

Start at login is the package's StartupTask (off by default, Settings > Apps > Startup). The lendspare:// protocol only opens a confirmation in LendSpare; it never changes settings by itself.
```

## 2. Restricted capability: runFullTrust

```
LendSpare is a classic Win32 desktop program packaged as MSIX. It needs full trust to: show a notification-area (tray) icon; start the container engine CLI the user installed (docker or podman) and nvidia-smi to run and monitor GPU work; serve its settings page on 127.0.0.1; and, only if the user enables it, accept connections on the local network for the folders the user shares. It does not install drivers or services and does not need administrator rights.
```

## 3. Store listing

**Short description**

```
Offer folders and spare GPU or CPU capacity from your PC to the services you choose. Nothing is shared until you connect and allow it.
```

**Description**

```
LendSpare lets you offer two things from your own computer to the services you choose:

- Folders, for the websites you trust. Each folder is read-only by default, with a quota, and offered to nobody until you grant it. Writing, deleting and replacing files are separate switches, and every action is listed in the activity log.
- Spare compute, for AI tasks such as image generation, local language models and sandboxed containers. Computing is off until you switch it on for a connection. LendSpare runs one task at a time and steps aside while you use the GPU yourself. Cryptocurrency mining is never permitted.

Installing LendSpare creates no account, contacts no service and shares nothing. A connection exists only after you confirm it on this computer, seeing who operates the service, its address and what it asks for. You can pause or remove any connection at any time.

LendSpare is open source (MPL-2.0).
```

**Additional system requirements** (or "Other requirements")

```
Shared folders work on any Windows 10 (1809) or Windows 11 PC. Sharing compute needs software you install yourself: Docker Desktop or Podman for container tasks, an NVIDIA GPU with current drivers for image generation, or a local model server such as Ollama or LM Studio for language models. LendSpare does not include or install them.
```

**Other fields**

| Field | Value |
|---|---|
| Privacy policy URL | `[URL OF lendspare-app-privacy-statement]` |
| Support contact | `[SUPPORT EMAIL]` |
| Website | `[PROJECT WEBSITE]` |
| Category | Utilities & tools |
| Licence terms | MPL-2.0, `[LICENSE URL]` |
