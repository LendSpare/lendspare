# LendSpare ⇄ provider protocol (v2)

The contract between the LendSpare application and a provider's control plane (`https://lendspare.com`,
`https://share.brainybrain.com`). Both sides are built and tested against this file; change it first.
Requirements it implements: `tmp/LendSpare_TZ_and_Terms_Drafts.md` (P0). It is a proprietary pairing with the
security properties listed at the end, not an implementation of RFC 8628.

## Words

- **Provider origin**: scheme + host + port of a control plane. LendSpare talks only to origins on its allow-list
  (the two production origins; development origins only with an explicit `--dev-origin` flag). Every request of a
  connection goes to that connection's origin and to no other; redirects to another origin are refused; a bearer
  token is never sent anywhere else.
- **Site**: where the person has an account and accepts terms (`lendspare.com`, `ai.brainybrain.com`, …). A site may
  be the provider origin itself, as lendspare.com is. It signs the person in, records the acceptance of terms and creates an intent.
- **Device**: one installation of LendSpare: a random UUIDv4 and an Ed25519 key pair made on first start. The UUID
  identifies, the key authenticates.
- **Connection**: (device, provider, account, service, scopes) with its own credential. A device may hold several;
  pairing adds or updates one and never replaces the list; removing one leaves the others untouched.
- **Service**: `ai` | `mount` | `sync`. **Scopes**: `compute:offer`, `files:serve` (more later). Everything is off
  until a scope is granted to a connection and the resource is switched on locally.

## 1. Provider description (public)

`GET /.well-known/share-provider` →

```json
{"api": 2, "provider": "lendspare", "operator": "…legal name…", "origin": "https://lendspare.com",
 "services": ["ai", "mount"], "terms": {"version": "2026-11-23", "url": "https://lendspare.com/terms"}}
```

LendSpare shows `operator` and the origin's host in its local confirmation. `origin` must equal the origin the file was
fetched from, or the provider is refused.

## 2. Connecting from a site (intent)

1. The signed-in person presses **Connect this computer** on the site and accepts the current terms there. The
   provider creates an **intent**: an opaque one-time nonce (≥ 128 bits), a short **code** (8 characters of
   `ABCDEFGHJKLMNPQRSTUVWXYZ23456789`, shown as `XXXX-XXXX`), ten minutes to live, bound server-side to
   provider, account, service, scopes and the accepted terms version. `POST /api/v2/share/intents` (site session,
   CSRF) → `{intent, code, expiresAt, connectUrl}`.
2. The browser goes to `connectUrl` = `<share origin>/connect?return=<ai|mount|sync>&i=<intent>`. `return` only
   selects wording; any other value is a 400; it is never a URL and never trusted. The page needs no session: it
   shows the operator, the service, the requested scopes, the installer downloads, an **Open LendSpare** button and
   the code.
3. **Open LendSpare** is the deep link `lendspare://connect?origin=<host[:port]>&intent=<intent>`. Nothing else may be in
   it: no token, no path, no scope. Without a working deep link the person opens LendSpare's local page, chooses
   the provider and types the code.
4. LendSpare checks the origin against its allow-list, fetches §1, and shows on the device: operator, host, service
   and scopes (read from step 5's answer), with **Allow connection** / **Cancel**. Nothing is sent before Allow
   except the two reads below: the description (§1) and `initialize` (step 5), which only binds the intent to this
   device and creates no credential. LendSpare sends nothing else, and `complete` only after **Allow connection**.
5. `POST /api/v2/share/devices/initialize` `{intent | code, deviceId, publicKey, name, platform}` →
   `{challenge, expiresAt, provider, service, scopes, account, terms: {version, url}, code?}`. `publicKey` is the
   standard base64 of the raw 32-byte Ed25519 key; `code`, when present, is the code the website shows (`XXXX-XXXX`),
   which LendSpare displays so the person can compare it with the page - a hijacked link then shows a code that is not
   on the page. A typed code is sent normalised: upper case, no hyphen or spaces (`K7QM2XWD`). `account` is a label the
   person recognises (masked e-mail). The intent stays usable only by this device from now on. Unknown, expired
   or used intent: `404 intent_invalid` (one answer for all three). A wrong code counts against the intent; five
   wrong codes from one address in ten minutes: `429`.
6. After Allow: `POST /api/v2/share/pair/complete` `{intent | code, deviceId, signature}` where `signature` is
   Ed25519 over `"share-pair-v2\n" + challenge + "\n" + origin` (UTF-8; `origin` is the normalised provider origin,
   for example `https://lendspare.com`: no trailing slash, default port omitted), standard base64. →
   `{connectionId, token, workerId, service, scopes, account}`. The intent is consumed in the same statement that
   returns the token; a second call is `404 intent_invalid`. Cancel: `POST /api/v2/share/intents/decline`
   `{intent | code, deviceId}` → 204.
7. The site's page polls `GET /api/v2/share/intents/{intent}` → `{state: pending|claimed|connected|declined|expired}`
   and shows **Connected** only for `connected`.

## 3. Connecting from LendSpare (no site first)

LendSpare's **Connect** lists the allow-listed providers. Choosing one starts the device-code flow that exists today
(`POST /api/v1/mesh/pair` → code, the person opens `<share origin>/mesh/pair?code=…`, signs in, accepts the terms
there, presses Allow; `POST /api/v1/mesh/pair/poll` collects the token once), with two additions: the start call
also carries `deviceId` and `publicKey`, and the poll's answer also carries `connectionId`, `service`, `scopes`,
`account`. A provider that predates v2 answers without them; LendSpare then records the connection with the scopes
`compute:offer` and `files:serve` that such a server always implied, and says so in its log.

## 4. Using a connection

- `Authorization: Bearer <token>` on the connection's origin only. The work API is unchanged and lives under the
  same origin: `/api/v1/mesh/{pending,submit,heartbeat,capabilities,stats,convert,input,files/…}`.
- The server refuses work calls outside the connection's scopes: no `compute:offer` → no jobs are leased and
  `capabilities` reports are ignored; no `files:serve` → the files mailbox is closed (`403 scope_missing`).
- LendSpare sends capabilities and folder names only to a connection whose scopes cover them, and only while the
  resource is switched on locally.
- `GET /api/v2/share/connection` (bearer) → `{connectionId, service, scopes, account, state, terms}`. `401`/`403`
  means revoked: LendSpare marks the connection **Disconnected**, stops its worker and keeps the others running.
- `DELETE /api/v2/share/connection` (bearer): LendSpare disconnects itself; the token is dead on return.
- Site side (session): `GET /api/v2/share/connections`, `DELETE /api/v2/share/connections/{id}`.
- `GET /api/v2/share/terms/current?service=` (public) → `{version, url, hash}`. Information only; acceptance is
  recorded on the site, never inferred from installing LendSpare.

## 5. One machine, several connections

Each connection has its own worker loop and credential. The device has one GPU: LendSpare runs at most one compute
job at a time across all connections (a single local scheduler, round-robin between connections that have work),
so two providers cannot overcommit the VRAM. Pausing or removing a connection never touches another's token,
settings, folder permissions or history.

## 6. What LendSpare stores

`config.json` (schema 2, `"schemaVersion": 2`): `device {id, publicKey}` and `connections[] {id, provider, operator,
origin, service, account, scopes, workerId, state, name, paused, compute, legacy, added}`. `state` is `connected` or
`disconnected`; `compute` is the local switch (off for every new connection); `paused` stops one connection's workers;
`legacy` marks a connection migrated from schema 1, which alone may keep a legacy origin (`https://ai.artur.work`). Private key and tokens go to the OS key store when one is available (Windows Credential
Manager, macOS Keychain, Linux Secret Service) and otherwise to a 0600 file beside the config; never to a log,
stdout, an installer or a URL. A schema-1 file (one `server` + `token`) is migrated once, with a backup copy (`backup-v1/`), to one
connection with exactly the rights it had (`compute:offer`, `files:serve`, compute on); `files.json` folders are granted
to that connection only. Nothing new is switched on by the migration.

## Security properties both sides must keep

1. A token is issued only to the device that proved possession of the key registered for that intent.
2. An intent is single-use, short-lived and bound to account, service and scopes on the server; nothing in a URL
   or a deep link can widen it.
3. `return` and `origin` parameters select from fixed lists; neither is ever used as a redirect target.
4. The person confirms on the device, seeing the operator and the scopes, before any credential exists.
5. Installing or starting LendSpare contacts no provider and shares nothing.
6. Revocation works from the site and from LendSpare, and kills the token on the server.

## Notes for implementers (from the LendSpare client)

- Scopes LendSpare does not know are ignored (logged by name, never acted on). A provider that adds one needs no client
  release for the existing two to keep working.
- The device-code flow (§3) cannot show scopes before the person presses Allow on the provider's page, because the
  start call carries none. LendSpare's local confirmation therefore names the provider and says the scopes are "as granted
  on its website"; after pairing the connection's real scopes are listed and nothing is on until switched on locally.
- The work API paths keep their historical names (`/api/v1/mesh/...`); the job container contract
  (`MESH_KIND`, `MESH_JOB`, `MESH_TIME_LIMIT_SEC`, `MESH_JUNIT`) and the shared-folder seal labels
  (`mesh-files v1`, the ticket label, the direct path `/mesh-files/v1`) are wire constants and are unchanged.
- Direct-transfer tickets (`ticket` in the direct path) are verified against each connection's token and worker id in
  turn; the one that verifies decides which connection's folder grants apply.
