# LendSpare Connected Services Terms (artur.work)

> **Draft 0.3, 2026-11-18. Working draft for lawyer review. Not legal advice, not a published document, and not in force.**
> Square-bracketed text is unknown and must be filled in or removed by the lawyer. Developed from Template A of `tmp/LendSpare_TZ_and_Terms_Drafts.md`.

**Effective date:** [DATE]
**Operator:** [FULL LEGAL NAME OF SLOVAK SOLE TRADER], business registration number (IČO) [ID], registered address [ADDRESS], contact [CONTACT EMAIL] (the "Operator", "we").
**Services covered:** `ai.artur.work` and `share.artur.work`. `sync.artur.work` is **planned, not offered**; it is covered by these Terms only from the day the Operator announces it and publishes its schedule.

## 1. What these Terms are, and what they are not

1.1 These Terms govern the connection of a Device running LendSpare to the Service you select, and the use of that Connection. They sit next to, and do not replace, the Operator's general site terms, privacy notice, retention page and service-level page, and the schedules listed in section 16.

1.2 **Defined terms.**

- **LendSpare**: the application you run on your computer. It is separately licensed to you under its published open-source licence (MPL-2.0 [CONFIRM]).
- **Device**: the computer on which LendSpare is installed.
- **Service**: a specific Operator service you connect a Device to (`ai`, `mount`; `sync` once offered).
- **Connection**: the link between one Device, your Operator account, one Service and a list of permissions, created under section 3.
- **Resource**: something on the Device you choose to offer through a Connection: computing capacity (GPU, CPU, installed models, containers), or a folder.
- **Participant**: you, when you offer computing capacity.
- **Account**: your account with the Operator.

1.3 **Installing LendSpare creates no contract with the Operator.** Downloading, installing or starting LendSpare does not open an account, does not make you accept these Terms, does not permit the Operator to access the Device and does not share anything. The software licence is not an agreement to these Terms, and agreeing to these Terms does not change your rights under the licence.

1.4 **Where you got LendSpare.** You may obtain LendSpare from its own download page or from an app store, such as the Microsoft Store. The store's terms govern only the download, installation and updates. The store operator is not a party to these Terms, has no obligations under them and neither provides nor supports the Service; questions about the Service go to the Operator ([CONTACT EMAIL]). The same rules apply whichever way you obtained LendSpare. [LAWYER: check this wording against the Microsoft Store App Developer Agreement and the licence terms entered in Partner Center; see README section 6.]

## 2. Who may connect

2.1 You must be at least [MINIMUM AGE] and old enough to enter into a contract where you live. You must have the right to offer the Resource you connect: a computer or folder you do not control, or that belongs to your employer or another person, is not yours to offer without their permission.

2.2 You need an Account on the applicable site. The existing site terms apply to the Account.

## 3. Enrollment: how a Connection is created

3.1 A Connection is activated only when all of the following have happened, in this order:

1. you are signed in to the applicable site and have accepted the current version of these Terms and of any schedule that applies to the Resource, **on that site**;
2. you asked the site to connect this computer, or asked LendSpare to connect to the Service;
3. LendSpare, on your Device, showed you the Operator's name, the address it will talk to, the Service and the permissions requested, and you confirmed on the Device;
4. for a folder, you chose the folder and its rights on the Device; for computing, you switched it on on the Device.

3.2 A Connection is limited to the Account, Device, Service and permissions shown in the confirmation. Nothing in a link, a code or a web page can widen it. Credentials issued to the Device are specific to that Connection and are not used for any other Operator service or any other party.

3.3 A Resource is not offered merely because LendSpare can see it. A permission granted to a Connection is not usable until you have also switched the Resource on locally.

3.4 We may refuse or suspend an enrollment for technical, legal or security reasons, as far as applicable law allows.

## 4. Your controls

4.1 You can, at any time and without giving a reason: pause a Connection or the whole of LendSpare; switch a Resource off; change limits; remove a Connection in LendSpare or on the site. Removing a Connection invalidates its credentials on our side.

4.2 Pausing, switching off or removal stops new remote operations being authorised. A job that is already running may need a short, bounded time to stop; it ends within [MAXIMUM TERMINATION TIME].

4.3 Disconnecting one Connection does not affect your other Connections, their settings or their folder permissions.

4.4 If our servers cannot be reached, LendSpare's local functions (for example its list of shared folders) keep working, and unfinished jobs stop rather than retry without limit.

## 5. Compute sharing

5.1 Compute sharing is opt-in. If you switch it on, the Device may run computing jobs for the Operator's customers and, only if and as disclosed in the Compute Participation Terms you accept, for customers of participating platforms. The customer behind an individual job need not be shown to you. Only the classes of work listed in the Compute Participation Terms run; cryptocurrency mining and the other work excluded there never do.

5.2 The detailed rules (job classes, containers, network limits, benchmarks and spot checks, the public gallery of your own test results, prices, credits, prohibited conduct) are in the **Compute Participation Terms**, which apply in addition to this section and which you accept separately on the site before a compute Resource is offered.

5.3 Running a job is not permission to look at your files, saved credentials, desktop sessions or other devices and services on your network, and no job receives such access because compute sharing is switched on. Equally, you must not use the Device or its software to read, copy, keep or use the content of a job beyond running it.

## 6. Storage and backup

6.1 If you enable folder functions, the site you named may use only the folders and operations you approved on the Device, within the quotas you set. Folders are read-only by default. Writing, deleting and replacing files each need your separate approval.

6.2 We pass sealed data between your Device and your site and cannot read the files. Authorisation lives on your Device, not on our servers.

6.3 The **Site Storage and Backup Schedule** contains the rest, including that a home computer is not an availability guarantee and that you must keep independent copies.

## 7. Compensation and fees

7.1 **Credits only.** Compensation for compute sharing is in platform credit, not money. Credit is spent on your own use of the Operator's service. It has no cash value, cannot be paid out, redeemed for money or transferred to another account, and does not earn interest. The Operator keeps a platform fee from what the buyer pays, as stated in the Compute Participation Terms and the pricing page ([PRICING URL]).

7.2 You are not entitled to compensation merely for installing LendSpare, connecting a Device or listing a Resource that nobody uses. Rules on measurement, reversal and disputes are in the Compute Participation Terms.

7.3 What happens to unspent credit when an account closes is in the Compute Participation Terms.

7.4 Taxes on anything you earn or receive are your responsibility, as far as the law allows. VAT on top-ups is handled under the site terms and is not changed by these Terms.

## 8. Security and acceptable use

8.1 The Operator enforces job classes, container restrictions, limits, abuse monitoring and emergency suspension. Technical safeguards and the channel for reporting a security problem are published at [SECURITY INFORMATION URL] and [SECURITY CONTACT EMAIL].

8.2 You must not: offer a Resource you have no right to offer; use LendSpare or a Connection to carry malware, unlawful content or unauthorised access; run, offer or request cryptocurrency mining or other work excluded by the Compute Participation Terms; try to obtain credentials, tokens or data meant for another person; tamper with LendSpare, a job, a result or a credential so as to deceive the Operator or a customer; or offer folders that hold other people's data you have no right to make available.

## 9. Personal data

9.1 The Operator's Privacy Notice ([PRIVACY NOTICE URL]) explains what personal data is processed, why, with whom it is shared, how long it is kept, international transfers, your rights and whom to contact. LendSpare's connection process sends the Operator technical data about your Device (see the notice).

9.2 Where a data-processing agreement is needed it is agreed separately. Nothing in these Terms replaces any right you have under data-protection law.

## 10. Availability

10.1 The service is offered on a best-effort basis, as set out on the Operator's service-level page. [LAWYER: that page currently promises no uptime percentage and no service credits; confirm this section stays consistent with it.]

10.2 Your Device is your own and can be switched off, disconnected or slow at any time. Neither side promises that a Resource will be available.

## 11. Liability and mandatory rights

[LIABILITY TERMS. LAWYER: the existing site terms limit total liability to what the user paid in the twelve months before a claim, with carve-outs for death, personal injury, fraud and anything else the law does not allow to be limited. Decide whether to repeat that here, to apply it to Participants (who may be paid in credit, not money), and how it applies to loss or damage to a Device or to a folder's contents. Do not insert a blanket exclusion. Nothing in these Terms is intended to remove rights that cannot be removed under the law of the country where you live, and a consumer's statutory rights are unaffected.]

## 12. Suspension, ending and revocation

12.1 You can end a Connection at any time (section 4). You can delete your Account from your profile; what is deleted and what is kept is stated on the Operator's "Deleting your account" page and in the Privacy Notice.

12.2 We may suspend or end a Connection, or the Account, if you break section 8 or the Compute Participation Terms, or if needed to protect users, systems or other people, and we will say why unless the law prevents us. [LAWYER: notice and appeal rules under consumer and platform-to-business law.]

12.3 Ending a Connection stops authorisation for new remote operations. Records the Operator must keep (sales, ledger entries) remain, as described in the Privacy Notice.

## 13. Changes

13.1 We will notify you of material changes as required by law, by [NOTICE METHOD AND PERIOD]. A new version applies to a Connection only after you accept it on the site; until then we may stop leasing new work to that Connection. [LAWYER: the existing site terms say continued use means acceptance; confirm that is adequate for a Connection that grants remote rights to a computer, or whether express re-acceptance, as drafted here, is required.]

13.2 Each acceptance is recorded as described in the Privacy Notice.

## 14. Law, disputes and complaints

[GOVERNING LAW, VENUE, COMPLAINTS PROCEDURE AND ADR BODY. LAWYER: the existing site terms say Slovak law applies and that a consumer keeps the protections of the country where they live. Check EU consumer law, online dispute resolution requirements, and whether Participants are treated as consumers or as traders.]

## 15. Language and order of documents

The English text governs, as on the Operator's other legal pages. If a schedule conflicts with these Terms for the specific Resource it covers, the schedule prevails for that Resource.

## 16. Related documents

Compute Participation Terms; Site Storage and Backup Schedule; Accounting Backup Schedule (planned, with `sync.artur.work`); Privacy Notice; retention page; service-level page; pricing and credits page.

## 17. Contact

[CONTACT EMAIL] for anything about these Terms. [PRIVACY CONTACT EMAIL] for privacy requests. [SECURITY CONTACT EMAIL] for security reports.
