# LendSpare: draft legal documents, index for the lawyer

> **Draft 0.3, 2026-11-18. Working drafts for lawyer review. Not legal advice, not published, not in force.**
> Source: `tmp/LendSpare_TZ_and_Terms_Drafts.md` (sections 1, 2, 3, 8, 9, Templates A, B, C), `docs/PROTOCOL.md`, and the facts of the existing service as recorded in its project notes and legal pages. Where a fact was not known it is a `[BRACKETED PLACEHOLDER]`.

## 1. The documents

| File | What it is | Per operator? |
|---|---|---|
| `artur-work-lendspare-terms.md` | LendSpare Connected Services Terms for the Slovak sole-trader operator (`ai.artur.work`, `share.artur.work`; `sync.artur.work` planned) | artur.work only |
| `brainybrain-lendspare-terms.md` | LendSpare Compute Connection Terms for BrainyBrain Inc. (`ai.brainybrain.com`, `share.brainybrain.com`) | BrainyBrain only |
| `compute-participation-terms.md` | Rules for offering computing capacity: opt-in, job classes, containers, quality checks, gallery, pricing, credit-only compensation, prohibited conduct. Common body plus Annex A (artur.work) and Annex B (BrainyBrain) | Body shared; one annex each |
| `storage-backup-schedule.md` | Site storage and backup via shared folders; planned accounting backup (not offered) | artur.work only (BrainyBrain offers no storage in the spec) |
| `privacy-notice-outline.md` | Structure of the GDPR Art. 13 notice with known facts filled in and open questions | One notice per operator |
| `gui-permission-texts.md` | The exact strings of the local LendSpare app, with ids | Neutral; run-time values name the operator |
| `lendspare-app-privacy-statement.md` | Privacy statement of the LendSpare app itself (what it keeps locally, what it sends after a connection), for app-store listings | Neutral; names no operator |

## 2. How the documents relate

1. **Software licence (MPL-2.0 [CONFIRM], the repository's `LICENSE`).** Governs the source code and the binary. Installing or running LendSpare needs nothing else. It is never evidence that anyone agreed to anything below.
2. **Operator terms** (the two Terms files). Accepted **on the operator's site**, while signed in, before a Connection is made. They define how LendSpare connects to that operator's services.
3. **Schedules** (compute, storage/backup). Accepted separately on the site before the matching Resource is offered. A schedule prevails over the Terms for the Resource it covers.
4. **Privacy notice.** A separate document per operator. It is not replaced by the Terms or the licence.
5. **Operator's existing legal pages** (artur.work: `terms`, `privacy`, `retention`, `data-removal`, `sla`). These drafts sit beside them and were written not to contradict them; the places where a decision touches them are flagged in the text.
6. **Agreement between the Slovak sole trader and BrainyBrain Inc.** (development, licensing, use of rights). Separate, not drafted here. The public licence does not replace it.

**Separation rules the drafts follow.** Installing LendSpare creates no contract. Acceptance happens on a site. The licence is not consent to compute, storage or anything else. Agreeing to one operator's terms never creates a contract with the other. Neither operator's published documents name or advertise the other: the artur.work Terms and the BrainyBrain Terms mention each other nowhere, and refer only to the category "participating platforms" where partner jobs matter. **Publishing note:** `compute-participation-terms.md` holds both annexes for convenience in drafting. Each operator must publish the body plus its own annex only. Remove the other annex and this README's cross-references before publication.

Consistent defined terms: Operator (artur.work) / Company (BrainyBrain Inc., the same role), LendSpare, Device, Connection, Service, Resource, Participant, Job, Listing, Credit.

## 3. What the server must record at acceptance (TZ section 9)

For each acceptance and each later change, an immutable, verifiable record of:

- legal operator id;
- account id;
- terms version, and an immutable copy or hash of the exact text (and of each schedule accepted);
- timestamp;
- site of acceptance (`ai.*` or `share.*`);
- authentication context (how the person was signed in);
- language shown;
- service and purpose (`ai` / compute, `mount` / site-storage, `sync` / accounting-backup);
- granted scopes (`compute:offer`, `files:serve`, ...);
- revocation history (who revoked, when, from site or from LendSpare).

Also, per the protocol: the intent is bound server-side to provider, account, service, scopes and the accepted terms version; `GET /api/v2/share/terms/current` returns version, url and hash and is information only. **Collect an IP address or a device fingerprint only if necessary and documented** in the privacy notice. Acceptance of the MPL licence must never be used as proof of agreement to compute participation.

## 4. Every placeholder (all documents)

Identity and contact
- `[DATE]` effective date
- `[FULL LEGAL NAME OF SLOVAK SOLE TRADER]`, `[ID]` (IČO), `[ADDRESS]`
- BrainyBrain Inc.: `[ID]` (Wyoming filing no.), `[REGISTERED BUSINESS ADDRESS]`
- `[CONTACT EMAIL]`, `[PRIVACY CONTACT EMAIL]`, `[SECURITY CONTACT EMAIL]`
- `[SECURITY INFORMATION URL]`, `[PRIVACY NOTICE URL]`, `[PRICING URL]`
- `[DPO NEEDED?]`, `[EU/UK REPRESENTATIVE NEEDED?]`, supervisory authority for BrainyBrain users

Terms
- `[MINIMUM AGE]` (both operators)
- `[MAXIMUM TERMINATION TIME]` for a running job after pause or revocation
- `[LIABILITY TERMS]` (artur.work §11; BrainyBrain §9; compute §13; storage §10)
- `[GOVERNING LAW, VENUE, COMPLAINTS PROCEDURE AND ADR BODY]` (artur.work §14)
- `[GOVERNING LAW, VENUE, ARBITRATION OR COURT, CLASS-ACTION TERMS]` (BrainyBrain §11)
- `[TERMINATION AND SUSPENSION TERMS]` (BrainyBrain §10); notice and appeal rules (artur.work §12.2)
- `[NOTICE METHOD AND PERIOD]` for changes (artur.work §13)
- `[SERVICE AVAILABILITY, WARRANTIES]` (BrainyBrain §9)
- `[PARTNER-PLATFORM DISCLOSURE, IF ANY]` and `[YES/NO]` per operator (compute §11, A4, B4)
- `[FINANCIAL MODEL]` for BrainyBrain (credits only or money) (BrainyBrain §5.2, compute B1)
- MPL-2.0 `[CONFIRM]`

Compute
- `[PLATFORM FEE PERCENTAGE]` per operator (A2, B2)
- `[SUGGESTED DISCOUNT]` and its basis (A2, B2); `[MINIMUM/MAXIMUM]` price limits, if any
- `[DISPLAY NAME RULES]` for the gallery; `[SEVEN DAYS]` review age; `[THREE]` reports threshold
- `[DISPUTE PERIOD]`, `[RESPONSE PERIOD]`
- credit-on-closure rule, option a/b/c/d with `[N]` days (compute §7.6, A5, B5)
- final list of job classes (compute §3.1)
- BrainyBrain currency and display units (B3); US state-law and tax points (B7)

Storage
- `[DEFAULT QUOTAS]`, `[DEFAULT VERSION POLICY]`, `[CONFIRM TIMES]` for session sweeps
- `[DEVICE LOG RETENTION]`
- accounting backup: `[BACKUP RETENTION]`, `[RESTORE-TEST FREQUENCY]`, `[CONFIRM DESIGN]` of encryption

Microsoft Store
- `[STORE PUBLISHER]`, `[STORE PUBLISHER LEGAL NAME]` (section 6)
- Partner Center reports used by the publisher (privacy outline 2a; app statement section 5)

Privacy
- suppliers per operator, international transfers per operator
- `[ACCEPTANCE RECORDS RETENTION]`, `[CONNECTION RECORDS RETENTION]`, `[SERVER LOG RETENTION OF PAIRING DATA]`, `[OWN RETENTION TABLE]` (BrainyBrain)
- BrainyBrain's own statements on training, sale and advertising use

## 5. Questions the lawyer must answer

Roles and structure
1. Are the two operators separate controllers, joint controllers, or controller and processor? May any personal data pass between them? (TZ §9 says this needs its own determination.) Which agreement between them is needed?
2. What is the Participant's legal role for Job content run on their Device (technical host, processor, sub-processor), and what must the contract say?

Money
3. Credits only: is it enough to state "no cash value, no payout, not transferable" and avoid money-services and e-money rules in both Slovakia/EU and the US states? If BrainyBrain wants cash, what is needed (payout, tax forms, sanctions)?
4. What happens to unspent credit when an account closes: forfeiture, grace period, refund of paid top-ups only, or freeze? (Four options in compute §7.6; the existing artur.work practice is closest to "freeze, handle case by case".)
5. Tax: any reporting or VAT consequence for the operator or Participant of credits earned for work done? (The artur.work seller is a Slovak sole trader, not a VAT payer; the second deployment is a US seller.)

Consumer and platform law
6. Is a Participant a consumer or a trader? Which statutory rights cannot be excluded, and what does the liability section (including damage to a Device) have to look like in each jurisdiction? Is the existing twelve-month cap and carve-out repeatable here?
7. May a new version bind a Connection only after express re-acceptance (as drafted), instead of "continued use is acceptance" as on the existing artur.work terms?
8. Suspension and switching a Listing off after a failed benchmark or spot check: statement of reasons, notice, appeal; Art. 22 GDPR if automated.

Partner platforms and disclosure
9. Is describing partner-platform jobs only as a category ("participating platforms"), without naming the platform or the Customer, enough disclosure in each jurisdiction? Under which conditions must a separately responsible company be named?

Evidence and age
10. Which acceptance evidence is enough, and is collecting an IP address justified? Minimum age, and how to enforce it without collecting more.

Storage
11. Data of a third party (for example a website's files, later accounting data) held on a private person's computer: what contract, what duties for the Device holder, and is the absence of any durability promise acceptable?

Other
12. Governing law, venue, ADR and (for BrainyBrain) arbitration and class-action terms.
13. Whether the public gallery needs consent wording.

Microsoft Store
14. Who is the Store publisher, and does naming one operator (or a third entity) as publisher conflict with the separation rules in section 2?
15. Licence terms entered in Partner Center (MPL-2.0 instead of Microsoft's Standard Application License Terms), and whether the "Where you got LendSpare" clause (artur.work Terms 1.4, BrainyBrain Terms 1.3) is consistent with the Microsoft Store App Developer Agreement.

## 6. Distribution through the Microsoft Store

LendSpare may be published in the Microsoft Store as an MSIX package (`packaging/msix`, `scripts/pack-msix.sh`). The submission texts are in `docs/store/certification-notes.md`. What that changes for these documents:

- **Store publisher.** The Store listing shows the Partner Center account holder as publisher: `[STORE PUBLISHER]`. It is the publisher of the software, not an operator of any Service. Decide who it is (question 14) before the listing is created; the identity values in `scripts/msix.env` come from that account.
- **Licence.** Enter MPL-2.0 as the app's own licence terms in Partner Center, so Microsoft's Standard Application License Terms do not apply instead (question 15).
- **Privacy link.** The listing needs a privacy-policy URL. It points to the published `lendspare-app-privacy-statement.md`, which names no operator. Each operator's notice adds section 2a.
- **Store operator not a party.** Each operator's Terms state it (artur.work 1.4, BrainyBrain 1.3).
- **Work that is never run.** Store policy does not allow apps that mine cryptocurrency on the device. Compute Participation Terms 3.6 excludes mining and other abusive work, both operators' acceptable-use sections repeat it, and LendSpare's connection screen states it (`connect.compute.own_use`). **Each operator's Customer-facing terms must forbid submitting such work, and the review of pipelines and container images must check for it.** These are outside this document set and must be done before the listing goes live.
- **Software installed separately.** Compute needs a container engine, a GPU driver or a local model server that LendSpare neither includes nor installs (Compute Participation Terms 2.5). The Store listing says the same.
