# LendSpare: local GUI permission texts

> **Draft 0.3, 2026-11-18. Working draft for lawyer review. Not legal advice.**
> Developed from Template C of `tmp/LendSpare_TZ_and_Terms_Drafts.md`. Each string has an `id` for the developers. Placeholders in `{braces}` are filled at run time from the provider description and the pairing answer. Text must match the protocol in `docs/PROTOCOL.md`. LendSpare shows only the connected operator's name; no other brand appears in the interface.

## Run-time values

| Value | Source |
|---|---|
| `{host}` | the origin's host, for example the provider's `share.` host |
| `{operator}` | `operator` from `/.well-known/share-provider` |
| `{service}` | the service label in the pairing answer |
| `{account}` | masked account label from the pairing answer |
| `{site}` | the site where the person accepted the terms |
| `{terms_url}` | `terms.url` from the provider description |
| `{folder}` | the folder the person chose on this computer |
| `{site_name}` | the site named in a folder grant |
| `{code}` | the typed or shown pairing code |

## 1. First run (no connection)

| id | String |
|---|---|
| `first_run.title` | LendSpare is ready |
| `first_run.body` | LendSpare is running on this computer. It has not connected to any service, and nothing on this computer is shared. |
| `first_run.connect_hint` | To use it with a service, connect from the website where you use LendSpare. |
| `first_run.folders_hint` | You can set up shared folders here without an account. |
| `first_run.button_folders` | Shared folders |
| `first_run.button_connect` | Connect |

## 2. Connect: confirmation (all kinds)

| id | String |
|---|---|
| `connect.title` | Connect to {host} |
| `connect.intro` | {service}, operated by {operator}, asks permission to use this computer. Account: {account}. Installing LendSpare alone never shares your resources. |
| `connect.terms_note` | You accepted the terms on {site}. You can read them at {terms_url}. |
| `connect.review` | Review permissions |
| `connect.allow` | Allow connection |
| `connect.cancel` | Cancel |
| `connect.later` | You can change or remove this connection at any time in LendSpare. |

## 3. Connect: compute

| id | String |
|---|---|
| `connect.compute.summary` | Offer computing capacity of this computer. |
| `connect.compute.body` | Allow computing tasks from customers of the connected service and permitted partner platforms, under the terms you accepted on {site}. The customer behind each task is not shown to you. A task cannot see your files, passwords or network. |
| `connect.compute.off_note` | Computing stays off until you switch it on in LendSpare, and you can set limits there. |
| `connect.compute.own_use` | Your own use comes first: LendSpare steps aside while another program uses the GPU. Cryptocurrency mining is never permitted. |
| `connect.compute.costs` | Running tasks uses power and may heat and wear your hardware. Earnings are credit in the service, not money. |

## 4. Connect: site storage

| id | String |
|---|---|
| `connect.storage.summary` | Let {site_name} use a folder on this computer. |
| `connect.storage.body` | This service will reach only the folder and operations listed below. Writing, deleting and replacing files each need your separate approval. |
| `connect.storage.default` | By default the folder is read-only. |
| `connect.storage.relay` | The service passes your data through sealed. It cannot read your files. |
| `connect.storage.copies` | Your computer may be off or offline. Keep your own copies of anything important. |
| `connect.storage.choose` | Choose folder |
| `connect.storage.folder_line` | Folder: {folder}. Rights: read only. |

## 5. Connect: typed code

| id | String |
|---|---|
| `code.title` | Connect with a code |
| `code.provider_label` | Service provider |
| `code.prompt` | Enter the code shown on the website. |
| `code.placeholder` | XXXX-XXXX |
| `code.submit` | Continue |
| `code.next` | Check the details on the next screen before you allow anything. |
| `code.wrong` | That code is not right. Check it and try again. |
| `code.too_many` | Too many wrong codes. Wait ten minutes and try again. |

## 6. Connected state

| id | String |
|---|---|
| `connected.title` | Connected to {host} |
| `connected.line` | {service}, account {account}. |
| `connected.pause` | Pause |
| `connected.resume` | Resume |
| `connected.disconnect` | Disconnect |

## 7. Pause

| id | String |
|---|---|
| `pause.title` | Pause sharing? |
| `pause.body` | No new tasks or requests will be accepted from {host}. A task that is already running stops shortly. |
| `pause.all_option` | Pause all connections |
| `pause.confirm` | Pause |
| `pause.cancel` | Cancel |
| `pause.status` | Paused |

## 8. Disconnect

| id | String |
|---|---|
| `disconnect.title` | Disconnect from {host}? |
| `disconnect.body` | This removes the connection and its permissions. Other connections are not affected. |
| `disconnect.server_note` | {operator} will no longer accept this computer's credentials. Records the service must keep stay with it. |
| `disconnect.folders_note` | Folders you set up stay on this computer and are no longer reachable by {site_name}. |
| `disconnect.confirm` | Disconnect |
| `disconnect.cancel` | Cancel |
| `disconnect.done` | Disconnected from {host}. |
| `disconnect.revoked_remote` | {host} removed this connection. It is now disconnected here. |

## 9. Errors

| id | String |
|---|---|
| `error.origin_refused.title` | Cannot connect |
| `error.origin_refused.body` | This request points to an address LendSpare does not accept, so nothing was sent. Start again from the website you use, or check that LendSpare is up to date. |
| `error.expired.title` | This request has expired |
| `error.expired.body` | Connection requests last ten minutes and work once. Go back to the website and press Connect this computer again. |
| `error.used.body` | This request has already been used. If you did not use it, remove the connection in LendSpare and start again. |
| `error.unreachable.body` | LendSpare cannot reach {host} right now. Nothing was changed. Shared folders still work on this computer. |
| `error.terms_changed.body` | {operator} has new terms. Accept them on {site} to keep this connection working. |
| `error.generic.body` | Something went wrong and nothing was shared. Try again, or start from the website. |
| `error.links_off.body` | lendspare:// links are turned off in LendSpare's settings |
| `error.button.retry` | Try again |
| `error.button.close` | Close |

## 10. Microsoft Store (packaged) build

In the Store package, Windows owns start-at-login (Settings > Apps > Startup) and the lendspare:// link, so these strings replace the usual ones.

| id | String |
|---|---|
| `tray.autostart_packaged` | Start with Windows... |
| `settings.autostart_packaged` | Start when I log in: Windows Settings |

`error.links_off.body` (section 9) is shown only by the packaged build, when the person turned lendspare:// links off: a package cannot unregister its link, so LendSpare refuses the link itself.

## 11. Notes for developers and the lawyer

- Never show a bearer token, local path or intent value in any string above.
- A one-screen confirmation (`connect.title` to `connect.allow`) is enough if the review content is on the same screen; the two-button flow of Template C (`connect.review` then `connect.allow`) is the alternative. Either way **Allow connection** is the only action that creates a credential.
- `connect.compute.own_use` states the rule of Compute Participation Terms sections 2.4 and 3.6, which the Store listing also promises. Keep the three texts identical in meaning.
- `connect.compute.body` mentions "permitted partner platforms". Show that clause **only** when the operator's terms permit partner jobs; otherwise use a variant without it (`connect.compute.body_no_partners`: Allow computing tasks from customers of the connected service, under the terms you accepted on {site}. The customer behind each task is not shown to you. A task cannot see your files, passwords or network.).
- A device connected to two providers shows both connections in the list; never hide one.
- Language: English only for now. Translation after the lawyer approves the English.
