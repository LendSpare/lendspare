# Compute Participation Terms

> **Draft 0.3, 2026-11-18. Working draft for lawyer review. Not legal advice, not a published document, and not in force.**
> Square-bracketed text is unknown and must be filled in or removed by the lawyer.

**How this document is organised.** The body is common to both operators. **Annex A** applies only to Participants of the artur.work Operator and **Annex B** only to Participants of BrainyBrain Inc. When published, each operator's page carries the body plus **its own annex only**, with no reference to the other operator (see README). In the body, "the Operator" means the operator whose site you accepted these terms on.

These terms are accepted on the Operator's site, **before** a compute Resource is offered. They are in addition to the Operator's LendSpare Connected Services Terms (or LendSpare Compute Connection Terms). Installing LendSpare, or accepting the LendSpare software licence, is not acceptance of these terms.

## 1. Defined terms

- **LendSpare**: the application on your computer. **Device**: the computer it runs on. **Connection**: the link between your Device, your account, one Service and a list of permissions. **Service**: the Operator service you connected to.
- **Resource**: computing capacity you offer (GPU, CPU, installed local models, the ability to run containers).
- **Participant**: you, when you offer a Resource.
- **Job**: one unit of work the Operator leases to your Device. **Customer**: whoever asked for the work.
- **Listing**: your priced offer to run a kind of work, with your Device behind it.
- **Credit**: the Operator's platform credit.

## 2. Opt-in and control

2.1 Nothing is offered unless you switch it on on the Device and the Connection carries the compute permission. Every compute permission starts off.

2.2 You choose which kinds of work you accept, and you can set limits (for example GPU memory, time, CPU and power). LendSpare runs **at most one compute job at a time** on the Device, across all Connections, so two services cannot overcommit one GPU.

2.3 The Device is offered work only when it can run it: LendSpare reports to the Operator what the Device can do (kinds of work, GPU, installed models and their identifiers, whether containers are available), and the Operator leases a Job only to a Device whose report covers it. LendSpare sends this report only while the Resource is on and only to a Connection that carries the permission.

2.4 **Your own use comes first.** By default LendSpare takes no GPU work while another program on the Device is using the GPU, and it stops the image-generation container it started while you use the card. You can switch this behaviour off in LendSpare's configuration.

2.5 **Software you install yourself.** Some classes of work need software that LendSpare neither includes nor installs: a container engine (Docker Desktop or Podman), a GPU driver, or a local model server (such as Ollama or LM Studio). That software is under its own licence and terms. Without it, the classes that need it are simply not offered to your Device.

## 3. What work may run

3.1 Only these classes of work may be leased to your Device: image-generation workflows from a curated or reviewed catalogue; chat with a local language model whose exact weights you have installed; text-to-speech; benchmark and quality-check jobs; and, where you enabled it, containerised tasks (pipelines, graders, feature-measurement jobs) as described below. The Operator's pages list the classes currently offered. [LAWYER/PRODUCT: confirm the final list before publication.]

3.2 **Containers.** A container runs only if its image is identified by a pinned digest and is on the list your LendSpare trusts. It runs with no network, or only on an isolated network whose single exit is an allow-list proxy limited to the destinations the job declared. A container receives its inputs and writes its outputs through a fixed file contract. LendSpare does not give any remote party your container-engine socket.

3.3 **Network.** A job cannot reach your local network or the internet except through the allow-list above. Job inputs are fetched only from the Operator's own address, and redirects to another address are refused.

3.4 **Secrets.** Where a container needs a key from you, LendSpare passes it to the container engine only for images on your trust list, and never reads it.

3.5 The Operator may add or remove classes of work. A class that needs more access than you have granted is not leased to you.

3.6 **Work that is never run.** The following is not a class of work and is never leased to your Device, whoever asks for it: mining or validating cryptocurrency, or any other proof-of-work or hashing computation done to earn or create a digital asset; cracking passwords, keys or other protection measures; attacking, scanning or flooding any system or network; and any work whose purpose or output is unlawful. The Operator does not accept such work from Customers and removes any Listing, pipeline or container image found to perform it. [PRODUCT: the Operator's Customer-facing terms and the review of pipelines and images must say and check the same; see README section 6.]

## 4. What a Job can and cannot reach on your Device

4.1 A Job runs inside the model server or container that does the work. It has no access to your files, folders, saved credentials, browser, desktop or other programs, and is not given any.

4.2 **Be aware.** The content of a Job (for example a prompt or a picture) is processed on your Device and may be technically visible to the software running it, to the operating system and to you. You must not read it, copy it, keep it, publish it or use it for any purpose except running the Job. You must not log or store prompts or outputs beyond what LendSpare does automatically and removes. The Operator tells Customers that work sent to community machines runs on another person's computer.

4.3 The Operator does not give you the identity of the Customer behind a Job, and you must not try to find out.

## 5. Quality: benchmarks, spot checks, switching a Listing off

5.1 The Operator measures that your Device does the work it says it does. This includes:

- **Benchmarks.** When you create a Listing, and periodically afterwards, the Operator sends benchmark jobs (for language models, fixed-temperature prompts compared with reference answers; for other kinds, fixed inputs checked against fixed criteria). Benchmark jobs are not charged to anyone and earn you nothing.
- **Spot checks.** A small share of real Customer jobs is accompanied by a re-run of a previously published result, to see whether your Device still produces comparable output.

5.2 If your results fall below the stated tolerance, the Operator may **switch the Listing off** (and, for a spot-check miss, hide your published test results for that workflow). A switched-off Listing receives no work. The Operator tells you the reason; you may ask for a re-check after you fix the cause. A benchmark that cannot be completed (for example no reference exists yet) is left pending and is not treated as a failure.

5.3 A Listing is offered to Customers only after it meets the Operator's conditions, including at least one published self-run of the workflow you offer, and, for local language models and containerised tasks, a passed benchmark. A Customer who has chosen to trust your Device may be offered it without the public evidence.

## 6. The public gallery of your own test results

6.1 To show Customers what your Listing produces, the Operator may publish, in a public gallery, results of **your own** test runs of your Listing, run by you on your own Device. Parameters and the output of that run are copied from the job and are visible to the public, and so is that you (or your Listing) produced them [DISPLAY NAME RULES: TO DECIDE].

6.2 Only a successful run you started yourself on your own machine can be published. Results of Customer jobs are never published. A Customer's own photo is never published.

6.3 Accounts newer than [SEVEN DAYS: CONFIRM] publish into review. Items reported by [THREE] different people are hidden until the Operator decides. You must not put into a test run anything you have no right to publish. Published items are removed when you delete your account.

6.4 A listing or pipeline that does not follow these rules is removed. A pipeline you offer must call AI vendors with an API key issued to you for that purpose, never with a consumer subscription (such as a chat subscription), which is for its subscriber alone.

## 7. Prices, fees and compensation

7.1 **Pricing is yours to set**, within the limits the Operator publishes [MINIMUM/MAXIMUM, IF ANY]. The Operator shows a **suggested price** (derived from what comparable vendor services currently charge, reduced by a stated discount, or a default) which you may accept or change. The price Customers pay and what you earn are shown to you, and the price is shown in the units the Operator publishes (for example per ten images, per million tokens, per 1,000 characters).

7.2 **Platform fee.** The Operator keeps a fee of **[PLATFORM FEE PERCENTAGE]** of the price paid for a Job. Your earning for a Job is the price minus that fee.

7.3 **Compensation is in Credit only.** You are paid in the Operator's platform credit, which is added to your balance when a Job is accepted as successful. **Credit has no cash value. There is no cash payout. Credit cannot be redeemed for money, sold, or transferred to another account.** It can be spent only on your own use of the Operator's service. Credit does not earn interest and is not money held on your behalf.

7.4 **When you are paid.** For ordinary jobs, when the result is delivered and accepted. For work that is paid on passing a check (for example some pipelines), you receive the full price if the check passes, a base share if it fails, nothing if the work is declined or the Device fails, and where the check is inconclusive twice you are paid in full. A job that fails, expires or is cancelled earns nothing. [LAWYER/PRODUCT: confirm wording once the rules are final.]

7.5 **Reversal and disputes.** The Operator may reverse credit that was earned through a falsified result or a breach of section 9. You may dispute an amount within [DISPUTE PERIOD] by writing to [CONTACT EMAIL]; the Operator answers within [RESPONSE PERIOD].

7.6 **Unspent Credit when an account closes.** [DECISION REQUIRED. Options for the lawyer: (a) unspent Credit is forfeited on closure, stated plainly before the Participant ever earns any; (b) Credit remains usable for [N] days after notice of closure, then is forfeited; (c) Credit earned by sharing is forfeited, but unspent Credit that the person themselves paid for (a top-up) is refunded as the site terms already provide; (d) Credit remains on the closed account, cannot be spent, and is handled case by case in a refund conversation. The existing artur.work system leaves the balance on the closed record and unusable, which is closest to (d). Whichever is chosen, the closure page, the privacy notice and the site terms must say the same.]

7.7 **Taxes.** What you earn and how it is taxed is your responsibility, as far as the law allows. The Operator does not give tax advice. [LAWYER: whether Credit that is not convertible to money has any reporting consequence for either operator or the Participant.]

7.8 You bear your own costs: **electricity, hardware wear, heat, noise, network traffic and the time your computer is busy.** Credit does not promise to cover them.

## 8. Pausing, limits and preemption

8.1 You can pause LendSpare, pause a Connection or disconnect at any time. LendSpare also holds off while your GPU is in use by something else (section 2.4).

8.2 If you stop or lose power during a Job, the Job's lease lapses and it is queued again for another machine. A stopped Job earns nothing, and no penalty is applied for an occasional interruption. A job stops within [MAXIMUM TERMINATION TIME] of a pause or revocation. Repeated abandonment of work may lower your Listing's priority.

## 9. Prohibited conduct

You must not: falsify or fabricate results, replay old results as new, or return output not produced by the work leased; tamper with, patch, reverse-engineer for the purpose of deceiving, or impersonate LendSpare or its credentials; use another person's Device or account; run work for the Operator's Customers outside LendSpare to avoid the fee or checks; read, keep or use Job content (section 4.2); offer, run or disguise any work excluded by section 3.6, including cryptocurrency mining; try to identify Customers; offer a Resource you do not have rights to; or interfere with other Participants or the service. The licence of LendSpare's source code lets you modify the code for your own use; it does not allow you to present a modified client to the Operator as the genuine one.

## 10. Suspension and ending

The Operator may suspend a Listing or Connection at once for a suspected breach of section 9 or to protect people or systems, and will say why unless the law prevents it. You can stop at any time. On closing, section 7.6 applies. [LAWYER: notice, appeal and statement-of-reasons rules.]

## 11. Jobs from partner platforms

[OPERATOR'S DECISION AND LAWYER: this section is used only if the Operator's terms really permit it.] If, and only if, the section below for your Operator says so, Jobs may originate from customers of participating platforms. Every Job, whatever its origin, is checked against the permissions you granted, the class of work and the data-handling rules in these terms, and is run under your contract with the Operator you accepted these terms with. Accounting records show which party is contractually responsible and where the Job came from, even when you are not shown the Customer.

## 12. Personal data

The Operator's Privacy Notice applies. Job content may contain personal data of Customers or third parties; you process it only as an incidental technical step of running the Job and must follow section 4.2. [LAWYER: whether the Participant is a processor, sub-processor or a mere technical host, and what that implies for the contract.]

## 13. Liability and mandatory rights

[LIABILITY PLACEHOLDER. LAWYER: liability for damage to a Device caused by a Job, for the Participant's costs, and for the Operator's losses caused by the Participant. Do not add a blanket exclusion, and do not remove any right that cannot be removed under the law of the country where the Participant lives.]

---

## Annex A. artur.work (Slovak sole trader)

**Operator:** [FULL LEGAL NAME OF SLOVAK SOLE TRADER], IČO [ID], [ADDRESS].
**Services:** `ai.artur.work`, `share.artur.work`.

- **A1. Credit.** Credit is the platform credit of the artur.work site. As the site terms already say, it pays for work on the site, earns no interest and cannot be transferred. Credit earned by sharing hardware works the same way. It cannot be withdrawn as money.
- **A2. Fee.** [PLATFORM FEE PERCENTAGE: the service's configuration holds this as a share of the price kept by the platform; fill with the verified live value.] Suggested price: [SUGGESTED DISCOUNT: current default is a stated discount below the cheapest comparable vendor price; confirm].
- **A3. Display.** Prices for image jobs are shown in EUR per ten images, for language models per million tokens, for speech per 1,000 characters.
- **A4. Partner platforms.** [YES/NO. If yes: "Jobs may also come from customers of participating platforms. Where they do, the participating platform is not your counterparty and receives none of your account data."] [Wording using only the category "participating platforms".]
- **A5. Closure.** Section 7.6 option: [CHOOSE]. The existing "Deleting your account" page currently says the balance is left in place.
- **A6. Governing law and complaints.** [See the LendSpare Connected Services Terms, section 14.]
- **A7. Peer-network disclosure to Customers.** The site's privacy page already tells users that work sent to the peer network runs on another user's computer; this annex is consistent with it.

## Annex B. BrainyBrain Inc. (Wyoming corporation)

**Operator:** BrainyBrain Inc., [ADDRESS], contact [CONTACT EMAIL].
**Services:** `ai.brainybrain.com`, `share.brainybrain.com`.

- **B1. Compensation.** [CONFIRM: credits only, with no cash value, as in the body, or a different model. If cash is offered, section 7 and this annex must be rewritten with payout, tax-form and sanctions rules.]
- **B2. Fee.** [PLATFORM FEE PERCENTAGE]. Suggested price: [BASIS].
- **B3. Currency and display.** [USD or other; units.]
- **B4. Partner platforms.** [YES/NO and category wording.]
- **B5. Closure.** Section 7.6 option: [CHOOSE].
- **B6. Governing law, dispute resolution, minimum age.** [SEE BRAINYBRAIN TERMS SECTIONS 2.3 AND 11.]
- **B7. State-law and tax points.** [LAWYER: US state consumer, privacy, money-transmitter and tax-reporting implications of the chosen compensation model.]
