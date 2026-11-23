# Privacy Notice: outline for LendSpare

> **Draft 0.3, 2026-11-18. Working outline for lawyer review. This is NOT a finished privacy notice, is not legal advice and must not be published as it stands.**
> It lists what a GDPR Article 13 notice needs for LendSpare and fills in what the service already does. Purposes and legal bases are **proposals**. One notice per operator; the two operators are separate and each notice must stand on its own and mention only its own operator.

## 0. Form

One notice per operator, linked from that operator's Terms and from the connection screen on its `share.*` site. It is separate from the Terms and from the software licence. It extends, and must not contradict, the operator's existing privacy page. (artur.work's existing page already covers accounts, usage and billing, technical records, providers, content storage, retention and rights; this outline adds the LendSpare-specific items.)

## 1. Controller (per operator)

| | artur.work | BrainyBrain Inc. |
|---|---|---|
| Controller | [FULL LEGAL NAME OF SLOVAK SOLE TRADER], IČO [ID], [ADDRESS] | BrainyBrain Inc., Wyoming corporation, [ADDRESS] |
| Contact | [PRIVACY CONTACT EMAIL] | [PRIVACY CONTACT EMAIL] |
| DPO / representative | [DPO NEEDED?] | [EU/UK REPRESENTATIVE NEEDED?] |
| Supervisory authority | Slovak Office for Personal Data Protection (as the existing page says) | [FOR EU/UK USERS: AUTHORITY; US STATE REGULATORS] |

The two operators are **separate**. They may share technical infrastructure or code. **Whether they are separate controllers, joint controllers, or controller and processor, and whether any personal data passes between them, needs its own legal determination.** Until it is made, the notice must say nothing that implies sharing, and the system must not share.

## 2. Data actually processed

| Category | Detail | Source |
|---|---|---|
| Account | e-mail (masked in the connection screen), name, language, social-login id | You |
| Device identity | random installation UUID, public key of the Device. No hardware fingerprint | LendSpare |
| Device description | device name you or LendSpare give it, operating-system platform | LendSpare, at pairing |
| Capability reports | kinds of work possible, GPU model and memory, installed local models and their weight identifiers, whether containers are available, whether an egress proxy is available, which model files and nodes an image workflow can use | LendSpare, only while a compute Resource is on and the Connection allows it |
| Folder metadata | names and quotas of shared folders you list; never full local paths and never file content (sealed) | LendSpare, only for a Connection with the storage permission |
| Connection records | Connection id, service, scopes, state (connected / disconnected), pairing and revocation times | Operator |
| Job metadata | which Job, when, which Listing, duration, outcome, price and credit, benchmark and spot-check scores; the Job's content and outputs as stored for the retention period | Operator |
| Logs | IP addresses and request records in server logs; the IP of failed pairing attempts (limited to five wrong codes per ten minutes) | Operator |
| Acceptance records | operator id, account id, terms version and hash, time, site of acceptance, authentication context, language, service and purpose, granted scopes, revocation history (see README) | Operator |
| Public gallery | parameters and output of your own test runs that you publish | You, via LendSpare |
| Billing | credit ledger for earnings and spending; sale and tax records for top-ups | Operator |

Not processed: file content of shared folders (the Operator cannot read it), local paths, a hardware fingerprint, telemetry from an installed LendSpare that is not connected, and data from the app store LendSpare was installed from (section 2a).

## 2a. Installation through the Microsoft Store

If LendSpare was installed from the Microsoft Store, Microsoft processes data about the download, installation, updates and crashes **as its own controller**, under the Microsoft Privacy Statement. The Operator receives nothing from Microsoft about a person. The Store publisher of LendSpare ([STORE PUBLISHER], see README section 6) sees only the reports Partner Center provides, such as aggregated acquisition and installation counts and app health reports [PRODUCT/LAWYER: confirm which reports are used, and whether health reports can contain personal data]. None of it is linked to an Account or a Connection. The Store listing links to the operator-neutral **LendSpare app privacy statement** (`lendspare-app-privacy-statement.md`), not to this notice.

## 3. Purposes and proposed legal bases

| Purpose | Proposed basis | Note |
|---|---|---|
| Create and operate a Connection; authenticate the Device | Contract (Art. 6(1)(b)) | |
| Lease and run compute Jobs; pay credit | Contract | |
| Quality control (benchmarks, spot checks, switching a Listing off) | Contract; legitimate interest in service integrity | Lawyer: whether automated switching-off needs Art. 22 wording |
| Security, abuse prevention, logs | Legitimate interest (6(1)(f)) | Retention to be fixed |
| Keep the books, sale and ledger records | Legal obligation (6(1)(c)) | |
| Record acceptance of terms | Legal obligation / legitimate interest (defence of claims) | Lawyer |
| Public gallery of your own test results | Consent or contract; and your own publishing action | Lawyer |
| Optional notifications | Consent | |

## 4. Recipients

- Hosting provider and storage provider of the Operator [NAMES ACCORDING TO EACH OPERATOR'S REAL SUPPLIERS].
- E-mail provider; payment provider (for top-ups).
- **Customers** of a Job do not receive your identity. **Other Participants** do not receive yours.
- AI vendors that process Customer work are recipients of Customer data, not of Participant data.
- Participating platforms: [ONLY IF THE OPERATOR'S TERMS PERMIT IT; CATEGORY AND WHAT DATA THEY SEE, IF ANY].
- The public, for gallery items you publish.

## 5. International transfers

[PER OPERATOR: where servers are, which providers are outside the EEA, which transfer mechanism. For the Wyoming operator, transfers of EU/UK users' data to the US.]

## 6. Retention (pointers)

- artur.work: point to the existing retention page; content deleted on the stated windows; sale, ledger and money columns kept on legal obligation; server logs twelve months; [ACCEPTANCE RECORDS RETENTION]; [CONNECTION RECORDS RETENTION]; [SERVER LOG RETENTION OF PAIRING DATA].
- BrainyBrain Inc.: [OWN RETENTION TABLE].
- Closed account: what remains and why (ledger and sale records; the name and e-mail on the record the books point to), matching the Participant's credit-closure rule.

## 7. Your rights and how to use them

Access, copy of everything (the account export), correction, deletion, restriction, objection to legitimate-interest processing, portability, withdrawing consent, complaint to an authority. [US: state-law rights and the process for requests, if applicable.] LendSpare itself keeps a local log and local settings that you can delete on your Device; removing a Connection invalidates credentials but does not delete server-side records, which are handled through the account deletion route.

## 8. Not done

No sale of data, no advertising use, no use of prompts, uploads or results to train models (the existing statements on the artur.work page; [BRAINYBRAIN TO CONFIRM ITS OWN]). No automatic collection by an installed but unconnected LendSpare.

## 9. Open questions for the lawyer

1. Controller/processor/joint roles of the two operators; any need for an agreement between them; what, if anything, may be shared, and whether the architecture should simply keep the data apart.
2. The Participant's role for Job content processed on their Device (technical host, processor, sub-processor), and the contract wording that follows.
3. Whether benchmark and spot-check switching-off is automated decision-making under Art. 22.
4. Legal basis and retention for acceptance records and for failed-pairing IP logs.
5. Whether the public gallery needs consent wording, and display-name rules.
6. Does any Art. 27 representative, DPO or Art. 30 record work apply for BrainyBrain Inc.? US state privacy laws?
7. International transfers per operator.
8. Children: minimum age [MINIMUM AGE] and how it is enforced.
9. Whether the existing artur.work privacy page should be amended in place or the LendSpare items added as a section.
10. Microsoft Store distribution: confirm section 2a (Microsoft as a separate controller; which Partner Center reports the publisher uses) and that the Store publisher's identity does not imply any sharing between the two operators.
