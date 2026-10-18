# debroxy — Specification, Revision 12

| | |
|---|---|
| Revision | 12 |
| Status | Draft. Based on live provider discovery (2026-10-11) and design iterations since revision 1; consistent with the accepted ADRs 0001–0019; describes v1 as implemented |
| Date | 2026-10-13 |
| Supersedes | [`revision11.md`](revision11.md) |
| Inputs | [`revision11.md`](revision11.md), [`../discovery/torbox.md`](../discovery/torbox.md), [`../discovery/realdebrid.md`](../discovery/realdebrid.md) |
| Architecture rules | [`docs/ddd-rules.md`](../ddd-rules.md) (binding); ADRs in [`docs/adr/`](../adr/) |

## 0. About this document

### 0.1 Revision workflow

1. Each revision is a new file `docs/specs/revisionN.md`. Past revisions are never edited.
2. Every revision starts with a **changelog** against the previous one and keeps an **assumption register** (§14).
3. ADRs are written next, from this revision's decision log (§13). Decisions marked **ADR required** bend a binding MUST rule and must not be implemented before their ADR is accepted. **ADR recommended** marks important decisions and storage assumptions that bend no rule.

### 0.2 Conventions

- **MUST / SHOULD / MAY** as in RFC 2119.
- `ASM-*` = an assumption that is not verified. `DEC-*` = a design decision (§13). `TB-N*` / `RD-N*` = new discovery findings.
- **Evidence** links point to discovery documents. Provider facts without an `ASM` tag are **observed**.

### 0.3 Changelog (revision 11 → 12)

Approved by the owner (2026-10-13). Revision 12 makes debroxy run in a container: configuration comes only from the environment, the database lives in a data directory, and the image is specified. The model and the ADR decisions are unchanged; the logical configuration keys that ADRs name (`providers[].userAgent`, `providers[].rateBudgets`) keep their meaning and gain an environment form.

| # | Change | Driven by |
|---|---|---|
| C12-01 | **Configuration comes only from environment variables** (§11, DEC-51). The YAML file, `--config` and the `*Env` indirection are gone: each logical key has one `DEBROXY_*` variable and secrets are given directly, or through a `_FILE` variable. Unknown `DEBROXY_*` variables fail validation. `.env.example` replaces `config.example.yaml` as the reference | containers are configured by environment |
| C12-02 | **The database lives in a data directory** (§11): `DEBROXY_DATA_DIR` holds `debroxy.db` and its start lock, so one volume holds all state | one mount point for the container volume |
| C12-03 | **`serve` can create the first admin from the environment** (§4.2, §11, DEC-52). It skips when users exist, so restarts are safe; `bootstrap-admin` stays | a container cannot be bootstrapped interactively before its first start |
| C12-04 | **Container image and `healthcheck` command** (§11, DEC-53); closes OQ-08 | OQ-08 |

### 0.4 Changelog (revision 10 → 11, for reference)

Approved by the owner (2026-10-13). Revision 11 settles two open questions and records one accepted risk; the model and the ADR decisions are unchanged.

| # | Change | Driven by |
|---|---|---|
| C11-01 | **`providers[].cdnDomains` is renamed `cdnProbeHosts`** (§6.6, §11). The old key is no longer accepted: like any unknown key, it fails startup validation | the key holds concrete probe hosts, not domains (OQ-15) |
| C11-02 | **A submission keeps its cached class** (§5.10, DEC-49). `SubmitJob` stores the result of its `CheckCached` with the submission, and every attempt of that submission, including re-runs after a budget block, a deferral or a late deletion check, uses it to pick the provider rate class (ADR 0013 rule 4: "in the same submission") | only the first attempt used the cheaper class (OQ-16) |
| C11-03 | **RISK-08**: a crash while the claims of a merged item move to the surviving item can lose one job (§1.5). Accepted for v1 | the claim move spans two transactions per claim |

### 0.5 Changelog (revision 9 → 10, for reference)

Approved by the owner (2026-10-12). Revision 10 records what building v1 settled: details the earlier revisions left open, names and slugs that were missing, one pipeline rule (C10-04, ADR 0019) and one provider fact (C10-03). The model and the ADR decisions are otherwise unchanged.

| # | Change | Driven by |
|---|---|---|
| C10-01 | **Configuration is one schema** (§11): the repository's `config.example.yaml` is the reference, merging revisions 1–9. Secret formats, `publicBaseURL` without a path, and migrations at `serve` are specified | the schema was spread over three revisions |
| C10-02 | **identity details** (§4.4): session revocation by a per-user session generation, session tokens and cookie, login throttling numbers, password and username rules, password hashing, the admin routes for password reset and OIDC links, OIDC-only users, and the OIDC flow | left open since revision 1 |
| C10-03 | **provider details** (§6.6): disabled accounts, the capability probe, create outcome classification, the Real-Debrid history sweep, CDN redirects, the startup egress check, and **TorBox list paging** (observed: `mylist` honours `limit`/`offset`, so lists are walked page by page) | left open; the paging was observed live |
| C10-04 | **Leftovers are only observations no debroxy item holds** (§5.6 step 6, ADR 0019). An observation of a `failed`/`gone` item that is neither active nor ready is dropped as tracked | ADR 0009 rule 18 listed tracked items as Unmanaged Items, against DEC-10 |
| C10-05 | **job details** (§5.10): where the provider hash is computed, completion with an unknown provider state, refresh-on-read scope, the submit responses, and the periodic commands by name | left open |
| C10-06 | **delivery details** (§7.5): the token payload encoding, links of disabled users, and the link's name segment | left open |
| C10-07 | **Slug catalogue** (§10.3) gains the slugs the API answers today, and the job representation (§10.2) gains `ownerUsername` and `stale` | slugs and fields were used without being listed |
| C10-08 | **Glossary** (§2) gains Provider API, Session Generation, Refresher, Scheduler and Outbox Processor | names used by the design without an entry |

### 0.6 Changelog (revision 8 → 9, for reference)

Approved by the owner (2026-10-12). They shape the external API; the domain model is unchanged.

| # | Change | Driven by |
|---|---|---|
| C9-01 | **API actions are path segments, not colon suffixes:** `POST /api/v1/providers/{id}/refresh`, `POST /api/v1/admin/providers/{id}/probe-capabilities`, `POST /api/v1/jobs/{id}/links/all` (ADR 0018) | the generated server routes on Go's `net/http.ServeMux` (ddd-rules IN-1), where a wildcard must fill a whole path segment, so `{id}:refresh` cannot be registered |
| C9-02 | **`job` serves `GET /api/v1/me`.** It reads the user through a new consumer port `UserDirectory` (`job/ports/out`), implemented by `job/adapters/out/identity` over identity's `UserDetails` query, and adds `QuotaUsage`. `SubmitJob` takes the owner's username snapshot (§5.1 `Job.ownerUsername`) from the same port. This adds the edge `job → identity` that DEC-23 already allows (ADR 0001 rule 6) | the endpoint combines two contexts; only `* → identity` edges are allowed, so the consumer must be `job` |
| C9-03 | **`job` serves the provider routes for users:** `GET /api/v1/providers`, `GET /api/v1/providers/{id}/hosters` and `POST /api/v1/admin/providers/{id}/probe-capabilities`, as the job queries `Providers` and `ProviderHosters` and the job command `ProbeProviderCapabilities`, through `ProviderGateway` | these routes need actors and roles; `provider` is an anti-corruption layer without users (ADR 0003) and has no HTTP adapter |

### 0.7 Changelog (revision 7 → 8, for reference)

Approved by the owner.

| # | Change | Driven by |
|---|---|---|
| C8-01 | **Usenet is deferred to v2.** `SubmitNZBFile` joins `SubmitNZBURL` as a capability the provider has but debroxy v1 does not enable (`capability-not-enabled`, ADR 0003 rule 7). The Job Kind `nzb`, its fingerprint rules and the usenet state mappings stay in the model so v2 needs no migration of the language. ASM-U01…U04 are deferred, not dropped | owner decision; the available TorBox plan rejects usenet creates, so no usenet behaviour can be verified before v1 |

### 0.8 Changelog (revision 6 → 7, for reference)

All items were approved by the owner. With them, the ADRs 0001–0013 are accepted.

| # | Change | Driven by |
|---|---|---|
| C7-01 | `Revive` also requires the observation to be newer than the `deletedAt` of every item that held that provider item ID | a stale listing could revive an item onto deleted content |
| C7-02 | `AttemptStartedAt` is cleared whenever an attempt ends without completing (budget block, deferral, late deletion check) | a leftover attempt start would mislead expiry and the late deletion check |
| C7-03 | `ExpireSubmissions` moves a `deleting` item without a provider ID and without an attempt in progress to `deleted` at its next run | such an item would otherwise stay `deleting` forever |
| C7-04 | `DiscardSubmission` records `SubmissionFailed`, `JobReleased` and `LifecycleAdvanced` | the discard path recorded no events for history and lifecycle |

### 0.9 Changelog (revision 5 → 6, for reference)

All items were approved by the owner.

| # | Change | Driven by |
|---|---|---|
| C6-01 | `Revive` is also blocked while a `deleting` item holds the same provider item ID | a deletion in progress could remove the content a revive points to |
| C6-02 | Budget-blocked creates: `Submission.NotBefore` schedules the re-run (survives restarts); `ExpireSubmissions` counts only from a started attempt; a new attempt starts only when none is in progress | budget-blocked creates were lost on restart or expired before they were ever sent |
| C6-03 | HTTP 429 on a create is treated as an **ambiguous** failure (follow-up refreshes resolve it) | a 429 does not prove that no item was created |
| C6-04 | `CheckCached` is skipped for web submits; every web submit counts as uncached | TorBox charges every web create to the 60/hour bucket |
| C6-05 | `cdnZip` budget in the configuration; new slug `refresh-rate-exceeded` | configuration key and error slug were missing |
| C6-06 | Webhook windows: a trailing refresh opens a new window; other refresh causes keep their own pacing and feed the back-off | the interplay of webhook windows and back-off was undefined |
| C6-07 | Shared SQLite pools and the start lock live in `internal/common/sqlite`, opened in `cmd`; `busy_timeout` 5 s | all contexts share one SQLite database, so they need one writer and one start lock |
| C6-08 | A definite failure after the request deadline appears as a failed job in the creator's history; explicit transition `submitting → deleted` for an item that only the creator claimed | a failure after the request deadline was invisible to a submitter who already got 202 |
| C6-09 | A claim moved by `merged-into` that the other item refuses is released into history with `merged-into-refused{reason}` | a refused `merged-into` claim had no defined outcome |

### 0.10 Changelog (revision 4 → 5, for reference)

| # | Change | Driven by |
|---|---|---|
| C5-01 | **Late deletion check** on submission completion: if an item that held the returned provider ID had its deletion confirmed after the current create attempt started, the create runs again instead of completing onto deleted content. The revision 4 fallback "re-submit the taken-over item" is **dropped**: with this check and deferral it cannot be reached | a create could complete onto content that a concurrent deletion had removed |
| C5-02 | Model fields added: `deletedAt`; `Submission.AttemptStartedAt`; `submission` and `source` also kept while `deleting` without a provider ID | needed by the late deletion check and by submission expiry |
| C5-03 | **`provider` performs the CDN upstream request** through a new operation `OpenDeliveryStream`. The CDN URL never leaves `provider`; `ResolveCDNLink` is removed from the driving port. Egress, User-Agent, budget and the CDN-link cache all stay in one context | CDN URLs are secrets and egress must stay in one context; OUT-4 |
| C5-04 | Deferred submissions are exempt from `ExpireSubmissions`; their own limit is the blocker's deletion outcome (parked → submission fails) | deferred submissions could expire while waiting for a slow deletion |
| C5-05 | The listing completeness flag is called `Complete` everywhere | inconsistent naming |
| C5-06 | Owner decisions: migration naming; writer/reader pools and a start lock; proxy variables ignored; webhook coalescing "immediate + one trailing"; rolling-hour submit rate without refund; failed `CheckCached` counts as uncached; Real-Debrid download history cleaned by a daily sweep only (DEC-25 rewritten) | owner decision |
| C5-07 | Failed or incomplete refreshes back off per provider; reads then answer from stored state with `stale: true` | failing refreshes must not keep hitting the provider and its budget |
| C5-08 | Budget-blocked creates (never sent) stay `submitting` without using an attempt; CDN file downloads are bounded by stream caps, not by a rate class; TorBox web creates always use the 60/hour bucket | rate budget rules clarified (ADR 0013) |
| C5-09 | Delivery targets per kind: TorBox per-file + zip verified for torrents only; web items deliver their single file; usenet targets unverified | delivery support differs per kind (ADR 0011) |
| C5-10 | `Revive` only when no claimable item conflicts on **any** of the three unique keys; with several candidates the newest is revived | a revive must never violate a unique key (ADR 0007) |
| C5-11 | Clarifications: `JobReleased.finalState`, default quota and the `<` comparison, late create outcome after the request deadline, `DeleteRemoteItem` contract, enabled-before-account-state precedence, untrusted link name segment | clarifications for ADRs 0003, 0004, 0005, 0006, 0008 and 0010 |

### 0.11 Changelog (revision 3 → 4, for reference)

| # | Change | Driven by |
|---|---|---|
| C4-01 | **Submission lives inside `RemoteItem`.** The item is created in a new `submitting` state before the provider call; the separate `PendingSubmission` aggregate, waiting jobs and the `DeleteProviderItem` message are gone. Joining a submitting item is an ordinary `Claim`. Every flow changes exactly one aggregate per transaction | one aggregate per transaction (D-11); simpler joining and quota checks |
| C4-02 | **No cross-aggregate revive or deletion cancellation.** Uniqueness of fingerprint, provider hash and provider item ID applies only among claimable items, so a new submission never collides with a `failed`/`gone`/`deleting` item. A create that returns a provider ID still held by a `deleting` item is **deferred** until that deletion finishes | cross-aggregate revive and cancellation changed several aggregates in one transaction (D-11) |
| C4-03 | Refresh pipeline: known live items → interrupted submissions (by provider hash) → reappearing items (`Revive`) → ignore `deleting` → unmanaged → absence (only on a complete listing) | refresh must handle interrupted submissions, reappearing and deleting items, and partial listings |
| C4-04 | Outbox deletion never holds a transaction across the provider call; it is superseded when a claimable item has taken over the provider ID | a transaction held across a network call blocks the single writer |
| C4-05 | Idempotent `SubmitJob` by `JobID` | client retries must not create duplicate jobs |
| C4-06 | Joining costs nothing unless the item's charge holder was released; then the joiner needs free quota. Spread on release stays unconditional | the quota rules for joining were unclear |
| C4-07 | Web password removed from the fingerprint (TorBox keeps one item per URL) | password variants cannot be held apart |
| C4-08 | NZB-URL submission disabled until ASM-U02 is verified | NZB-URL reconciliation is unverified |
| C4-09 | `Release` checks "no claims remain" first; `Revive` charge fields defined for active and non-active results; debroxy-detected stalls clear on progress or state change | edge cases in release, revive charges and stall clearing were undefined |
| C4-10 | Retention for `deleted` items; follow-up refreshes cancelled when a submission resolves; absence grace configurable | `deleted` items grew without bound; follow-ups ran after resolution; a fixed grace did not fit every setup |
| C4-11 | TorBox `SubmitWebURL` starts `available` (observed); `unverified` only for usenet | observed on plan 1 |
| C4-12 | Startup budget rule reworded ("single-flight until first headers"); header-less buckets start at 50 % of their static budget for the first window | the old rule was ambiguous; header-less providers may still count calls from before a restart |

Kept: webhook secret in the URL path (TorBox offers nothing else); the verified-deletion residual.

### 0.12 Changelog (revision 2 → 3, for reference)

| # | Change | Driven by |
|---|---|---|
| C3-01 | **Slot charge rule (final).** While the job that started the current active run (the **charge holder**) is unreleased, only its owner is charged; joiners are free. Once the charge holder is released while the item is still active, **every** remaining claimant is charged against their own quota, including later joiners. Joining an active item without a charge holder needs free quota. Being over quota never stops a download; it only blocks new charged submissions | owner decision (rewrites DEC-28) |
| C3-02 | **Revive** transition: a `failed` or `gone` item whose provider ID comes back (provider dedup on resubmit, or reappearing in an authoritative listing) returns to the observed state; the job that caused it becomes charge holder | provider dedup can return the ID of a `failed` or `gone` item |
| C3-03 | Webhook-triggered refreshes are **coalesced** to a minimum interval per provider; the DoS surface is documented | webhook abuse could drain the shared budget |
| C3-04 | Pending submissions: definite failures resolve immediately (`abandoned{reason}`, no history entry); ambiguous failures schedule **follow-up refreshes**; the timeout exceeds the follow-up schedule | definite failures produced false `submission-lost` entries, and the pending timeout was shorter than the idle refresh |
| C3-05 | One refresh **pipeline**: list → match → reconcile pending → leftovers become Unmanaged Items | refresh steps had no defined order |
| C3-06 | **Same-fingerprint creates are serialized** per provider: a second submission attaches to the in-flight pending submission instead of creating again | concurrent same-content submissions could create twice |
| C3-07 | Web password: secondary evidence that TorBox accepts a form field `password` (ASM-T26) | web password support was unverified |
| C3-08 | Egress startup check covers CDN domains | CDN downloads use the same pinned egress |
| C3-09 | Capability state persisted (small `provider` table); the API shows `unverified` for never-confirmed capabilities | capability state was lost on restart; clients could not tell unproven capabilities |
| C3-10 | `capability-not-enabled` (provider supports, debroxy v1 doesn't) separated from `capability-not-supported` | two different causes shared one error |
| C3-11 | Stall detection only in `resolving_metadata` and `downloading`; `lastProgressAt` resets on every state change | stalls were flagged in states without expected progress |
| C3-12 | ADR tags corrected; the SQLite single-writer reliance recorded as a storage assumption | wrong ADR tags; the quota check relies on SQLite's single writer |
| C3-13 | Every state change records `LifecycleAdvanced` | not every state change was recorded |
| C3-14 | Retention for `unmanaged_items` and `job_history` | these tables grew without bound |
| C3-15 | Rate budgets: a bucket allows one call at a time until the provider's headers have been seen after startup | budgets are unknown until the provider's headers arrive after startup |

Kept unchanged: the verified-deletion residual (covered by adapter contract tests).

### 0.13 Changelog (revision 1 → 2, for reference)

| # | Change | Driven by |
|---|---|---|
| C-01 | Slot quota is charged only to the job that **caused** provider-side creation. Joining an existing item is free; the charge moves to the oldest remaining claim when the charged job is released | owner decision |
| C-02 | **CDN URLs are secrets.** TorBox embeds the API key in them; neither provider binds them to an IP or revokes them on deletion. They never leave the process and are never logged | TB-N01, TB-N02, RD-N02 |
| C-03 | Capabilities split into **kind capabilities** (static) and **account capability state** (plan-dependent, learned at runtime) | TB-N03 |
| C-04 | New **delivery targets** model: `file` and `bundle` (zip at TorBox, RAR at Real-Debrid). Real-Debrid multi-file items expose only a bundle | RD discovery §4, TorBox §5 |
| C-05 | **Pending submissions** recorded before the provider call; reconciled by fingerprint on sync | TorBox §5 (interrupted submit created an item) |
| C-06 | Outbox gains `DeleteProviderItem` (by provider reference) besides `DeleteRemoteItem` | provider items without a `RemoteItem` must be deletable too |
| C-07 | Webhooks are **refresh hints only** (no item identity in the TorBox payload); refresh-on-read stays active in webhook mode; webhook handling moved into `job` so `provider` no longer calls `job` | T12 refuted (webhooks carry no item identity); `provider` must not call `job`; a missed webhook needs an on-demand refresh |
| C-08 | Lifecycle: sync table and command transitions separated; `failed` items can be superseded; new mappings (`cached`, `uploading`, `processing`) | discovery; sync and command transitions were mixed |
| C-09 | Observations are ordered by **debroxy's own clock** (Real-Debrid timestamps are mislabelled) and stale ones are dropped | RD-N03 |
| C-10 | Released claims leave the aggregate (history read model) | popular items grew without bound because released claims stayed in the aggregate |
| C-11 | Per-user submit rate limit; per-user stream cap; provider create budgets | protection of the shared account; T13 |
| C-12 | Web downloads only for provider-listed hosters; hoster catalogue operation; password part of the web fingerprint | T11 refuted; content addressing |
| C-13 | Egress pinned per provider (IP family / local address) | IPv6 dual-stack finding |
| C-14 | Error translation by provider error **code**, not HTTP status | TB-N04, RD-N08 |
| C-15 | Stall detection (no provider "stalled" terminal state exists) | TB-N07, RD addendum |
| C-16 | Bundle downloads: Range is not forwarded where the provider ignores it (TorBox zip) | TorBox §5 |
| C-17 | Generic authentication middleware in `common/server` | ddd-rules §3 import rule |
| C-18 | Usenet stays in scope as a **plan-dependent** capability; all usenet behaviour remains unverified | user decision; TB-N03 |
| C-19 | Every MUST-rule exception is tagged **ADR required** | rule exceptions had been decided without ADRs |

---

## 1. Vision and scope

### 1.1 Problem

Debrid services fetch torrents, usenet posts and hoster files on their infrastructure and serve the results over fast HTTP. An account is paid, bound to one API key, and providers watch for account sharing (e.g. many IPs on one account).

### 1.2 Goal

debroxy is a self-hosted proxy:

- Several **Users** share one **Provider Account** per **Provider**.
- All provider traffic (API calls and file bytes) leaves from **one IP address** per provider (§9.3).
- Users are isolated: they see and control only their own **Jobs**.
- debroxy stores **no files**: bytes stream from the provider CDN through debroxy to the user.
- debroxy offers only what the provider API can do. Differences show up as **Capabilities**.

### 1.3 In scope (v1)

| Area | Included |
|---|---|
| Job kinds | Torrents (magnet, `.torrent`), web downloads (provider-listed hosters). Usenet is deferred to v2 (C8-01) |
| Providers | TorBox, Real-Debrid. Several providers per deployment, one account each |
| Provider selection | Explicit per job |
| Users | Semi-trusted, strictly isolated; roles Admin and User |
| Authentication | Username + password; external OIDC |
| Sharing | Same content held once at the provider, shared through **Claims** |
| Quota | Per-user **Slot Quota** (charge holder only; all claimants once the charge holder is gone, C3-01); per-user submit rate; per-user concurrent streams |
| File access | **Signed Links** to a **Delivery Target** (file or bundle) |
| Status | Refresh-on-read; provider webhooks as extra refresh triggers; periodic safety refresh |
| Persistence | Embedded SQLite (metadata only) |
| API | debroxy's own HTTP API, OpenAPI-first |

### 1.4 Out of scope (v1)

- Search, RSS, notifications to users, streaming/transcoding endpoints.
- debroxy-side job queueing (provider queues are used).
- debroxy-side retention (provider expiry is mirrored and shown).
- Emulating missing features (local zip/RAR creation, unpacking RAR bundles).
- Pooling several accounts of one provider; automatic provider routing.
- Provider-compatible facades; a web UI.
- Per-file delivery for Real-Debrid multi-file items (needs one provider item per file; OQ-12).
- Real-Debrid web/hoster links (DEC-16).
- Usenet submission (`.nzb` file and NZB URL); deferred to v2 (C8-01). The model keeps the `nzb` kind.

### 1.5 Risks

| ID | Risk | Mitigation |
|---|---|---|
| RISK-01 | Provider terms may forbid account sharing | Operator's responsibility, documented |
| RISK-02 | Provider limits on concurrent downloads from one IP | Per-user and global stream caps; rate budgets (§9.4) |
| RISK-03 | All bytes pass through the proxy host | Operator sizing |
| RISK-04 | A Signed Link is a bearer capability | Short TTL; ownership and user status re-checked per request |
| RISK-05 | **A leaked CDN URL leaks the TorBox API key** and cannot be revoked by deleting the item | CDN URLs never leave the process (§7.4, NFR-03); key rotation procedure documented for operators |
| RISK-06 | Provider behaviour changes silently (undocumented fields, states) | Tolerant reader (DEC-09), unknown-state logging, recorded fixtures as regression tests |
| RISK-07 | Webhooks are unauthenticated at the provider side (no signature). Anyone who learns the path secret can trigger refreshes and spend the shared rate budget | Secret path (≥ 256 bit), constant-time check; webhooks only trigger a refresh, never carry state; webhook refreshes coalesced to a minimum interval per provider (§5.6); secret rotation documented |
| RISK-08 | Moving the claims of a merged item (DEC-36) takes each claim off the merged item before it joins the surviving one, in separate transactions, because a JobID is unique across items. A crash between the two loses that one job: it is neither claimed nor in history, and until then clients briefly see it missing rather than failed | Accepted for v1 (owner decision). The content stays tracked, so the user can resubmit and joins the surviving item |

---

## 2. Ubiquitous language (glossary)

| Term | Definition |
|---|---|
| **User** | A person allowed to use debroxy. Has one **Role** (`admin`, `user`). |
| **Actor** | The authenticated User performing an action (ID + Role), as seen by a context. |
| **Session** | A logged-in state identified by an opaque **Session Token**. |
| **Provider** | A debrid service type: `torbox`, `realdebrid`. |
| **Provider Account** | One configured account of a Provider, with **Provider ID**, API key, User-Agent, egress and limits. |
| **Kind Capability** | A feature the provider type can offer at all (static per Provider). |
| **Capability State** | Whether this account can currently use a Kind Capability: `available` (confirmed by a successful call), `unavailable` (e.g. plan restriction), `unverified` (never confirmed). |
| **Hoster Catalogue** | The provider's list of supported web hosters (domains/regex, status, daily limits). |
| **Source** | What a User submits: magnet, `.torrent` bytes, `.nzb` bytes, NZB URL, or web URL (+ optional password, used only by the first submitter). |
| **Job Kind** | `torrent`, `nzb`, `web`. |
| **Fingerprint** | Identity of a Source's content used for deduplication (§5.1). |
| **Submission** | The phase of a Remote Item between the start of a create and the provider's confirmation (state `submitting`). Other users can already claim it. |
| **Remote Item** | The single item at the Provider Account for one Fingerprint; shared by all Jobs that claim it. |
| **Provider Item ID** | The provider's identifier of a Remote Item (string). |
| **Job** | A User's claim on a Remote Item; the core noun. |
| **Claim** | The relation between an unreleased Job and its Remote Item. |
| **Active Run** | The period during which a Remote Item is in an active Lifecycle state (including `submitting`), started by a submission or a revive. |
| **Charge Holder** | The Job whose submission started the current Active Run. |
| **Slot Charge** | A Claim that counts against its owner's Slot Quota: the Charge Holder's claim; or, when the Charge Holder has been released during the Active Run, every remaining claim. |
| **Slot Quota** | Per-user maximum number of active Remote Items on which the user holds a Slot Charge. Exceeding it never stops a download; it only blocks new charged submissions. |
| **Lifecycle** | Provider-neutral state machine of a Remote Item (§5.2). |
| **Remote File** | A file inside a Remote Item: Provider File ID (string), path, size. Known only once the provider reports it. |
| **Delivery Target** | What a Signed Link points to: `file` (one Remote File) or `bundle` (the provider-built archive of the whole item). |
| **Bundle Format** | `zip` (TorBox) or `rar` (Real-Debrid). |
| **Telemetry** | Volatile progress data (progress, speed, ETA, peers, stalled). Not part of the Lifecycle. |
| **Stall** | No progress for a configured time while in an active state. |
| **Sync** | Updating Remote Items from provider state. |
| **Observation** | One provider snapshot of a Remote Item, translated to neutral terms, stamped with debroxy's observation time. |
| **Unmanaged Item** | An item at the Provider Account that debroxy does not track and no submission matches. |
| **Signed Link** | Expiring HMAC-signed URL to one Delivery Target, served through debroxy. |
| **CDN URL** | The provider's direct download URL. A **secret**: never shown, never logged. |
| **Outbox Message** | A side effect recorded in the same transaction as a state change, executed later at least once. |
| **Outbox Processor** | The driving adapter that runs `ProcessOutbox` every second, executing due Outbox Messages (C10-08). |
| **Provider API** | One provider's HTTP API as `provider` sees it; its driven port is `ProviderAPIGateway`, one per Provider Account (C10-08). |
| **Session Generation** | A per-user counter; a Session is valid only while it was created under the user's current generation. Raising it revokes every session at once (§4.4). |
| **Refresher** | The part of `job` that coordinates refreshes per provider: single flight, back-off, the forced-refresh limit and the webhook window (§5.6). |
| **Scheduler** | The driving adapter that runs the periodic commands of each context (§5.4, §4.2). |

---

## 3. Bounded contexts

### 3.1 Overview

| Context | Subdomain | Owns | Persistent state |
|---|---|---|---|
| `identity` | Supporting | Users, roles, credentials, OIDC links, sessions | yes |
| `provider` | Generic (anti-corruption layer) | Provider Accounts, capability state, hoster catalogues, per-provider HTTP behaviour, translation of APIs and webhooks | capability state only (`provider_capability_state`); everything else in memory (DEC-21) |
| `job` | **Core** | Remote Items (incl. submissions), Jobs, Lifecycle, dedup, quotas, sync, outbox | yes |
| `delivery` | Supporting | Signed Links, byte streaming | no |

### 3.2 Context map

```
              ┌────────────┐
 HTTP ───────►│  identity  │◄──── common/server auth middleware (wired in cmd)
              └─────▲──────┘◄──────────────┐
                    │ UserStatusChecker    │ UserDirectory
┌──────────┐ JobTargets          ┌─────────┴──┐
│ delivery │────────┼──────────►│    job     │◄── webhook HTTP adapter (job/adapters/in/webhook)
└────┬─────┘        │           └─────┬──────┘
     │ DeliveryStreamOpener         │ ProviderGateway (incl. TranslateWebhook)
     ▼                              ▼
┌─────────────────────────────────────────────┐
│                  provider                   │──► TorBox / Real-Debrid
└─────────────────────────────────────────────┘
```

| Consumer → Supplier | Consumer port (`ports/out`) | Supplier `ports/in` | Relationship |
|---|---|---|---|
| job → provider | `ProviderGateway` | provider operations | Customer/Supplier; provider is the ACL |
| job → identity | `UserDirectory` | identity query `UserDetails` | Customer/Supplier (C9-02) |
| delivery → job | `JobTargets` | job query `DeliveryTarget` | Customer/Supplier |
| delivery → provider | `DeliveryStreamOpener` | provider operation `OpenDeliveryStream` | Customer/Supplier (C5-03) |
| delivery → identity | `UserStatusChecker` | identity query `UserStatus` | Customer/Supplier |
| all HTTP adapters → identity | `common/server.Authenticator` (interface) | identity query `AuthenticatedActor` | Conformist; wired in `cmd` (DEC-22) |

- **DEC-01** Cross-context calls follow ddd-rules DEP-4.
- **DEC-02** `provider` exposes operations, not CQRS (ddd-rules A-10).
- **DEC-23** The dependency graph between contexts is **acyclic**: `delivery → job → provider`, `delivery → provider`, `* → identity` (used today by `delivery` and, since C9-02, by `job`). Webhook requests enter through `job` (command `HandleProviderWebhook`), which asks `provider` to translate them.
- **DEC-22** `common/server` defines `Authenticator interface { Authenticate(ctx, credential) (auth.Principal, error) }` and a generic middleware. The adapter around `identity`'s `AuthenticatedActor` query lives in `identity` and is built by `identity/service`; `cmd` only passes it to the server (ddd-rules §3 import rule). Each context's HTTP adapter converts `auth.Principal` to its own `ports/in.Actor`.

---

## 4. Context: `identity`

Unchanged from revision 1 §4 except where noted.

### 4.1 Model

**Aggregate `User`**: `id` (UUIDv7), `username` (3–32 `[a-z0-9._-]`, unique, **immutable**, DEC-03), `passwordHash` (argon2id, optional), `oidcIdentity{Issuer, Subject}` (optional, unique), `role`, `status` (`active` | `disabled`), `createdAt`.
Invariants: at least one login method; the last active admin can't be disabled or demoted (`ErrLastAdmin`); passwords ≥ 12 chars.

**Aggregate `Session`**: `id`, `userID`, `tokenHash` (SHA-256), `createdAt`, `lastSeenAt`, `expiresAt` (absolute + idle, from a configured Factory), `revokedAt?`.

### 4.2 Use cases

| Use case | Type | Actor | Behaviour |
|---|---|---|---|
| `BootstrapAdmin` | command | CLI, or `serve` from the environment (C12-03) | Only while no user exists |
| `CreateUser`, `ChangeUserRole`, `DisableUser`, `EnableUser`, `ResetUserPassword`, `LinkOIDCIdentity`, `UnlinkOIDCIdentity` | command | admin | Disabling or resetting revokes sessions. A disabled user's jobs stay; Signed Links stop working |
| `ChangeOwnPassword` | command | user | Needs the current password; revokes other sessions |
| `LogInWithPassword` | command | anonymous | Token generated by the adapter (ddd-rules A-3); uniform `invalid-credentials` |
| `LogInWithOIDC` | command | anonymous | Authorization code + PKCE in the adapter; only pre-linked users (DEC-04) |
| `LogOut`, `TouchSession` | command | user / system | |
| `AuthenticatedActor`, `UserStatus`, `Users`, `UserDetails` | query | system / admin | `UserDetails` also serves a user's own record (C9-02) |
| `PruneSessions` | command | system (hourly) | Deletes sessions that are revoked or past either expiry (C10-02) |

### 4.3 Transport details

Bearer token (`Authorization: Bearer`) or cookie `debroxy_session` (HttpOnly, Secure, SameSite=Strict). Cookie-authenticated state changes need a matching `Origin`. Login throttling per username and per IP (DEC-05).


### 4.4 Details (C10-02)

- **Revocation without touching sessions.** `User.sessionGeneration` starts at 0; each Session stores the generation it was created under. Disabling a user, an admin password reset and a password change raise it; authentication rejects a session whose generation differs. Each of these flows changes only the User (ddd-rules D-11). A password change then moves the actor's current session to the new generation in a second transaction, so only that session survives.
- **Tokens and cookie.** A session token is 32 random bytes, base64url, generated by the adapter (ddd-rules A-3) and stored as its SHA-256. The cookie `debroxy_session` is always `HttpOnly; Secure; SameSite=Strict; Path=/` and lives for the absolute lifetime; the login response's `expiresAt` is the idle expiry. Logout clears the cookie.
- **Login throttling (DEC-05).** Per username and per client IP: 5 free failures, then a wait of 1 s doubling up to 15 min, reset by a success, kept in memory (at most 10 000 entries). A throttled attempt answers 429 `login-throttled` before any password check. Unknown and disabled users verify a decoy hash, so timing reveals no account.
- **Credentials.** Usernames are lowercased on input. Passwords have at least 12 characters and at most 1024 bytes. Hashes are argon2id with 19 MiB, t = 2, p = 1 and carry their parameters, so they can be raised later.
- **Admin routes.** `PATCH /api/v1/admin/users/{id}` takes `role` and/or `status`, applied as two commands in that order (not atomic). `PUT /api/v1/admin/users/{id}/password` resets a password. `PUT` and `DELETE /api/v1/admin/users/{id}/oidc-identity` link and unlink an OIDC identity; `CreateUser` takes a password, an OIDC identity, or both, so OIDC-only users exist; unlinking the identity of a user without a password is `no-login-method`. A user may read their own record with `GET /api/v1/admin/users/{id}`.
- **OIDC flow (DEC-04).** `GET /api/v1/auth/oidc/login` redirects to the provider with `state`, `nonce` and a PKCE S256 challenge; the three values travel in a 10-minute `HttpOnly; Secure; SameSite=Lax` cookie scoped to `/api/v1/auth/oidc`, so debroxy keeps no flow state. The callback checks the state, redeems the code with the verifier, verifies the ID token against the discovered keys and the nonce, and logs in the user linked to the exact `(issuer, subject)`. Success redirects to the public base URL with the session cookie; every failure answers 401 `invalid-credentials` and logs the cause. Discovery runs on the first login, so an unreachable provider does not stop startup. OIDC logins skip the password throttle; the identity provider throttles its own.
---

## 5. Context: `job` (core)

### 5.1 Model

**Aggregate `RemoteItem`** (`job/domain/remoteitem`): the consistency boundary for dedup, claims, submission and the slot charge (DEC-06). It exists **from the moment a create starts**, before the provider is called (state `submitting`, C4-01). Every flow in this context changes exactly one `RemoteItem` per transaction (ddd-rules D-11).

| Field | Type | Notes |
|---|---|---|
| id | `RemoteItemID` (UUIDv7) | |
| providerID | `ProviderID` | |
| providerItemID | `ProviderItemID?` (string) | absent while `submitting` |
| providerHash | string | the hash the provider reports: btih for torrents; MD5 of the submitted URL string for TorBox web. Used to complete interrupted submissions |
| kind | `Kind` | `torrent`, `nzb`, `web` |
| fingerprint | `Fingerprint` | dedup key (§5.1 Fingerprint) |
| source | `Source` | kept while `submitting`, and while `deleting` without a provider ID (a create may still be in flight); cleared once a provider ID is recorded or the item is `deleted` (C5-02) |
| name, sizeBytes | | from observations (initially from the Source) |
| lifecycle | `Lifecycle` | §5.2 |
| failureReason | `FailureReason?` | e.g. `provider-rejected{detail}`, `submission-lost`, `provider-failed{detail}` |
| files | `[]RemoteFile{FileID string, Path, SizeBytes}` | empty until the provider reports files |
| bundle | `BundleAvailability{Format zip\|rar, RangeSupported bool}`? | §5.9 |
| perFileDelivery | bool | false for Real-Debrid items with more than one selected file |
| expiresAt | time? | provider expiry |
| telemetry | `Telemetry{Progress 0–1, BytesPerSecond, ETA?, Peers?, Stalled, StallSource provider\|debroxy}` | |
| lastProgressAt | time | debroxy clock; set on every state change and on progress increase |
| lastObservationAt | time | debroxy clock; stale observations are rejected (DEC-09b) |
| submission | `Submission{StartedAt, AttemptStartedAt?, Attempts, NotBefore?, FollowUpsDue []time, DeferredBehind RemoteItemID?}`? | present while `submitting`, and while `deleting` without a provider ID. `AttemptStartedAt` = start of the current provider create attempt, empty while no attempt is in progress (C5-02, C6-02). `NotBefore` = earliest time for the next attempt after a budget block (C6-02) |
| deletedAt | time? | set by `ConfirmDeleted`, `SupersedeDeletion` and the no-provider-call paths to `deleted`; used by the late deletion check (C5-01) |
| claims | `[]Job` | unreleased jobs only |
| chargeHolder | `JobID?` | the job that started the current Active Run |
| chargeHolderReleased | bool | the charge holder was released during the current Active Run → every claim is charged (C3-01) |

Derived: `IsChargedTo(userID)` = item active **and** (owner of `chargeHolder` = user, **or** `chargeHolderReleased` and the user holds a claim).

**Entity `Job`** (inside `RemoteItem`): `id` (UUIDv7, chosen by the caller), `ownerID`, `ownerUsername` (snapshot), `displayName`, `claimedAt`, `origin` (`created` | `joined`).
Released jobs move to the `job_history` read model, visible to their owner and admins. `JobReleased.finalState` is the Remote Item's Lifecycle state at the moment of release (C5-11).

**Value object `Fingerprint`** (DEC-07)

| Source | Fingerprint |
|---|---|
| Magnet | `btih:<40 hex lowercase>` (base32 converted); `btmh:<hex>` only if no btih |
| `.torrent` | SHA-1 of the bencoded `info` dictionary → `btih:`; v2-only → `btmh:` |
| `.nzb` | `nzb:<sha256 of raw bytes>` (ASM-U03); **not submittable in v1** (C8-01) |
| NZB URL | `nzburl:<normalized URL>`; **not submittable in v1** (C8-01); in v2 only once ASM-U02 is verified |
| Web URL | `url:<normalized URL>`. **The password is not part of the fingerprint**: TorBox keeps one item per URL, so password variants cannot be held apart. The first submission's password is used; later joiners' passwords are ignored, and the API says so |

URL normalization: scheme and host lowercased, default port and fragment removed, query kept. The provider always receives the URL exactly as the first submitter gave it.

**Aggregate `SlotQuota`**: `{UserID, MaxActiveJobs}`; default from configuration.

### 5.2 Lifecycle state machine (ddd-rules SM-1…SM-4)

| State | Meaning | Active | Claimable | Terminal |
|---|---|---|---|---|
| `submitting` | Create started; provider item not yet confirmed (C4-01) | ✓ | ✓ | |
| `queued` | Waiting at the provider | ✓ | ✓ | |
| `resolving_metadata` | Magnet or NZB being resolved | ✓ | ✓ | |
| `awaiting_file_selection` | Real-Debrid waits for selection (the adapter selects immediately) | ✓ | ✓ | |
| `downloading` | Provider downloading (incl. stalled) | ✓ | ✓ | |
| `processing` | Post-download work at the provider | ✓ | ✓ | |
| `ready` | Delivery targets can be downloaded | | ✓ | |
| `failed` | Submission or provider failed | | | |
| `gone` | Disappeared upstream | | | |
| `deleting` | Last claim released; provider deletion pending (or awaiting the outcome of an in-flight create) | | | |
| `deleted` | | | | ✓ |

**Sync transitions** (`Observe`, only for items with a provider item ID in the states below):

| From \ To | queued | resolving_metadata | awaiting_file_selection | downloading | processing | ready | failed |
|---|---|---|---|---|---|---|---|
| queued | | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| resolving_metadata | ✓ | | ✓ | ✓ | ✓ | ✓ | ✓ |
| awaiting_file_selection | ✓ | | | ✓ | ✓ | ✓ | ✓ |
| downloading | ✓ | | | | ✓ | ✓ | ✓ |
| processing | | | | ✓ | | ✓ | ✓ |
| ready | | | | | | | |
| failed | | | | | | | |

Observations for items in `submitting`, `gone`, `deleting` or `deleted` never go through `Observe`; the refresh pipeline (§5.6) routes them explicitly.

**Command transitions** (each with its own method, rule and test):

| Method | From | To |
|---|---|---|
| `CompleteSubmission(providerItemID, observation)` | `submitting` | the observation's mapped state |
| `CompleteSubmission` | `deleting` (released while submitting) | stays `deleting`; provider ID recorded; outbox `DeleteRemoteItem` |
| `FailSubmission(reason)` | `submitting` | `failed` |
| `DiscardSubmission(reason)` (definite failure while only the creator holds a claim) | `submitting` | `deleted` (no provider item; the creator's job goes to history as failed, C6-08) |
| `FailSubmission(reason)` | `deleting` (no provider item) | `deleted` |
| `DeferSubmission(blocker)` | `submitting` | `submitting` (records `DeferredBehind`) |
| `Revive(observation)` | `failed`, `gone` (provider item reappears active or ready, **no claimable item conflicts on fingerprint, provider hash or provider item ID**, **no `deleting` item holds that provider item ID** (C6-01), and the observation's `ObservedAt` is **later than the `deletedAt` of every item that held that provider item ID** (C7-01); with several candidates, the newest by `created_at`) | the observation's mapped state |
| `MarkGone` | any state with a provider item ID except `deleting`/`deleted` | `gone` |
| `RequestDeletion` (last claim released) | `submitting` | `deleting` (no provider item yet) |
| `RequestDeletion` | any other active state, `ready`, `failed` with provider item | `deleting` |
| `RequestDeletion` | `gone`; `failed` without provider item | `deleted` (no provider call) |
| `ConfirmDeleted` / `SupersedeDeletion` | `deleting` | `deleted` |

Rules:
- Forbidden sync transitions are not applied; the anomaly is logged (DEC-09).
- **Uniqueness among claimable items** (partial unique indexes): (`provider_id`, `fingerprint`), (`provider_id`, `provider_hash`), and (`provider_id`, `provider_item_id`) where the provider item ID is set. A `failed`/`gone`/`deleting` item does not block a new item for the same content or the same provider ID. No cross-aggregate revive or cancellation exists.
- `processing → downloading` exists for usenet repair (ASM-U04). It stays in the table for v2; in v1 it is only reachable if a provider reports it for another kind.

### 5.3 Domain behaviour of `RemoteItem`

| Method | Rules |
|---|---|
| `StartSubmission(id, provider, source, fingerprint, providerHash, firstJob, now)` | New item in `submitting`; `firstJob.origin = created`; it becomes `chargeHolder` |
| `Claim(job, now)` | Only in claimable states (`ErrRemoteItemUnavailable` otherwise). One unreleased job per owner (`JobAlreadyExistsError{ExistingJobID}`). Same `JobID` already present → `ErrDuplicateJobID` (the use case turns it into an idempotent answer). `origin = joined`. The new claim is charged iff the item is active and `chargeHolderReleased`; the quota check for that case happens in the use case |
| `CompleteSubmission(providerItemID, observation, now)` | §5.2. Precondition: the late deletion check of §5.5 step 5 passed. Clears `submission` and `source`; applies the observation as `Observe` would. If the resulting state is not active, clears the charge fields. Records `SubmissionCompleted` and `LifecycleAdvanced` |
| `FailSubmission(reason, now)` | §5.2. Records `SubmissionFailed{reason, claimants}` |
| `DeferSubmission(blocker, now)` | The provider returned a provider item ID that a `deleting` item still holds (§5.5). Clears `AttemptStartedAt` (C7-02). Records `SubmissionDeferred` |
| `DiscardSubmission(reason, now)` | §5.2. Only the creator's claim exists. Records `SubmissionFailed{reason}`, `JobReleased{job, finalState = submitting}` (history entry with the failure reason) and `LifecycleAdvanced` (C7-04) |
| `Release(jobID, actor, now)` | Owner or admin. Records `JobReleased{job, finalState}`. **First:** if no claims remain → `RequestDeletion`, `LastClaimReleased`. **Otherwise**, if the released job is the charge holder and the item is active: clear `chargeHolder`, set `chargeHolderReleased = true`, record `ChargeSpreadToClaimants{userIDs}` (C3-01; spread is unconditional, even beyond quota) |
| `Observe(observation)` | Rejects observations older than `lastObservationAt`. Applies the sync table and the observed fields. Sets `lastProgressAt` on every state change and progress increase. Clears a debroxy-detected stall on the next progress increase or state change; a provider-reported stall is taken as reported. When the Active Run ends, clears both charge fields |
| `Revive(observation, now)` | §5.2. No job is added. The resulting state's charge fields: active → `chargeHolder = none`, `chargeHolderReleased = true` (every claimant is charged); non-active → both cleared |
| `DetectStall(now, threshold)` | Only in `resolving_metadata` and `downloading`. No progress since `threshold` → `Stalled = true`, `StallSource = debroxy`; `StallDetected` once per run |
| `MarkGone`, `RequestDeletion`, `ConfirmDeleted`, `SupersedeDeletion` | §5.2 |

**Events:** every state change records `LifecycleAdvanced{from, to, cause}`; the other events are listed with their methods.

Policy `CanActorSeeJob(actor, job)`: owner or admin; repositories enforce it (ddd-rules R-3).

### 5.4 Use cases

| Use case | Type | Actor | Summary |
|---|---|---|---|
| `SubmitJob` | command | user | §5.5 |
| `ExecuteSubmission` | command | system (after `SubmitJob`; deferred and retried submissions) | §5.5 steps 4–6 |
| `ReleaseJob` | command | owner / admin | `RemoteItem.Release`; outbox `DeleteRemoteItem` when the last claim of an item with a provider item goes |
| `RenameJob` | command | owner | |
| `RefreshProviderState` | command | system / user (forced) / webhook | §5.6 pipeline |
| `HandleProviderWebhook` | command | webhook adapter | §5.6 |
| `ExpireSubmissions` | command | system (every minute) | `submitting` items **without** `DeferredBehind` **with an attempt in progress** whose `AttemptStartedAt` is older than the submission timeout → `FailSubmission(submission-lost)`. Items waiting for `NotBefore` (budget block) have no attempt in progress and are not expired (C6-02). Deferred items are exempt; they fail with `blocked-by-deletion` when their blocker's deletion is parked (C5-04). `deleting` items without a provider ID: past the timeout of an attempt in progress → `FailSubmission` (→ `deleted`); **without an attempt in progress** (released while deferred or while waiting for `NotBefore`) → `FailSubmission` (→ `deleted`) at the next run (C7-03) |
| `ResumeDeferredSubmissions` | command | system (every minute) | `submitting` items whose blocker reached `deleted`, and `submitting` items without an attempt in progress whose `NotBefore` has passed (also after a restart) → `ExecuteSubmission` again (C6-02) |
| `DetectStalls` | command | system (periodic) | §5.3 |
| `ExecuteRemoteItemDeletion` | command | outbox processor | §5.7 |
| `SetSlotQuota` | command | admin | |
| `JobsOfUser`, `JobDetails`, `JobHistory` | query | user | |
| `AllJobs`, `UnmanagedItems`, `ParkedOutboxMessages` | query | admin | |
| `CachedAvailability` | query | user | needs `CachedCheck` |
| `Providers`, `ProviderHosters` | query | user | provider `ListProviders` / `HosterCatalogue` through `ProviderGateway` (C9-03) |
| `ProbeProviderCapabilities` | command | admin | provider `ProbeCapabilities` through `ProviderGateway` (C9-03) |
| `DeliveryTarget` | query | delivery | §5.9 |
| `QuotaUsage` | query | user | Shown by `GET /api/v1/me` together with the user from `UserDirectory` (C9-02). `slotsUsed` counts active items where `IsChargedTo(user)`, `submitting` included. A user without a `SlotQuota` row uses `job.defaultSlotQuota`; a new charged claim is allowed only while `slotsUsed < MaxActiveJobs` (C5-11) |

### 5.5 Submission flow

Command: `SubmitJob{Actor, JobID, ProviderID, Source, DisplayName?}`

1. **Idempotency.** If `JobID` already exists (as a claim, or in history), return its current representation: 201/202 for the same owner and the same fingerprint; `conflict` otherwise. No other step runs. This is a plain read before any write transaction.
2. **Validate.** Provider exists and is enabled. The kind capability exists (`capability-not-supported`), is enabled in v1 (`capability-not-enabled`), and isn't `unavailable` for the account (`capability-unavailable-for-plan`). Web URLs must match the Hoster Catalogue (`unsupported-hoster`). Compute the Fingerprint and provider hash in `job` (§5.10).
3. **Join or start: one transaction.**
   - A claimable item exists for (provider, fingerprint), **including `submitting` items**:
     - item active and `chargeHolderReleased` → check the slot quota (`slot-quota-exceeded`);
     - otherwise joining costs nothing (no quota or rate check);
     - `Claim`. If the item is `submitting`, respond **202 `submission-pending`**, otherwise 201. **Done; no provider call.**
   - None exists → check the user's **submit rate** (DEC-24) and **slot quota** (the item will be active and charged to them), then `StartSubmission`. This is the only quota check on the create path. Respond 202 if the provider call below doesn't finish within the request deadline (default 10 s), else 201 with the completed item. `ExecuteSubmission` runs detached from the request context, so a create that finishes after the deadline still records its outcome through step 5 (C5-11).
4. **Provider call** (`ExecuteSubmission`, outside any transaction, ddd-rules A-9): in a short transaction, start an attempt **only if none is in progress** (set `AttemptStartedAt`, increment `Attempts`), then `ProviderGateway.CreateRemoteItem(source)`. If the rate budget blocks the call **before it is sent** (gateway result `not-sent{retryAfter}`), nothing was sent: a second short transaction clears `AttemptStartedAt`, undoes the increment and sets `NotBefore = now + retryAfter`; `ResumeDeferredSubmissions` runs it again (C5-08, C6-02). HTTP 429 from the provider on a create is **not** `not-sent`: it counts as an ambiguous failure (C6-03).
5. **Outcome: one transaction on the same item.**
   - **Success, provider item ID P:**
     - another **claimable** item already holds P (the provider deduplicated into an item debroxy already tracks; possible because content can enter under a different fingerprint, e.g. a magnet vs. a hash variant) → `FailSubmission(merged-into{otherItem})` for this item; the use case then claims the other item for each of this item's claims, **one transaction per claim** (each changes only the other item), and releases them here. A claim the other item refuses (its owner already has a job there, or the claim would be charged and the owner has no free quota) is released into history with `merged-into-refused{reason}` (C6-09). Rare; documented as eventual consistency (DEC-36);
     - a **`deleting`** item still holds P (deletion not yet executed) → `DeferSubmission(blocker)`; `ResumeDeferredSubmissions` repeats the create after the blocker is `deleted`;
     - **late deletion check (C5-01):** an item that held P has `deletedAt ≥ AttemptStartedAt` (its provider DELETE may have removed P after our create returned it) → don't complete; clear `AttemptStartedAt` (C7-02) and run `ExecuteSubmission` again right away (the provider then returns a new ID; observed, torbox.md §7). The check only reads the other item (D-11 holds);
     - otherwise → `CompleteSubmission(P, observation)`. Any `failed`/`gone` item with P stays as it is (its claimants keep their history; they can resubmit and will join this item).
   - **Definite failure** (classified provider error) → if the item has only the creator's claim: `DiscardSubmission(provider-rejected{detail})`. Within the request deadline the error goes to the submitter synchronously and no job is shown; after the deadline (the submitter already got 202) the job appears in the creator's history as failed with that reason (C6-08). Otherwise `FailSubmission(provider-rejected{detail})`; joined users see the failed job.
   - **Ambiguous failure** → the item stays `submitting`, follow-up refreshes are scheduled (§5.6), and the submitter gets 202.
6. A unique-index violation in step 3 or 5 is never surfaced; the transaction is retried from its look-up.

Errors: `invalid-source`, `upload-too-large`, `capability-not-supported`, `capability-not-enabled`, `capability-unavailable-for-plan`, `unsupported-hoster`, `submit-rate-exceeded`, `slot-quota-exceeded`, `job-already-exists`, `provider-unavailable`, `provider-rejected`.

**Slot-quota consistency.** The count reads other `RemoteItem` aggregates inside the same `BEGIN IMMEDIATE` transaction and changes only one. Relies on ASM-S01 (SQLite single writer).

### 5.6 Sync

**Observation** = `{ProviderItemID, Kind, ProviderHash, State, Name, SizeBytes, Files?, Bundle?, PerFileDelivery, ExpiresAt?, Telemetry, FailureReason?, ObservedAt}`; `ObservedAt` is debroxy's clock at request start.

**`RefreshProviderState{ProviderID, Cause}`**, `Cause` ∈ `read` (MaxAge 10 s), `forced` (0; per-user rate limited), `webhook` (coalesced: refresh immediately on the first webhook, then **exactly one trailing refresh** at the end of the 30 s window if more webhooks arrived during it; a trailing refresh opens a new window; C6-06), `safety`, `follow-up` (skipped when an authoritative sync younger than the follow-up's due time already ran). Single-flight per provider; all causes share the rate budgets. During a refresh back-off (C5-07), `read` doesn't call the provider; `webhook`, `safety` and `follow-up` keep their own pacing, and their outcomes reset or extend the back-off (C6-06).

**Refresh pipeline (single source of truth)**:
1. `ProviderGateway.ListRemoteItems` → observations, with `Complete = true` only if every page and kind was listed without error. Only a complete listing updates `last_authoritative_sync_at`.
   A failed or incomplete refresh starts a per-provider **back-off** (5 s doubling to 2 min); during it, refresh-on-read doesn't call the provider and reads answer from stored state with `stale: true` in the response (C5-07).
2. **Known live items:** observation's provider ID held by a claimable item → `Observe`.
3. **Interrupted submissions**: remaining observations whose `providerHash` matches a `submitting` item of that provider → the same outcome logic as §5.5 step 5 (`CompleteSubmission` / `DeferSubmission`). Uniqueness of `(provider, provider_hash)` among claimable items makes the pairing 1:1. Remaining follow-ups of that item are cancelled.
   The same applies to `deleting` items without a provider ID (released while submitting): they record the provider ID and write `DeleteRemoteItem`.
4. **Reappearing items:** remaining observations whose provider ID belongs to a `failed`/`gone` item and whose state is active or ready → `Revive`, under the conditions of §5.2 (no conflict on any unique key, no `deleting` holder of the provider ID (C6-01), newest candidate). A blocked revive is logged; the observation is dropped if a `deleting` item holds the ID (step 5), otherwise it goes to step 6.
5. **Observations for `deleting` items** (deletion not yet propagated) → ignored.
6. **Leftovers** → Unmanaged Items (DEC-10): remaining observations whose provider item ID **no debroxy item holds**, plus revives blocked by a claimable item. An observation of a `failed`/`gone` item that is neither active nor ready is dropped as tracked (C10-04, ADR 0019).
7. **Absence** (only if `Complete`): claimable items with a provider ID missing from the observations → `MarkGone`. Grace: items created, completed or revived within the absence grace (default 60 s, configurable per provider).

**Triggers**
- **Refresh-on-read** before `JobsOfUser`, `JobDetails`, `AllJobs`; it stays on in webhook mode. It refreshes the providers of the jobs the read returns (read, refresh, read again), so a user without jobs costs no provider call (C10-05).
- **Webhook** → `HandleProviderWebhook` → `ProviderGateway.TranslateWebhook` (constant-time secret check) → `RefreshProviderState{Cause=webhook}`.
- **Follow-ups after an ambiguous create:** +30 s, +2 min, +5 min, +10 min, +15 min; cancelled when the submission resolves.
- **Safety refresh:** every 5 min while any item is active (incl. `submitting`), otherwise every 30 min.
- **Periodic:** `ExpireSubmissions` (timeout 20 min > the follow-up schedule), `ResumeDeferredSubmissions`, `DetectStalls` (threshold 30 min).

**Per-provider list strategy** (adapter):
- TorBox: `mylist?bypass_cache=true` per usable kind plus the queued list.
- Real-Debrid: walk `/torrents` pages for IDs, then `/torrents/info/{id}` per non-terminal item (list status is stale). A failed page → `Complete = false`.

### 5.7 Outbox (pattern: transactional outbox)

| Message | Written by | Executed by |
|---|---|---|
| `DeleteRemoteItem{RemoteItemID}` | `RequestDeletion` on an item with a provider ID; `CompleteSubmission` on a `deleting` item | `ExecuteRemoteItemDeletion` |

`ExecuteRemoteItemDeletion` never holds a transaction across a network call:
1. **Transaction 1:** load the item. Not `deleting` → message done. If a **claimable** item now holds the same provider item ID (a new submission took it over) → `SupersedeDeletion` (no provider call) → done.
2. **Provider call:** `ProviderGateway.DeleteRemoteItem`. On error, a lookup decides (verified deletion).
3. **Transaction 2:** `ConfirmDeleted` (sets `deletedAt`). A claimable item that completed onto the same provider ID cannot exist here: completion defers while this item is `deleting` and runs the late deletion check afterwards (§5.5, C5-01). If one is found anyway (provider ID reuse), it is logged as an anomaly and the next refresh marks it `gone`.

Retries with exponential back-off (1 s … 1 h), parked after 20 attempts; parking also fails submissions deferred behind this item (`blocked-by-deletion`, C5-04). `ProviderGateway.DeleteRemoteItem` returns `deleted` (confirmed directly or by lookup) or an error that leads to a retry (C5-11). Real-Debrid download-history cleanup per DEC-25. The former `DeleteProviderItem` message is no longer needed: no provider item exists without a `RemoteItem` (C4-01).

### 5.8 Persistence (SQLite, sketch)

| Table | Key columns |
|---|---|
| `remote_items` | `id`, `provider_id`, `provider_item_id` (nullable), `provider_hash`, `kind`, `fingerprint`, `source_blob` (nullable; only while submitting), `state`, `failure_reason`, `name`, `size_bytes`, `bundle_format`, `bundle_range_supported`, `per_file_delivery`, `expires_at`, `telemetry_json`, `last_progress_at`, `last_observation_at`, `submission_json`, `charge_holder_job_id`, `charge_holder_released`, `created_at`, `deleted_at`. Partial unique indexes over claimable states: (`provider_id`, `fingerprint`), (`provider_id`, `provider_hash`), (`provider_id`, `provider_item_id`) where not null |
| `remote_files` | `remote_item_id`, `file_id`, `path`, `size_bytes` |
| `jobs` | unreleased claims |
| `job_history` | released jobs; retention default 180 days |
| `slot_quotas`, `submit_rate_windows` | |
| `provider_sync_state` | `provider_id`, `last_authoritative_sync_at` |
| `unmanaged_items` | pruned when unseen for 7 days |
| `outbox_messages` | processed rows pruned after 30 days |

**Storage setup:** migrations are named `NNNN_<context>_<change>.sql`. One writer connection (all write transactions `BEGIN IMMEDIATE`) and a read-only reader pool, `busy_timeout` 5 s on every connection; an exclusive lock taken at startup prevents a second debroxy process from opening the same database. The pools and the lock are created by `internal/common/sqlite`, opened once in `cmd` and passed to each context's `service`; each context still owns its tables (C6-07).

**Retention of items:** `deleted` items and their files are pruned after 30 days; `failed`/`gone` items without claims are moved to `deleting`/`deleted` by `RequestDeletion` when their last claim is released, so they don't accumulate. Each context owns its tables (DEC-13).

### 5.9 Delivery targets

| Target | Available when | Source at provider |
|---|---|---|
| `file:<FileID>` | item `ready`, file reported, `perFileDelivery = true` | TorBox torrents: `requestdl` with `file_id` (verified); TorBox web: the item's single file via `webdl/requestdl` (verified); TorBox usenet: deferred to v2 (C8-01; mapping per ADR 0011 rule 9, ASM-U01); Real-Debrid: unrestrict of the single link (single selected file only) |
| `bundle` | item `ready`, `bundle` present | TorBox torrents: `requestdl … zip_link=true` (**Range not supported**; verified for torrents only, not offered for web items in v1); Real-Debrid unrestrict of the single link when more than one file is selected (RAR, range assumed supported, ASM-R15) |

File identity is the provider file ID, never the position in the `.torrent`.


### 5.10 Details (C10-05)

- **Provider hash.** `job` computes it while validating (§5.5 step 2): the btih for torrents (none for v2-only torrents), the MD5 of the URL exactly as given for web URLs (TorBox's convention; Real-Debrid web links are not enabled in v1).
- **Unknown provider state.** A completion whose observation carries a state the adapter does not know moves the item to `queued`; the next refresh corrects it. `Observe` keeps the state on an unknown value and still updates name, size, files and telemetry (DEC-09).
- **Submit responses.** `POST /api/v1/jobs` answers 201 with the job, 202 with `{slug: "submission-pending", job}`, or, for a retry of a job released since, 200 with its history entry. The client may send its own job `id` to make retries safe; otherwise debroxy generates one. A JobID used by another user or for other content is 409 `job-id-conflict`, which reveals nothing about the other job.
- **Cached class (C11-02).** The submission records whether `SubmitJob`'s `CheckCached` reported the provider hash cached (DEC-24). Every attempt of the submission passes that class to `CreateRemoteItem`, so a cached torrent create takes from `general` on every attempt (ADR 0013 rule 4). No later attempt runs `CheckCached` again.
- **Usernames of new jobs.** `job` reads the owner's username snapshot through `UserDirectory` (C9-02).
- **Periodic commands.** The Scheduler runs, every minute: `RunFollowUpRefreshes` (takes due follow-ups; refreshes unless a complete listing started after the latest due one), `RunSafetyRefreshes` (the safety interval counts from the provider's last finished refresh of any cause), `ExpireSubmissions`, `ResumeDeferredSubmissions` (a `submitting` item with no attempt, no `NotBefore` and no blocker counts as due) and `DetectStalls`; hourly, `PruneExpiredRecords` (§5.8 retention, plus submit-rate rows older than an hour). The Outbox Processor runs `ProcessOutbox` every second. All of them stop with the process before the database closes.
- **Refresh state** (single flight, back-off, webhook windows, forced-refresh counts) is in memory and starts empty after a restart; MaxAge is measured against the stored `last_authoritative_sync_at` (ADR 0009 rule 6). A shared refresh run ends with the process, not with the request that started it.
---

## 6. Context: `provider` (anti-corruption layer)

### 6.1 Model

**`ProviderAccount`** (from configuration; runtime state in memory)

| Field | Notes |
|---|---|
| id, kind, displayName, enabled | |
| credentials | API key reference (env/file); never stored in the DB |
| httpProfile | `{BaseURL, UserAgent, Timeout, Retry, CircuitBreaker}`; **User-Agent configurable per account**, sent on API and CDN requests (DEC-14) |
| egress | `{IPFamily ipv4\|ipv6, LocalAddress?}` (DEC-26) |
| rateBudgets | per operation class (§9.4); updated from provider headers when present |
| capabilityState | map Kind Capability → `available{since}` \| `unavailable{reason, since}` \| `unverified` (C-03); persisted in `provider_capability_state` |
| hosterCatalogue | cached list (refresh 1 h) |
| webhook | `{Enabled, Secret}` |

**Capability state rules**
- Starts `unverified` for the usenet capabilities (`SubmitNZBFile`, `SubmitNZBURL`) and `available` for the rest, including TorBox `SubmitWebURL` (observed on plan 1); then loaded from `provider_capability_state` at startup.
- `unverified` capabilities can be used; the API marks them `unverified` so clients know the plan has not proven them yet.
- A provider answer meaning "plan does not allow this" (TorBox `PLAN_RESTRICTED_FEATURE`) sets `unavailable{plan}`; a success sets `available`.
- `unavailable` is re-probed only on admin request or after 24 h, never per user request.

### 6.2 Operations (driving port, ddd-rules A-10)

| Operation | Input | Output |
|---|---|---|
| `ListProviders` | — | ID, kind, display name, enabled, capabilities with state, hoster summary |
| `HosterCatalogue` | provider ID | supported hosters (name, domains, status, daily limits/usage) |
| `CreateRemoteItem` | provider ID, Source | provider item ID, provider hash, Observation |
| `ListRemoteItems` | provider ID | Observations (all usable kinds), `Complete` (C5-05) |
| `DescribeRemoteItem` | provider ID, kind, provider item ID | Observation or not-found |
| `DeleteRemoteItem` | provider ID, kind, provider item ID | `deleted` (confirmed directly or by lookup), or an error to retry (§5.7) |
| `OpenDeliveryStream` | provider ID, kind, provider item ID, target, method (`GET`/`HEAD`), client `Range`/`If-Range` | `DeliveryStream{Status, Headers (whitelisted), RangeSupported, Body io.ReadCloser}` or a neutral error. Inside: CDN-link cache (max 30 min), pinned egress, User-Agent, rate budget, Range forwarding only when supported, re-resolve once on 401/403/404/410 before the first byte (C5-03) |
| `CheckCached` | provider ID, kind, provider hashes | cached + file list where offered |
| `TranslateWebhook` | provider ID, path secret, headers, body | `WebhookSignal{RefreshRequested}` or `rejected` |
| `ProbeCapabilities` | provider ID | refreshed capability state (admin) |

A capability must be enabled in v1 before its account state is consulted; a not-enabled capability is reported as `capability-not-enabled` whatever its state (C5-11).

Provider-specific steps stay inside adapters: Real-Debrid file selection right after creation (select all, DEC-15); Real-Debrid hoster-link → unrestrict; TorBox per-kind endpoints and ID parameter names.

### 6.3 Capabilities

| Kind Capability | TorBox | Real-Debrid | Evidence |
|---|---|---|---|
| `SubmitMagnet` | ✓ | ✓ | observed |
| `SubmitTorrentFile` | ✓ | ✓ | observed |
| `SubmitNZBFile` | plan-dependent; **not enabled in v1** (usenet deferred to v2, C8-01) | ✗ | TorBox 500 `PLAN_RESTRICTED_FEATURE` on plan 1 |
| `SubmitNZBURL` | plan-dependent; **not enabled in v1** (C8-01); in v2 only once ASM-U02 is verified | ✗ | unverified (ASM-U02) |
| `SubmitWebURL` | ✓ hoster catalogue only | provider ✓ (`/unrestrict`, 281 domains), **not enabled in debroxy v1** (DEC-16) | `UNSUPPORTED_SITE` for direct links; RD-N06 |
| `PerFileDelivery` | ✓ | single-file items only | RAR packaging |
| `BundleDelivery` | ✓ zip (no Range) | ✓ rar (multi-file items) | observed |
| `CachedCheck` | ✓ | ✗ (`disabled_endpoint`) | observed |
| `Webhooks` | ✓ refresh hint only | ✗ | observed |
| `ProviderDedup` | ✓ (same ID for known content) | ✗ (new item per add) | observed; informational |

The API advertises kind capabilities, whether debroxy enables them, and the account state. Errors:
- the provider lacks the capability → `capability-not-supported` (501);
- the provider has it but debroxy v1 doesn't enable it (`SubmitNZBFile`, `SubmitNZBURL`, Real-Debrid `SubmitWebURL`) → `capability-not-enabled` (501);
- the account state is `unavailable` → `capability-unavailable-for-plan` (409).

### 6.4 Error translation (by provider code, not HTTP status — C-14)

| Provider signal | Neutral result |
|---|---|
| TorBox `AUTH_ERROR`; Real-Debrid `bad_token` (8); 401 without envelope | `provider-unavailable` + operator alert (credentials) |
| TorBox `PLAN_RESTRICTED_FEATURE` | capability state `unavailable{plan}`; `capability-unavailable-for-plan` |
| TorBox `UNSUPPORTED_SITE`; Real-Debrid `hoster_unsupported` (16) | `unsupported-hoster` |
| TorBox `ITEM_NOT_FOUND` (404, `success:true`); Real-Debrid `unknown_ressource` (7) | `not-found` |
| TorBox `DATABASE_ERROR` (500; `error`/`detail` swapped) on delete | verify with lookup (§5.7) |
| Real-Debrid `unavailable_file` (24) | `provider-rejected` |
| Real-Debrid `disabled_endpoint` (37) | kind capability treated as absent; operator log |
| FastAPI validation body (`{"detail":[…]}`, 422) | internal error (adapter bug); logged |
| Local rate budget exhausted (call not sent) | wait inside the call's deadline, then `not-sent{retryAfter}` (creates) or `provider-unavailable` with `retryAfter` (other calls) |
| HTTP 429 from the provider | idempotent calls: retry inside the deadline, then `provider-unavailable`; **creates: ambiguous failure** (C6-03) |
| Network errors, 5xx without a known code, open circuit breaker | `provider-unavailable`; for **creates** see §6.6 |
| Any other envelope error code | `provider-rejected` with sanitized detail; unknown codes logged (RISK-06) |

### 6.5 Adapter rules

- Provider JSON types are unexported; native→neutral state mapping is one table per provider (§8).
- **CDN URLs and API keys never appear in logs, errors, metrics, traces or API responses.** URLs are logged as scheme + host only. TorBox `requestdl` needs the key as a query parameter, so request URLs are redacted too.
- Real-Debrid timestamps are ignored for logic (RD-N03); TorBox `eta = 8640000` means unknown.


### 6.6 Details (C10-03)

- **Create outcomes.** *Not sent* (`not-sent{retryAfter}`): an empty create bucket, and also an open circuit breaker, no address in the pinned family, or a dial error, since nothing reached the provider. *Ambiguous*: transport errors after connecting, 429, and 5xx without a definite code (including TorBox `DATABASE_ERROR`), plus a create cut short by shutdown. *Definite*: `UNSUPPORTED_SITE`, `PLAN_RESTRICTED_FEATURE`, unknown envelope codes, 422 (also logged as an adapter bug), and rejected credentials. Rejected credentials are reported as `provider-unavailable`; `job` treats them as a definite failure (nothing was created), discards or fails the item with reason `provider-rejected`, and the submitter gets `provider-unavailable` within the deadline.
- **Disabled accounts.** Create, cached check, hoster catalogue and webhooks refuse (`provider-disabled`); list, describe, delete and `OpenDeliveryStream` keep working, so existing items are still refreshed, delivered and cleaned up.
- **Capability probe.** `ProbeCapabilities` turns every `unavailable` capability into `unverified` without calling the provider; a plan-restricted feature is tested by its next use. `ListProviders` shows no hoster summary (that would cost a provider call per listing); the catalogue has its own route.
- **TorBox lists are paged.** `mylist` honours `limit` and `offset`, so the adapter walks pages of 1000 until a short page; the page size TorBox applies without `limit` is unknown (ASM-T27).
- **Real-Debrid history sweep (DEC-25).** The adapter runs it itself, daily per enabled account, for the life of the process; the first sweep runs one interval after startup. `generated` timestamps are local time labelled UTC (RD-N03), so entries labelled older than 24 h plus a 14 h margin are deleted; CDN links keep working after their history entry is gone (RD-N02).
- **CDN answers.** Redirects are refused (`provider-unavailable`, logged once); answers other than 200, 206 and 416 are `provider-unavailable`. `OpenDeliveryStream` checks the target's delivery capability (`PerFileDelivery` or `BundleDelivery`).
- **Startup egress check (§9.3).** For each enabled account: the configured local address must be assigned to an interface, then TCP connections to the API host and to each CDN probe host (port 443 unless given), 30 s in all. `providers[].cdnProbeHosts` lists those probe hosts (`host` or `host:port`; C11-01).
---

## 7. Context: `delivery`

### 7.1 Signed Link

`<publicBaseURL>/dl/<token>/<url-escaped name>`; `token = base64url(payload) "." base64url(HMAC-SHA256(key[kid], payload))`; payload `{v, kid, uid, jid, tgt, exp}` where `tgt` is `f:<FileID>` or `b`. The key list supports rotation (first key signs, all verify). TTL configurable (default 6 h, OQ-03). A link never contains provider data.

### 7.2 Use cases

| Use case | Type | Actor | Behaviour |
|---|---|---|---|
| `IssueSignedLink` | query | owner | Target must exist per `JobTargets` (`job.DeliveryTarget`) |
| `IssueSignedLinksForAllFiles` | query | owner | One link per file, or one bundle link when `perFileDelivery = false` |
| `StreamDownload` | adapter flow | link holder | §7.3 |

### 7.3 Streaming flow (`GET`/`HEAD /dl/{token}/{name}`)

1. Verify the token (constant-time HMAC, key ID, expiry) → else 403 `invalid-signed-link` / `signed-link-expired`.
2. `UserStatusChecker`: user active.
3. Per-user stream cap (default 4) and global cap (default 64) → else 503 `too-many-streams` (DEC-18).
4. `JobTargets.DeliveryTarget(uid, jid, tgt)`: unreleased, owned, `ready`, target available → else 404/409.
5. `DeliveryStreamOpener.OpenDeliveryStream` (provider operation, C5-03). `provider` resolves and caches the CDN link, applies egress, User-Agent and budget, forwards `Range`/`If-Range` only when the target supports ranges, and re-resolves once on 401/403/404/410 before the first byte. Errors → 502 `provider-unavailable`.
6. For targets without range support, a client `Range` is ignored: 200 with the full body and `Accept-Ranges: none` (C-16).
7. Copy the status (200/206/416) and the whitelisted headers `Content-Length`, `Content-Range`, `Content-Type`, `Last-Modified`, `ETag`; set `Accept-Ranges` from `RangeSupported`. Always set `Content-Disposition: attachment; filename*=UTF-8''<name>` (Real-Debrid sends no filename). The `<name>` path segment is outside the signature: it is cosmetic and untrusted, sanitized, and falls back to the file's stored name if empty (C5-11).
8. Stream with a bounded buffer; client disconnect closes the stream and cancels the upstream request.

`user_ip` is **not** sent to TorBox: links aren't IP-bound, and omitting it lets TorBox choose the CDN region for the proxy's own IP (resolves OQ-05).

### 7.4 CDN URL secrecy (C-02)

- CDN URLs exist only inside `provider`: in its link cache and during an upstream request. No port carries them; `delivery` only receives a stream (C5-03).
- Inside `provider`, `CDNLink` is an opaque type whose `String()`, `GoString()`, `MarshalJSON()` and `LogValue()` return a redacted form; only the provider adapter's HTTP call reads the raw URL.
- If a CDN URL is suspected leaked, the operator rotates the provider API key. Deleting the item does not revoke the link.


### 7.5 Details (C10-06)

- **Token payload.** The six fields `v` (1), `kid`, `uid`, `jid`, `tgt`, `exp` (Unix seconds) joined by newlines, then base64url; signing keys have at least 32 bytes.
- **Disabled or deleted users.** Their links answer 403 `invalid-signed-link`, so a link never reveals the account's state.
- **Name segment.** Issued links carry the last path element of the file's stored name; a bundle carries the job's display name (no archive extension, since the target has no format). `Content-Disposition` uses RFC 5987 `attr-char` encoding.
- **Headers first.** The download adapter sends status and headers before the first upstream byte arrives.
- **Owner only.** Links are issued and served only for the job's owner, admins included (§10.1).
---

## 8. Provider state mapping

### 8.1 TorBox → Lifecycle

| TorBox signal | Lifecycle | Telemetry | Evidence |
|---|---|---|---|
| item in queued list | `queued` | | ASM-T20 |
| `metaDL`, `metaDL_done` | `resolving_metadata` | | ASM-T21 |
| `stalledDL`, `stalled (no seeds)` | `downloading` | `Stalled = true` | observed |
| `downloading`, `checkingResumeData`, `paused` | `downloading` | | `downloading` observed; others ASM-T21 |
| `processing`, `uploading`, `compressing` | `processing` | | web `processing` observed; others ASM-T21 |
| `completed` or `cached` with `download_present = true` | `ready` | | observed |
| `error`, `failed*`, item `error` field set | `failed` | | ASM-T22 |
| missing from authoritative listing; `ITEM_NOT_FOUND` | `gone` (`MarkGone`) | | observed |
| unknown value | keep state; log (DEC-09) | | |

### 8.2 Real-Debrid → Lifecycle

| Real-Debrid status (from `/torrents/info`) | Lifecycle | Evidence |
|---|---|---|
| `magnet_conversion` | `resolving_metadata` | seen in list only |
| `waiting_files_selection` | `awaiting_file_selection` | observed |
| `queued` | `queued` | observed |
| `downloading` | `downloading` | observed |
| `uploading`, `compressing` | `processing` | `uploading` observed |
| `downloaded` | `ready` | observed |
| `magnet_error`, `error`, `virus`, `dead` | `failed` | ASM-R16 |

---

## 9. Provider profiles

### 9.1 TorBox (observed unless tagged)

- Base `https://api.torbox.app/v1/api`, Bearer auth; `requestdl` requires `token=<key>` in the query.
- Envelope `{success, error, detail, data}`, with exceptions (FastAPI bodies, 500 business errors, 404 with `success:true`).
- Endpoints: `torrents/createtorrent` (multipart `magnet` | `file`, `allow_zip`), `torrents/mylist`, `torrents/controltorrent` (JSON `torrent_id`, `operation`), `torrents/requestdl` (`torrent_id`, `file_id` | `zip_link`), `torrents/checkcached`; `webdl/createwebdownload` (`link`), `webdl/mylist`, `webdl/controlwebdownload` (`webdl_id`), `webdl/requestdl` (`web_id`), `webdl/checkcached`, `webdl/hosters`; `queued/getqueued`; `user/me?settings=true`. Usenet endpoints ASM-U01 (not used in v1, C8-01).
- Provider dedup: known content returns the existing ID (torrents by btih, web by URL MD5).
- `files` is null until the item is finished; cached adds report files immediately.
- Expiry: `expires_at` = completion + 24 h (torrent) / + 30 days (web) on the observed plan.
- Webhook: account settings `webhook_url` + `webhook_notifications`; payload `{type, timestamp, data{title, message}}` with no item identity; no signature; fires for (non-cached) completions only.
- `cooldown_until` in account info: meaning unknown (ASM-T23).

### 9.2 Real-Debrid (observed unless tagged)

- Base `https://api.real-debrid.com/rest/1.0`, Bearer auth; errors `{error, error_code}`.
- `torrents/addMagnet` (form), `torrents/addTorrent` (PUT raw body) → 201 `{id, uri}`; `torrents/selectFiles/{id}`; `torrents/info/{id}`; `torrents` (paged, `X-Total-Count`, 204 past the end); `torrents/delete/{id}`; `unrestrict/link`; `downloads`, `downloads/delete/{id}`; `hosts/domains`.
- No provider dedup; file IDs 1-based in torrent order; files known right after creation.
- More than one selected file → one RAR link; one selected file → that file.
- List status is stale; `added` is local time labelled `Z`.

### 9.3 Egress (C-13)

- Each Provider Account MUST pin its egress: `ipFamily` (`ipv4` default) and optionally `localAddress`. API calls and CDN downloads use the same dialer, owned by `provider` (C5-03). Go's environment proxy variables are ignored for provider clients.
- Startup check: resolve the API host **and the provider's known CDN domains** (TorBox `*.tb-cdn.st`, `*.tb-cdn.io`; Real-Debrid `*.download.real-debrid.com`; list kept in the adapter, overridable in config) in the pinned family and open a TCP connection to one representative host of each (concrete names from the fixtures, e.g. `nexus-182.ceur.tb-cdn.st`); otherwise refuse to start. This proves the family and route work, not that every dynamic CDN host is reachable; that is enforced at runtime.
- At runtime, a CDN host that has no address in the pinned family fails the stream with 502 `provider-unavailable`, logs an operator alert, and is never retried over the other family.

### 9.4 Rate budgets

| Provider | Budget | Value | Source |
|---|---|---|---|
| TorBox | general API, per endpoint | 300 / 60 s | observed headers |
| TorBox | uncached torrent create; **every** web create (cached or not) | **60 / hour** | observed headers (`08-readd-same-webdl` shows a cached web re-add in this bucket) |
| TorBox | CDN zip | 60 / 60 s | observed headers |
| Real-Debrid | whole API | 250 / 60 s | ASM-R11 (no headers seen) |

CDN **file** downloads carry no rate-limit headers at either provider; they are bounded by the stream caps (DEC-18), not by a rate class (C5-08).

- Budgets are tracked per Provider Account and updated from `x-ratelimit-*` headers when present.
- **Startup:** each header-reporting bucket is **single-flight until its first headers arrive** after a restart; from then on the provider's own counters drive it. Header-less buckets (Real-Debrid) get 50 % of their static budget in the first window after a restart, since the provider may still count calls from before it.
- **DEC-24** Per-user submit rate (create path only; joins are free): default 10 per hour. With `CachedCheck`, a submit whose hash is cached counts as **cheap** and is limited separately (default 60 per hour). Both limits use a rolling hour; a submit counts when it is accepted and is not refunded if the create later fails. A failed `CheckCached` counts the submit as uncached. Web submits skip `CheckCached` and always count as uncached, because TorBox charges every web create to the 60/hour bucket (C6-04).

---

## 10. External API (OpenAPI-first)

### 10.1 Endpoints

| Method & path | Context / use case | Auth |
|---|---|---|
| `POST /api/v1/auth/login`, `POST /api/v1/auth/logout` | identity | anonymous / user |
| `GET /api/v1/auth/oidc/login`, `GET /api/v1/auth/oidc/callback` | identity | anonymous |
| `GET /api/v1/me` | job: identity `UserDetails` through `UserDirectory` + job `QuotaUsage` (C9-02) | user |
| `PUT /api/v1/me/password` | identity | user |
| `PUT /api/v1/admin/users/{id}/password`, `PUT`/`DELETE /api/v1/admin/users/{id}/oidc-identity` | identity (§4.4) | admin |
| `GET /api/v1/providers` | job `Providers` (provider `ListProviders`, C9-03) | user |
| `GET /api/v1/providers/{id}/hosters` | job `ProviderHosters` (provider `HosterCatalogue`, C9-03) | user |
| `POST /api/v1/providers/{id}/cached-availability` | job `CachedAvailability` | user |
| `POST /api/v1/providers/{id}/refresh` | job `RefreshProviderState{Forced}` (rate limited) | user |
| `POST /api/v1/jobs` | job `SubmitJob` → 201, or **202 `submission-pending`** | user |
| `GET /api/v1/jobs`, `GET /api/v1/jobs/{id}` | job queries (after refresh-on-read) | user / owner |
| `GET /api/v1/jobs/history` | job `JobHistory` | user |
| `PATCH /api/v1/jobs/{id}`, `DELETE /api/v1/jobs/{id}` | rename / release | owner (admin may release) |
| `POST /api/v1/jobs/{id}/links` | `{target: {fileId} \| {bundle: true}}` → `{url, expiresAt, rangeSupported}` | owner |
| `POST /api/v1/jobs/{id}/links/all` | links for all files, or the bundle | owner |
| `/api/v1/admin/users…`, `PUT /api/v1/admin/users/{id}/slot-quota` | identity / job | admin |
| `GET /api/v1/admin/jobs`, `DELETE /api/v1/admin/jobs/{id}` | job | admin |
| `GET /api/v1/admin/unmanaged-items`, `GET /api/v1/admin/outbox`, `POST /api/v1/admin/providers/{id}/probe-capabilities` | job (`ProbeProviderCapabilities`, C9-03) | admin |
| `GET`, `HEAD /dl/{token}/{name}` | delivery | Signed Link |
| `POST /webhooks/{providerId}/{secret}` | job `HandleProviderWebhook` | path secret |
| `GET /healthz`, `GET /readyz` | | none |

### 10.2 Job representation (sketch)

```json
{
  "id": "0192…",
  "displayName": "Sintel",
  "provider": "torbox",
  "kind": "torrent",
  "state": "ready",
  "telemetry": { "progress": 1.0, "bytesPerSecond": 0, "etaSeconds": null, "peers": null, "stalled": false },
  "sizeBytes": 129302391,
  "expiresAt": "2026-10-12T14:35:00Z",
  "shared": true,
  "slotCharged": false,
  "delivery": { "perFile": true, "bundle": { "format": "zip", "rangeSupported": false } },
  "files": [ { "id": "10", "path": "Sintel/Sintel.mp4", "sizeBytes": 129241752 } ],
  "createdAt": "2026-10-11T14:35:00Z",
  "failureReason": null
}
```

Lists carry `stale: true` when they come from stored state during a refresh back-off (C5-07). `shared` never reveals who else holds a claim. `slotCharged` tells the user whether this job currently counts toward their quota; it can turn `true` when the charge holder releases (C3-01). Jobs on an item whose submission isn't confirmed yet show `"state": "submitting"`. Two optional fields (C10-07): `ownerUsername`, shown in admin lists only, and `stale` on single-job reads. Internal identifiers (fingerprint, remote item ID, provider item ID) never appear.

### 10.3 Slug catalogue (additions and changes vs revision 1)

| Slug | Type | When |
|---|---|---|
| `capability-unavailable-for-plan` | conflict | account plan lacks the feature |
| `unsupported-hoster` | incorrect-input | URL not in the Hoster Catalogue, or hoster disabled |
| `submit-rate-exceeded` | quota-exceeded | per-user submit rate |
| `submission-pending` | (HTTP 202, not an error) | the item is still `submitting` (provider call slow or ambiguous) |
| `submission-lost` | — (history state) | ambiguous submission never appeared at the provider |
| `merged-into` | — (failure reason) | the provider deduplicated into another tracked item; claims were moved there |
| `capability-not-enabled` | unsupported | provider has the capability, debroxy v1 doesn't enable it |
| `target-not-available` | conflict | file not yet known, or per-file delivery unavailable |
| `too-many-streams` | service-unavailable | per-user or global cap |
| `refresh-rate-exceeded` | quota-exceeded (429) | per-user forced-refresh limit (C6-05) |
| `authentication-required` | authentication (401) | protected route without a session (C10-07) |
| `login-throttled` | quota-exceeded (429) | DEC-05 |
| `username-taken`, `oidc-identity-taken` | conflict | unique username / OIDC identity |
| `bootstrap-not-allowed` | conflict | `bootstrap-admin` while users exist (the `serve` bootstrap logs it and continues) |
| `invalid-username`, `invalid-password`, `invalid-role`, `invalid-status`, `invalid-oidc-identity`, `wrong-current-password`, `no-login-method` | incorrect-input | identity input rules (§4.4) |
| `user-not-found` | not-found | |
| `oidc-not-enabled` | unsupported (501) | OIDC routes while OIDC is off |
| `oidc-provider-unavailable` | provider-unavailable (502) | OIDC discovery failed |
| `provider-not-found` | not-found | unknown provider ID |
| `provider-disabled` | conflict | the account is disabled (§6.6) |
| `remote-item-unavailable` | conflict | a claim raced with the item leaving the claimable states |
| `job-id-conflict` | conflict | JobID used by another user or other content (§5.10) |
| `webhook-too-large` | incorrect-input (400) | webhook body above 64 KiB |
| `provider-state-stale` | — (internal) | a refresh could not give fresh state; shown to clients as `stale: true`, never as an error |
| `internal-error` | 500 | any error without a slug |

All revision 1 slugs remain.

---

## 11. Configuration (C10-01, C12-01…C12-04)

Configuration comes **only from environment variables**. The reference is **`.env.example`** in the repository root: every variable with its default, kept complete by the test suite. The logical schema is revision 10's (it merges revisions 1–9: `delivery.maxConcurrentStreams`; `cdnLinkCacheMax` per provider; `rateBudgets` per provider, allowed classes per kind, overrides merged over the kind defaults; `safetyRefresh{active, idle}`; `job.outbox.initialBackoff`, `job.retention.deletedItems`, `providers[].absenceGrace`, `providers[].cdnProbeHosts`; `server.shutdownTimeout` 30 s, `log.level` info, `database.readerConnections` 4), with the changes below.

- **Names.** A logical key path becomes `DEBROXY_` plus the path in upper snake case: `server.publicBaseURL` → `DEBROXY_SERVER_PUBLIC_BASE_URL`, `job.submitRate.cachedPerHour` → `DEBROXY_JOB_SUBMIT_RATE_CACHED_PER_HOUR`.
- **Providers.** `DEBROXY_PROVIDERS` lists the provider IDs in order (required, at least one). Each provider's keys are `DEBROXY_PROVIDER_<ID>_<KEY>`, where `<ID>` is the ID upper-cased with `-` as `_` (IDs are lowercase letters, digits and dashes, so names cannot collide): `DEBROXY_PROVIDER_TORBOX_KIND`, `…_USER_AGENT`, `…_EGRESS_IP_FAMILY`. A rate budget is `DEBROXY_PROVIDER_<ID>_RATE_BUDGET_<CLASS>=<requests>/<duration>`, for example `…_RATE_BUDGET_UNCACHED_CREATE=60/1h`.
- **Signing keys.** `DEBROXY_SIGNING_KEYS` lists the key IDs; the first signs, all verify (§7.1). Each key's secret is `DEBROXY_SIGNING_KEY_<ID>`.
- **Values.** Lists are comma-separated (`trustedProxies`, `followUpRefreshes`, `oidc.scopes`, `cdnProbeHosts`, `disabledCapabilities`); durations are Go duration strings (`30s`, `720h`); booleans are `true` or `false`. An unset variable keeps the default; a set but empty variable is the empty value.
- **Secrets** are given directly: `DEBROXY_PROVIDER_<ID>_API_KEY`, `DEBROXY_PROVIDER_<ID>_WEBHOOK_SECRET`, `DEBROXY_SIGNING_KEY_<ID>`, `DEBROXY_IDENTITY_OIDC_CLIENT_SECRET`, `DEBROXY_BOOTSTRAP_ADMIN_PASSWORD`. Each may instead be given as `<NAME>_FILE`, a path whose content is the value (trailing newlines removed; at most 64 KiB), for container secrets; setting both is an error. The webhook secret and each signing key are the raw value, at least 32 bytes (no base64 decoding); API keys and the OIDC client secret must be non-empty.
- **Unknown variables.** A `DEBROXY_*` variable the schema does not know, including one for a provider or key ID that is not listed, fails validation; this catches typos and old names.
- **Data directory.** `DEBROXY_DATA_DIR` is required and must be an existing directory. The database is `<dir>/debroxy.db`, its start lock `<dir>/debroxy.db.lock`; nothing else is written. Backups copy that file (ADR 0002).
- **`server.publicBaseURL`** is scheme and host only, without a path: Signed Links, the OIDC callback and the cookie `Origin` check derive from it.
- **Migrations.** `serve` applies pending migrations at startup; `migrate` applies them and exits. Both take the start lock, so `migrate` cannot run beside a running server.
- **First admin from the environment (C12-03).** If `DEBROXY_BOOTSTRAP_ADMIN_USERNAME` is set, `serve` runs `BootstrapAdmin` with that username and `DEBROXY_BOOTSTRAP_ADMIN_PASSWORD` before it listens. If users already exist, it logs that the bootstrap was skipped and continues. The password is required when the username is set. `bootstrap-admin --username <name>` with the password on standard input remains.
- **Commands.** `serve`, `migrate`, `bootstrap-admin` and `healthcheck`. `healthcheck` requests `/readyz` on the loopback address of `DEBROXY_SERVER_LISTEN` (default `:8080`) with a 3 s timeout and exits 0 only on 200; it reads no other configuration.
- **Container image (C12-04).** A static binary (pure Go, NFR-05) on the current Alpine release (pinned by exact version, with its CA bundle), run as a non-root user; `DEBROXY_DATA_DIR=/data` declared as a volume and owned by the image's user; port 8080; `HEALTHCHECK` through `debroxy healthcheck`; entrypoint `debroxy`, default command `serve`. The repository ships a `Dockerfile` and a `compose.yaml` example.
- Startup validation reports every problem at once; it adds webhook secret and signing key length, egress reachability (§9.3) and rate budgets > 0 to revision 1's rules.
---

## 12. Non-functional requirements

| ID | Requirement |
|---|---|
| NFR-01 | **No file storage.** Uploads held in memory up to the limit; bounded streaming buffers; no temp files |
| NFR-02 | **Single egress IP per provider** via pinned egress (§9.3) |
| NFR-03 | **Secrets**: API keys, CDN URLs, session tokens, signing keys and webhook secrets never appear in logs, errors, metrics, traces or responses. Access logs exclude the path of `/webhooks/*` and the token segment of `/dl/*` |
| NFR-04 | **Isolation**: other users' jobs return `job-not-found`; `shared` reveals sharing, not identities |
| NFR-05 | Single binary, pure Go, Linux amd64/arm64 and macOS |
| NFR-06 | Graceful shutdown (streams, outbox processor, DB) |
| NFR-07 | `log/slog` JSON logs; request IDs; use-case name and duration from decorators |
| NFR-08 | Runs behind a TLS reverse proxy |
| NFR-09 | **Resilience**: per-provider budgets, retries with jitter on idempotent calls, circuit breaker. Creates are never blindly retried; ambiguous failures leave the item `submitting` until follow-up refreshes resolve it (§5.5, §5.6) |
| NFR-10 | **Testability**: every driven port has a memory or fake implementation; provider adapters are tested against the recorded fixtures in `docs/discovery/` |
| NFR-11 | **Tolerant reader**: unknown provider fields are ignored; unknown states and error codes are logged once per value |

---

## 13. Decision log

| ID | Decision | Basis | ADR |
|---|---|---|---|
| DEC-01 | Cross-context calls only via consumer ports and supplier `ports/in` | DEP-4 | |
| DEC-02 | `provider` exposes operations (A-10) | ACL pattern | recommended |
| DEC-03 | Usernames immutable; jobs keep a username snapshot | simplicity | |
| DEC-04 | OIDC logs in pre-linked users only | isolation | |
| DEC-05 | Login throttling per username and per IP | pattern | |
| DEC-06 | Claims are entities inside `RemoteItem` | one consistency boundary for the ref-count | |
| DEC-07 | Fingerprint rules (§5.1), incl. the web password | content addressing | |
| DEC-08 | Slot quota counts active items on which the user holds a slot charge; checked in the charged-claim transaction by reading other aggregates (no D-11 exception) | owner decision on slot charging; SQLite single writer (ASM-S01) | recommended (storage assumption ASM-S01) |
| DEC-09 | Forbidden sync transitions are ignored and logged | tolerant reader | |
| DEC-09b | Observations stamped and ordered by debroxy's clock; older ones dropped | RD-N03 | |
| DEC-10 | Unmanaged items are listed, not adopted (except matches with `submitting` items) | isolation | |
| DEC-11 | Refresh-on-read stays on in webhook mode; webhooks only trigger coalesced refreshes | T12 refuted; webhook abuse could drain the shared budget | |
| DEC-12 | Periodic safety refresh and stall detection | reconciliation loop | |
| DEC-13 | Each context owns its tables | pattern | |
| DEC-14 | User-Agent per Provider Account, for API and CDN | user requirement | |
| DEC-15 | Real-Debrid adapter selects all files immediately | provider-neutral `job` | |
| DEC-16 | Real-Debrid web links unsupported in v1 | no persistent item; narrow hoster coverage | |
| DEC-17 | Signed Links are HMAC capability URLs with key rotation | pattern | |
| DEC-18 | Global and per-user stream caps | resource protection | |
| DEC-19 | IDs are UUIDv7 | pattern | |
| DEC-20 | Provider side effects via transactional outbox with verified deletion | pattern; deletions must be verified | |
| DEC-21 | `provider` persists only capability state; budgets and hoster catalogue live in memory | capability state must survive restarts | |
| DEC-22 | Generic auth middleware in `common/server`, wired in `cmd` | ddd-rules §3 import rule | |
| DEC-23 | Acyclic context graph; webhooks enter through `job` | `provider` must not call `job` | |
| DEC-24 | Per-user submit rate; cached submits limited separately | protection of the shared account; T13 | |
| DEC-25 | Real-Debrid `/downloads` history entries are removed by a **daily sweep** of entries older than 24 h; no per-item tracking (relies on ASM-G01) | RD-N05; owner decision | |
| DEC-26 | Egress pinned per Provider Account (IP family / local address) | dual-stack finding | |
| DEC-27 | Submissions are a `RemoteItem` state (`submitting`), completed by the create response or, after an ambiguous failure, by provider-hash matching during refresh | interrupted submits create items (TorBox §5); one aggregate per transaction (D-11) | recommended |
| DEC-28 | Charge holder pays while unreleased; once released during an active run, every remaining (and later) claimant pays | owner decision | recommended |
| DEC-31 | Uniqueness only among claimable items; no cross-aggregate revive or cancellation; creates blocked by a `deleting` holder of the same provider ID are deferred; `Revive` only for reappearing `failed`/`gone` items in sync | provider dedup can return the ID of a `failed`/`gone` item; one aggregate per transaction (D-11) | recommended |
| DEC-32 | Same-content submissions serialize on the claimable `submitting` item (unique fingerprint and provider hash) | concurrent same-content submissions must not create twice | |
| DEC-33 | Definite create failures fail the submission immediately (silently removed if only the creator claimed it); ambiguous ones get follow-up refreshes, cancelled on resolution | create failures need a defined outcome; follow-ups must stop once resolved | |
| DEC-34 | Refresh pipeline: live items → submissions by provider hash → revive → ignore deleting → unmanaged → absence on complete listings | refresh must handle interrupted submissions, reappearing and deleting items, and partial listings | |
| DEC-36 | A create deduplicated by the provider into another tracked item fails as `merged-into`; its claims move to that item one transaction per claim (eventual consistency) | D-11 | |
| DEC-37 | Outbox deletion: verify before the provider call, never hold a transaction across it; supersede when a claimable item holds the provider ID; parked deletions fail submissions deferred behind them | no transaction across a network call; deferred submissions need a limit | |
| DEC-39 | Late deletion check on completion (`deletedAt ≥ AttemptStartedAt` → create again); no re-submission fallback | a create could complete onto deleted content | recommended |
| DEC-40 | `provider` performs CDN upstream requests (`OpenDeliveryStream`); CDN URLs never leave `provider` | CDN URLs are secrets; egress stays in one context | recommended |
| DEC-41 | Refresh back-off per provider after failed or incomplete listings; stale reads flagged | failing refreshes must not keep hitting the provider and its budget | |
| DEC-42 | Storage setup: one writer connection, read-only reader pool, exclusive start lock, `busy_timeout` 5 s; created by `internal/common/sqlite` in `cmd`; context-named migrations | owner decision; all contexts share one SQLite database | |
| DEC-43 | Budget-blocked creates wait via `NotBefore` and are exempt from expiry until an attempt starts; provider 429 on a create is ambiguous | blocked creates must survive restarts; a 429 does not prove that no item was created | |
| DEC-44 | `Revive` blocked while a `deleting` item holds the provider item ID, and only from observations newer than any confirmed deletion of that ID | a deletion in progress could remove revived content; a stale listing could revive onto deleted content | |
| DEC-38 | Idempotent `SubmitJob` by `JobID` | client retries must not create duplicate jobs; A-3 | |
| DEC-35 | Stall detection only in `resolving_metadata` and `downloading` | no progress is expected in other states | |
| DEC-29 | Delivery targets `file` / `bundle`; Real-Debrid multi-file items are bundle-only | RD packaging | |
| DEC-30 | `CDNLink` is an opaque, self-redacting type | C-02 | |
| DEC-45 | Session revocation by a per-user session generation (§4.4) | revoking many sessions would change many aggregates in one transaction (D-11) | |
| DEC-46 | Create outcomes classified as not sent, ambiguous or definite beyond the budget rule (§6.6) | nothing reached the provider on dial errors or an open circuit; rejected credentials create nothing | |
| DEC-47 | Leftovers are only observations no debroxy item holds | DEC-10's meaning of Unmanaged Items | ADR 0019 |
| DEC-48 | TorBox lists are walked page by page | observed paging; a partial list would mark items gone | |
| DEC-49 | A submission stores its cached class for all its attempts | ADR 0013 rule 4 decides the class per submission, not per attempt | |
| DEC-50 | `providers[].cdnDomains` renamed `cdnProbeHosts`, without an alias | the key lists probe hosts; v1 has no installed base to keep compatible | |
| DEC-51 | Configuration only from environment variables; secrets directly or via `_FILE` | container deployment; one configuration path to maintain | |
| DEC-52 | `serve` bootstraps the first admin from the environment, skipping when users exist | a container is started before anyone can run a command in it | |
| DEC-53 | Alpine image, non-root, with the data directory as its volume | owner decision; one volume holds all state | |

---

## 14. Assumption register

Discovery verdicts per original assumption are in the discovery documents. Revision 10 adds ASM-T27 (TorBox's default page size; paging itself is observed). Assumptions **still open**:

| ID | Assumption | Status |
|---|---|---|
| ASM-T20 | TorBox lists provider-queued items in `queued/getqueued` and they move into `mylist` when started | Unverified |
| ASM-T21 | TorBox states `metaDL`, `metaDL_done`, `checkingResumeData`, `paused`, `uploading`, `compressing` exist with the mapped meaning | Unverified |
| ASM-T22 | TorBox failure surfaces as `error`/`failed*` states or the item `error` field | Unverified |
| ASM-T23 | `cooldown_until` does not block creates for this use | Unverified |
| ASM-T24 | TorBox CDN links stay valid for at least the CDN cache maximum (30 min) | Partly verified (27 min observed) |
| ASM-T25 | TorBox account slot limit enforced by queueing rather than rejection | Unverified |
| ASM-U01 | TorBox usenet endpoints mirror the torrent ones (`createusenetdownload`, `mylist`, `controlusenetdownload`, `requestdl` with `usenet_id`) | Unverified (plan); **deferred to v2** (C8-01) |
| ASM-U02 | Usenet items report a hash usable for reconciliation | Unverified; **deferred to v2** (C8-01) |
| ASM-U03 | Dedup of NZB files by SHA-256 of raw bytes is good enough (whitespace variants are separate) | Design assumption; **deferred to v2** (C8-01) |
| ASM-U04 | Usenet post-processing can move back to downloading | Unverified; **deferred to v2** (C8-01) |
| ASM-R11 | Real-Debrid allows ~250 requests per minute | Unverified |
| ASM-R15 | Real-Debrid RAR bundle links support Range | Unverified (file links do) |
| ASM-R16 | Real-Debrid failure states as mapped in §8.2 | Unverified |
| ASM-G01 | debroxy has exclusive use of each Provider Account | Operator requirement |
| ASM-T26 | TorBox `webdl/createwebdownload` accepts a form field `password` for protected hoster links | Secondary evidence (pyload, stremthru, Raycast extension, debrify send it); not tested live |
| ASM-T27 | TorBox `mylist` without `limit` returns at least as many items as the adapter's page of 1000 would; the adapter always sends `limit` and `offset`, so the default no longer matters for correctness | Unverified (paging with `limit`/`offset` observed) |
| ASM-S01 | SQLite with `BEGIN IMMEDIATE` serializes all writers, so read-then-write checks (slot quota, submit rate, look-ups before inserts) are race-free; uniqueness additionally has the partial unique indexes as a backstop | Storage assumption; breaks if the store changes |

---

## 15. Next steps

1. ADRs 0001–0019 are `Accepted`. v1 is implemented as this revision describes; an end-to-end run against both live providers passed.
2. ADRs for the decisions marked **recommended** in §13 (no decision currently requires one), plus the core decisions DEC-06, DEC-20, DEC-29, DEC-32, DEC-36, DEC-37.
3. Verify ASM-T26 (web password) with a protected link on a supported hoster, if the user can supply one.
4. Optional discovery top-ups: Real-Debrid failure states; TorBox queueing; RAR range support.
5. v2: usenet (ASM-U01…U04) with a plan that allows it.

---

## 16. Open questions

| ID | Question |
|---|---|
| OQ-01 | Should admins be able to adopt unmanaged items? |
| OQ-03 | Default Signed Link TTL (6 h assumed) |
| OQ-04 | OIDC linking by e-mail? Just-in-time provisioning later? |
| OQ-06 | Per-provider outbound proxy beyond IP pinning? |
| OQ-07 | Metrics backend and tracing |
| OQ-10 | Pause/resume/reannounce for users in v1? |
| OQ-11 | Per-user bandwidth limits later? |
| OQ-12 | Real-Debrid per-file delivery via one provider item per selected file (allowed, since RD doesn't dedup), at the cost of more items and slots? |
| OQ-13 | Should debroxy unpack nothing ever (current), or document that Real-Debrid bundles are RAR and need client-side extraction? |
| OQ-14 | Should a long stall release the slot charge so the user can submit something else? |

Resolved since revision 9: none (details settled by the implementation, see C10-*).

Resolved since revision 8: none (API shape only, see C9-*).

Resolved since revision 7: none (scope change only, see C8-01).

Resolved since revision 6: none (design fixes only, see C7-*).

Resolved since revision 5: none (design fixes only, see C6-*).

Resolved since revision 4: none (design fixes and owner decisions, see C5-*).

Resolved since revision 3: none (design fixes only, see C4-*).

Resolved since revision 2: slot charge after the creator releases (DEC-28).

Resolved since revision 1: OQ-02 (webhook authenticity: secret path only, refresh-hint semantics), OQ-05 (`user_ip` omitted), OQ-09 (history read model).
