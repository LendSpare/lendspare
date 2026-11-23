# Site Storage and Backup Schedule

> **Draft 0.2, 2026-11-12. Working draft for lawyer review. Not legal advice, not a published document, and not in force.**
> Square-bracketed text is unknown and must be filled in or removed by the lawyer.
> **Operator scope:** drafted for the artur.work Operator only (services `share.artur.work` and the planned `sync.artur.work`). The BrainyBrain Inc. service described in the working specification offers compute only; no storage schedule is drafted for it. If it later offers storage, this schedule would be re-issued for it separately.

This schedule applies in addition to the LendSpare Connected Services Terms and is accepted on the Operator's site before a folder is made available. Installing LendSpare, or accepting the software licence, is not acceptance of it. Defined terms (LendSpare, Device, Connection, Service, Resource, Operator) are as in those Terms.

## 1. What this covers

1.1 **Site storage.** You let a website you control (a "site") read, and with separate approval write, delete or replace files in a folder on your Device, so the site can use your computer as storage.

1.2 **Backup to a folder.** You let a site write copies of its data (backups, exports) to a folder on your Device.

1.3 Part 9 describes an **accounting-backup service that is planned and not offered**.

## 2. Only folders you choose, on the Device

2.1 You choose each folder on the Device itself. A folder path is never accepted from a website or from the Operator. Nothing in your computer is offered merely because LendSpare can see it.

2.2 LendSpare can show a folder's list and set its rights locally without any account. Connecting a site adds no folder; a Connection to the Operator is needed only to relay requests.

2.3 Nothing outside a chosen folder is reachable. LendSpare keeps requests inside the folder and refuses attempts to escape it by relative paths, links or reserved names. Do not put into a chosen folder anything you do not want a permitted site to see.

## 3. Permissions: read-only by default

3.1 **Default.** A folder starts **read-only**, with **no write, no delete and no overwrite**, and with a quota.

3.2 **Separate approvals.** Each of these is approved separately, per folder, on the Device: writing new files; replacing existing files; deleting files. For backups, a destination folder needs the write permission and a policy for how many versions are kept [DEFAULT VERSION POLICY].

3.3 **Per site.** A permission is granted to one named site and its key, not to the Operator's whole platform. A second site does not see the folder unless you grant it separately. A folder granted to one site is not available to compute jobs.

3.4 **Quotas.** Each folder has a size quota and a limit on transfer [DEFAULT QUOTAS]. When a quota is reached, writes stop; nothing is deleted to make room unless you permitted deletion and a policy says so.

3.5 Permissions and grants live only on the Device, in LendSpare's local settings. The Operator cannot grant itself or a site access to a folder, and cannot change your permissions from its servers.

## 4. What the Operator does and does not do

4.1 **The Operator relays sealed data and cannot read it.** Requests and file content between a site and your Device are sealed so that the Operator's servers pass them along without being able to read them. The Operator keeps only what it needs to forward them (for example the latest answer of a session and short-lived chunks), and sweeps idle sessions after a short time [CONFIRM TIMES].

4.2 The Operator does not decide which folder or site may be reached; that decision is made on your Device.

4.3 A site may reach your Device by other routes that you enable or that the site names (for example a direct connection on your local network, or a relay you operate). The Operator is not responsible for those routes, and a route named inside the sealed request cannot be redirected by the Operator.

## 5. No guarantee that a home computer is available

5.1 Your Device may be off, asleep, offline, slow, moved, full or damaged. **Neither the Operator nor the site can promise that your files will be available, durable or restorable.** The Operator gives no availability, durability or backup guarantee for data kept on your Device. [LAWYER: if a specifically purchased service guarantees otherwise, that service's own terms say so.]

5.2 **You keep independent copies** of everything you care about, unless a service you specifically bought expressly guarantees otherwise. A copy on your Device, or on a site that depends on it, is not your only copy.

## 6. Revocation

6.1 You can remove a site's access at any time on the Device (folder, site or Connection), or remove the Connection in LendSpare or on the Operator's site. The Operator's credentials for the Connection are then invalid.

6.2 Revocation stops new operations; an operation already running ends within [MAXIMUM TERMINATION TIME]. Files already copied to a site stay with that site, and your arrangement with the site governs them, not this schedule.

## 7. Logs

LendSpare keeps a visible log of operations on the Device (which site, which folder, which operation, when). The log stays on the Device and is kept for [DEVICE LOG RETENTION]. The Operator's servers keep only the technical records described in the Privacy Notice and do not see file names or content.

## 8. Your responsibilities

You are responsible for what you place in a shared folder, for having the right to make it available, and for the permissions you set. Do not share folders holding other people's personal data without the right to do so. Do not use a shared folder for anything unlawful. Keep your Device updated and its operating system account secure.

## 9. Planned: accounting-backup service (NOT YET OFFERED)

> **This section describes a service that does not exist yet. It must not be shown to users or relied on until it is built, reviewed and announced.** It is a placeholder for what the lawyer and the product owner must settle first.

If offered (planned for `sync.artur.work`), an accounting backup would write backups of an organisation's accounting data to a folder you choose. Open points:

- **Encryption.** Backups would be encrypted before leaving the site, with keys the Device holder does not hold [CONFIRM DESIGN].
- **Retention.** Backup versions kept for [BACKUP RETENTION], then removed under a stated policy.
- **Restore testing.** A restore check run at least [RESTORE-TEST FREQUENCY], with its result recorded.
- **Audit log** of writes, reads and deletions, with data of different organisations kept apart.
- **Separation.** Accounting data would never be mixed with public file sharing or with compute inputs, and a folder granted for it would never be available to compute jobs.
- **Legal role.** Whether the Device holder acts as a processor, what a data-processing agreement must say, and what an accountant or tax authority needs [LAWYER].
- **Guarantees.** Whether any durability promise is made, and its limits. Until decided, section 5 applies.

## 10. Liability and mandatory rights

[LIABILITY PLACEHOLDER. LAWYER: loss of data, loss through an unavailable Device, loss through a site's own misuse, and a site's data placed on a consumer's computer. Do not add a blanket exclusion, and do not remove any right that cannot be removed under the law of the country where you live.]

## 11. Personal data

The Privacy Notice applies. Files in a shared folder may contain personal data; the Operator cannot read them (section 4.1).
