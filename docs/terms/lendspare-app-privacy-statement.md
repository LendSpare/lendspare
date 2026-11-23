# LendSpare app privacy statement

> **Draft 0.1, 2026-11-18. Working draft for lawyer review. Not legal advice, not published, not in force.**
> Operator-neutral statement about the **LendSpare application itself**, for the privacy-policy link that app stores (Microsoft Store) require. It names no operator: what a connected service does with data is in that service's own privacy notice (`privacy-notice-outline.md`). Square-bracketed text must be filled in or removed.

**Publisher of LendSpare:** [STORE PUBLISHER LEGAL NAME], [ADDRESS], [PRIVACY CONTACT EMAIL] (see README section 6).
**Effective date:** [DATE]

## 1. In short

Installing and starting LendSpare sends nothing to anyone. LendSpare has no advertising, no analytics and no telemetry. It talks to a service only after you connect that service on this computer, and then only to that service.

## 2. What LendSpare keeps on your computer

All of this stays on your computer, in LendSpare's own folder (on Windows under your user profile's application data; in the Microsoft Store version, in the package's private storage):

- an identity for this installation: a random identifier and a key pair (the private key is kept in the operating system's key store where possible, otherwise in a file only your user account can read);
- your settings, the connections you made and the folders you chose to share, with their rules;
- for each connection, its access token (kept like the private key);
- a list of what connected sites did in your shared folders, and a log of what LendSpare did, for you to read.

LendSpare's settings page runs on your computer only (`127.0.0.1`) and cannot be opened from another computer. You can delete all of the above by removing LendSpare and its folder.

## 3. What LendSpare sends, and only after you connect

When you connect a service, LendSpare shows you who operates it, its address and what it asks for, and connects only if you allow it. From then on LendSpare sends that service, and only that service:

- this installation's identifier and public key, a device name and the operating system;
- if you switch computing on for that connection: what this computer can run (kinds of work, GPU model and memory, installed local models, whether containers are available);
- if you share folders with that connection: the names and quotas of those folders, and file content only in sealed form that the service cannot read.

LendSpare talks only to services on its built-in list, shown in its settings. Two further kinds of traffic follow from what you switch on: with computing on, your container engine downloads the container images a task needs from their registries, which see your computer's internet address; with direct folder access on, LendSpare accepts connections for your shared folders on your local network. What a service does with this data is described in **that service's privacy notice**, shown when you connect.

## 4. Work run on your computer

If you switch computing on, tasks from the connected service run inside a model server or container on your computer. Their content is processed on your computer only for as long as the task runs. A task cannot reach your files, passwords or network beyond what the terms of the service allow.

## 5. App stores

If you installed LendSpare from the Microsoft Store, Microsoft processes data about the download, installation, updates and crashes under the Microsoft Privacy Statement. As publisher, we receive from Microsoft only the reports the Store provides to publishers, such as installation counts and app health reports [PRODUCT/LAWYER: confirm which reports are used], and we do not link them to any person or connection.

## 6. Children

LendSpare is not directed at children. Connecting a service requires an account with that service, which sets its own minimum age.

## 7. Changes and contact

We will update this statement when LendSpare starts handling data differently, and change its date. Questions: [PRIVACY CONTACT EMAIL].
