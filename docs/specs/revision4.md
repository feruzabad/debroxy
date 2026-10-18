# debroxy — Specification, Revision 4

| | |
|---|---|
| Revision | 4 |
| Status | Draft. Based on live provider discovery (2026-10-11) and design iterations since revision 1 |
| Date | 2026-10-11 |
| Supersedes | [`revision3.md`](revision3.md) |
| Inputs | [`revision3.md`](revision3.md), [`../discovery/torbox.md`](../discovery/torbox.md), [`../discovery/realdebrid.md`](../discovery/realdebrid.md) |
| Architecture rules | [`docs/ddd-rules.md`](../ddd-rules.md) (binding) |

## 0. About this document

### 0.1 Revision workflow

1. Each revision is a new file `docs/specs/revisionN.md`. Past revisions are never edited.
2. Every revision starts with a **changelog** against the previous one and keeps an **assumption register** (§14).
3. ADRs are written next, from this revision's decision log (§13). Decisions marked **ADR required** bend a binding MUST rule and must not be implemented before their ADR is accepted. **ADR recommended** marks important decisions and storage assumptions that bend no rule.

### 0.2 Conventions

- **MUST / SHOULD / MAY** as in RFC 2119.
- `ASM-*` = an assumption that is not verified. `DEC-*` = a design decision (§13). `TB-N*` / `RD-N*` = new discovery findings.
- **Evidence** links point to discovery documents. Provider facts without an `ASM` tag are **observed**.

### 0.3 Changelog (revision 3 → 4)

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

Kept: webhook secret in the URL path (TorBox offers nothing else); the verified-deletion residual race.

### 0.4 Changelog (revision 2 → 3, for reference)

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

Kept unchanged: the verified-deletion residual race (covered by adapter contract tests).

### 0.5 Changelog (revision 1 → 2, for reference)

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
| Job kinds | Torrents (magnet, `.torrent`), web downloads (provider-listed hosters), usenet (`.nzb`; NZB URL once ASM-U02 is verified) **where the account's plan allows it** |
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
              └─────▲──────┘
                    │ UserStatusChecker
┌──────────┐ JobTargets          ┌────────────┐
│ delivery │────────┼──────────►│    job     │◄── webhook HTTP adapter (job/adapters/in/webhook)
└────┬─────┘        │           └─────┬──────┘
     │ CDNLinkResolver              │ ProviderGateway (incl. TranslateWebhook)
     ▼                              ▼
┌─────────────────────────────────────────────┐
│                  provider                   │──► TorBox / Real-Debrid
└─────────────────────────────────────────────┘
```

| Consumer → Supplier | Consumer port (`ports/out`) | Supplier `ports/in` | Relationship |
|---|---|---|---|
| job → provider | `ProviderGateway` | provider operations | Customer/Supplier; provider is the ACL |
| delivery → job | `JobTargets` | job query `DeliveryTarget` | Customer/Supplier |
| delivery → provider | `CDNLinkResolver` | provider operation `ResolveCDNLink` | Customer/Supplier |
| delivery → identity | `UserStatusChecker` | identity query `UserStatus` | Customer/Supplier |
| all HTTP adapters → identity | `common/server.Authenticator` (interface) | identity query `AuthenticatedActor` | Conformist; wired in `cmd` (DEC-22) |

- **DEC-01** Cross-context calls follow ddd-rules DEP-4.
- **DEC-02** `provider` exposes operations, not CQRS (ddd-rules A-10).
- **DEC-23** The dependency graph between contexts is **acyclic**: `delivery → job → provider`, `delivery → provider`, `* → identity`. Webhook requests enter through `job` (command `HandleProviderWebhook`), which asks `provider` to translate them.
- **DEC-22** `common/server` defines `Authenticator interface { Authenticate(ctx, credential) (auth.Principal, error) }` and a generic middleware. `cmd` wires it with an adapter around `identity`'s `AuthenticatedActor` query. Each context's HTTP adapter converts `auth.Principal` to its own `ports/in.Actor`.

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
| `BootstrapAdmin` | command | CLI | Only while no user exists |
| `CreateUser`, `ChangeUserRole`, `DisableUser`, `EnableUser`, `ResetUserPassword`, `LinkOIDCIdentity`, `UnlinkOIDCIdentity` | command | admin | Disabling or resetting revokes sessions. A disabled user's jobs stay; Signed Links stop working |
| `ChangeOwnPassword` | command | user | Needs the current password; revokes other sessions |
| `LogInWithPassword` | command | anonymous | Token generated by the adapter (ddd-rules A-3); uniform `invalid-credentials` |
| `LogInWithOIDC` | command | anonymous | Authorization code + PKCE in the adapter; only pre-linked users (DEC-04) |
| `LogOut`, `TouchSession` | command | user / system | |
| `AuthenticatedActor`, `UserStatus`, `Users`, `UserDetails` | query | system / admin | |

### 4.3 Transport details

Bearer token (`Authorization: Bearer`) or cookie `debroxy_session` (HttpOnly, Secure, SameSite=Strict). Cookie-authenticated state changes need a matching `Origin`. Login throttling per username and per IP (DEC-05).

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
| source | `Source` | kept while `submitting` (needed for a deferred or repeated create); cleared afterwards |
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
| submission | `Submission{StartedAt, Attempts, FollowUpsDue []time, DeferredBehind RemoteItemID?}`? | present only while `submitting` |
| claims | `[]Job` | unreleased jobs only |
| chargeHolder | `JobID?` | the job that started the current Active Run |
| chargeHolderReleased | bool | the charge holder was released during the current Active Run → every claim is charged (C3-01) |

Derived: `IsChargedTo(userID)` = item active **and** (owner of `chargeHolder` = user, **or** `chargeHolderReleased` and the user holds a claim).

**Entity `Job`** (inside `RemoteItem`): `id` (UUIDv7, chosen by the caller), `ownerID`, `ownerUsername` (snapshot), `displayName`, `claimedAt`, `origin` (`created` | `joined`).
Released jobs move to the `job_history` read model, visible to their owner and admins.

**Value object `Fingerprint`** (DEC-07)

| Source | Fingerprint |
|---|---|
| Magnet | `btih:<40 hex lowercase>` (base32 converted); `btmh:<hex>` only if no btih |
| `.torrent` | SHA-1 of the bencoded `info` dictionary → `btih:`; v2-only → `btmh:` |
| `.nzb` | `nzb:<sha256 of raw bytes>` (ASM-U03) |
| NZB URL | `nzburl:<normalized URL>`; **NZB-URL submission disabled until ASM-U02 is verified** |
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
| `FailSubmission(reason)` | `deleting` (no provider item) | `deleted` |
| `DeferSubmission(blocker)` | `submitting` | `submitting` (records `DeferredBehind`) |
| `Revive(observation)` | `failed`, `gone` (provider item reappears active or ready, and no claimable item holds that provider ID) | the observation's mapped state |
| `MarkGone` | any state with a provider item ID except `deleting`/`deleted` | `gone` |
| `RequestDeletion` (last claim released) | `submitting` | `deleting` (no provider item yet) |
| `RequestDeletion` | any other active state, `ready`, `failed` with provider item | `deleting` |
| `RequestDeletion` | `gone`; `failed` without provider item | `deleted` (no provider call) |
| `ConfirmDeleted` / `SupersedeDeletion` | `deleting` | `deleted` |

Rules:
- Forbidden sync transitions are not applied; the anomaly is logged (DEC-09).
- **Uniqueness among claimable items** (partial unique indexes): (`provider_id`, `fingerprint`), (`provider_id`, `provider_hash`), and (`provider_id`, `provider_item_id`) where the provider item ID is set. A `failed`/`gone`/`deleting` item does not block a new item for the same content or the same provider ID. No cross-aggregate revive or cancellation exists.
- `processing → downloading` exists for usenet repair (ASM-U04).

### 5.3 Domain behaviour of `RemoteItem`

| Method | Rules |
|---|---|
| `StartSubmission(id, provider, source, fingerprint, providerHash, firstJob, now)` | New item in `submitting`; `firstJob.origin = created`; it becomes `chargeHolder` |
| `Claim(job, now)` | Only in claimable states (`ErrRemoteItemUnavailable` otherwise). One unreleased job per owner (`JobAlreadyExistsError{ExistingJobID}`). Same `JobID` already present → `ErrDuplicateJobID` (the use case turns it into an idempotent answer). `origin = joined`. The new claim is charged iff the item is active and `chargeHolderReleased`; the quota check for that case happens in the use case |
| `CompleteSubmission(providerItemID, observation, now)` | §5.2. Clears `submission` and `source`; applies the observation as `Observe` would. If the resulting state is not active, clears the charge fields. Records `SubmissionCompleted` and `LifecycleAdvanced` |
| `FailSubmission(reason, now)` | §5.2. Records `SubmissionFailed{reason, claimants}` |
| `DeferSubmission(blocker, now)` | The provider returned a provider item ID that a `deleting` item still holds (§5.5). Records `SubmissionDeferred` |
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
| `ExpireSubmissions` | command | system (every minute) | `submitting` older than the submission timeout → `FailSubmission(submission-lost)` |
| `ResumeDeferredSubmissions` | command | system (every minute) | `submitting` items whose blocker reached `deleted` → `ExecuteSubmission` again |
| `DetectStalls` | command | system (periodic) | §5.3 |
| `ExecuteRemoteItemDeletion` | command | outbox processor | §5.7 |
| `SetSlotQuota` | command | admin | |
| `JobsOfUser`, `JobDetails`, `JobHistory` | query | user | |
| `AllJobs`, `UnmanagedItems`, `ParkedOutboxMessages` | query | admin | |
| `CachedAvailability` | query | user | needs `CachedCheck` |
| `DeliveryTarget` | query | delivery | §5.9 |
| `QuotaUsage` | query | user | `slotsUsed` counts active items where `IsChargedTo(user)`, `submitting` included |

### 5.5 Submission flow

Command: `SubmitJob{Actor, JobID, ProviderID, Source, DisplayName?}`

1. **Idempotency.** If `JobID` already exists (as a claim, or in history), return its current representation: 201/202 for the same owner and the same fingerprint; `conflict` otherwise. No other step runs.
2. **Validate.** Provider exists and is enabled. The kind capability exists (`capability-not-supported`), is enabled in v1 (`capability-not-enabled`), and isn't `unavailable` for the account (`capability-unavailable-for-plan`). Web URLs must match the Hoster Catalogue (`unsupported-hoster`). Compute the Fingerprint and provider hash.
3. **Join or start: one transaction.**
   - A claimable item exists for (provider, fingerprint), **including `submitting` items**:
     - item active and `chargeHolderReleased` → check the slot quota (`slot-quota-exceeded`);
     - otherwise joining costs nothing (no quota or rate check; stated explicitly);
     - `Claim`. If the item is `submitting`, respond **202 `submission-pending`**, otherwise 201. **Done; no provider call.**
   - None exists → check the user's **submit rate** (DEC-24) and **slot quota** (the item will be active and charged to them), then `StartSubmission`. This is the only quota check on the create path. Respond 202 if the provider call below doesn't finish within the request deadline (default 10 s), else 201 with the completed item.
4. **Provider call** (`ExecuteSubmission`, outside any transaction, ddd-rules A-9): `ProviderGateway.CreateRemoteItem(source)`.
5. **Outcome: one transaction on the same item.**
   - **Success, provider item ID P:**
     - another **claimable** item already holds P (the provider deduplicated into an item debroxy already tracks; possible because content can enter under a different fingerprint, e.g. a magnet vs. a hash variant) → `FailSubmission(merged-into{otherItem})` for this item; the use case then claims the other item for each of this item's claims, **one transaction per claim** (each changes only the other item), and releases them here. Rare; documented as eventual consistency (DEC-36);
     - a **`deleting`** item still holds P (deletion not yet executed) → `DeferSubmission(blocker)`; `ResumeDeferredSubmissions` repeats the create after the blocker is `deleted`;
     - otherwise → `CompleteSubmission(P, observation)`. Any `failed`/`gone` item with P stays as it is (its claimants keep their history; they can resubmit and will join this item).
   - **Definite failure** (classified provider error) → if the item has only the creator's claim: the item is removed (bookkeeping `deleted`; no job is shown) and the error goes to the submitter synchronously. Otherwise `FailSubmission(provider-rejected{detail})`; joined users see the failed job.
   - **Ambiguous failure** → the item stays `submitting`, follow-up refreshes are scheduled (§5.6), and the submitter gets 202.
6. A unique-index violation in step 3 or 5 is never surfaced; the transaction is retried from its look-up.

Errors: `invalid-source`, `upload-too-large`, `capability-not-supported`, `capability-not-enabled`, `capability-unavailable-for-plan`, `unsupported-hoster`, `submit-rate-exceeded`, `slot-quota-exceeded`, `job-already-exists`, `provider-unavailable`, `provider-rejected`.

**Slot-quota consistency.** The count reads other `RemoteItem` aggregates inside the same `BEGIN IMMEDIATE` transaction and changes only one. Relies on ASM-S01 (SQLite single writer).

### 5.6 Sync

**Observation** = `{ProviderItemID, Kind, ProviderHash, State, Name, SizeBytes, Files?, Bundle?, PerFileDelivery, ExpiresAt?, Telemetry, FailureReason?, ObservedAt}`; `ObservedAt` is debroxy's clock at request start.

**`RefreshProviderState{ProviderID, Cause}`**, `Cause` ∈ `read` (MaxAge 10 s), `forced` (0; per-user rate limited), `webhook` (coalesced to one per webhook interval, default 30 s), `safety`, `follow-up` (skipped when an authoritative sync younger than the follow-up's due time already ran). Single-flight per provider; all causes share the rate budgets.

**Refresh pipeline (single source of truth)**:
1. `ProviderGateway.ListRemoteItems` → observations, with `Complete = true` only if every page and kind was listed without error.
2. **Known live items:** observation's provider ID held by a claimable item → `Observe`.
3. **Interrupted submissions**: remaining observations whose `providerHash` matches a `submitting` item of that provider → the same outcome logic as §5.5 step 5 (`CompleteSubmission` / `DeferSubmission`). Uniqueness of `(provider, provider_hash)` among claimable items makes the pairing 1:1. Remaining follow-ups of that item are cancelled.
   The same applies to `deleting` items without a provider ID (released while submitting): they record the provider ID and write `DeleteRemoteItem`.
4. **Reappearing items:** remaining observations whose provider ID belongs to a `failed`/`gone` item and whose state is active or ready → `Revive` (only if no claimable item holds that provider ID; step 2 would have taken it).
5. **Observations for `deleting` items** (deletion not yet propagated) → ignored.
6. **Leftovers** → Unmanaged Items (DEC-10).
7. **Absence** (only if `Complete`): claimable items with a provider ID missing from the observations → `MarkGone`. Grace: items created, completed or revived within the absence grace (default 60 s, configurable per provider).

**Triggers**
- **Refresh-on-read** before `JobsOfUser`, `JobDetails`, `AllJobs`; it stays on in webhook mode.
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
3. **Transaction 2:** re-check step 1's condition. If a claimable item took over the provider ID meanwhile, the deletion also removed its content: that item gets `MarkGone` through the next refresh, and its users can resubmit. This residual race is documented and logged; the window is one provider call. Otherwise `ConfirmDeleted`.

Retries with exponential back-off (1 s … 1 h), parked after 20 attempts. Real-Debrid download-history cleanup per DEC-25. The former `DeleteProviderItem` message is no longer needed: no provider item exists without a `RemoteItem` (C4-01).

### 5.8 Persistence (SQLite, sketch)

| Table | Key columns |
|---|---|
| `remote_items` | `id`, `provider_id`, `provider_item_id` (nullable), `provider_hash`, `kind`, `fingerprint`, `source_blob` (nullable; only while submitting), `state`, `failure_reason`, `name`, `size_bytes`, `bundle_format`, `bundle_range_supported`, `per_file_delivery`, `expires_at`, `telemetry_json`, `last_progress_at`, `last_observation_at`, `submission_json`, `charge_holder_job_id`, `charge_holder_released`, `created_at`. Partial unique indexes over claimable states: (`provider_id`, `fingerprint`), (`provider_id`, `provider_hash`), (`provider_id`, `provider_item_id`) where not null |
| `remote_files` | `remote_item_id`, `file_id`, `path`, `size_bytes` |
| `jobs` | unreleased claims |
| `job_history` | released jobs; retention default 180 days |
| `slot_quotas`, `submit_rate_windows` | |
| `provider_sync_state` | `provider_id`, `last_authoritative_sync_at` |
| `unmanaged_items` | pruned when unseen for 7 days |
| `outbox_messages` | processed rows pruned after 30 days |

**Retention of items:** `deleted` items and their files are pruned after 30 days; `failed`/`gone` items without claims are moved to `deleting`/`deleted` by `RequestDeletion` when their last claim is released, so they don't accumulate. Each context owns its tables (DEC-13).

### 5.9 Delivery targets

| Target | Available when | Source at provider |
|---|---|---|
| `file:<FileID>` | item `ready`, file reported, `perFileDelivery = true` | TorBox `requestdl` with `file_id`; Real-Debrid unrestrict of the single link (single selected file only) |
| `bundle` | item `ready`, `bundle` present | TorBox `requestdl … zip_link=true` (**Range not supported**); Real-Debrid unrestrict of the single link when more than one file is selected (RAR, range assumed supported, ASM-R15) |

File identity is the provider file ID, never the position in the `.torrent`.

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
| `ListRemoteItems` | provider ID | Observations (all usable kinds), `Authoritative` |
| `DescribeRemoteItem` | provider ID, kind, provider item ID | Observation or not-found |
| `DeleteRemoteItem` | provider ID, kind, provider item ID | deleted / not-found (verified, §5.7) |
| `ResolveCDNLink` | provider ID, kind, provider item ID, target | opaque `CDNLink{secret URL, RangeSupported, ExpiresHint?}` (C-02) |
| `CheckCached` | provider ID, kind, provider hashes | cached + file list where offered |
| `TranslateWebhook` | provider ID, path secret, headers, body | `WebhookSignal{RefreshRequested}` or `rejected` |
| `ProbeCapabilities` | provider ID | refreshed capability state (admin) |

Provider-specific steps stay inside adapters: Real-Debrid file selection right after creation (select all, DEC-15); Real-Debrid hoster-link → unrestrict; TorBox per-kind endpoints and ID parameter names.

### 6.3 Capabilities

| Kind Capability | TorBox | Real-Debrid | Evidence |
|---|---|---|---|
| `SubmitMagnet` | ✓ | ✓ | observed |
| `SubmitTorrentFile` | ✓ | ✓ | observed |
| `SubmitNZBFile` | plan-dependent | ✗ | TorBox 500 `PLAN_RESTRICTED_FEATURE` on plan 1 |
| `SubmitNZBURL` | plan-dependent; **not enabled in v1** until ASM-U02 is verified | ✗ | ASM-U02 |
| `SubmitWebURL` | ✓ hoster catalogue only | provider ✓ (`/unrestrict`, 281 domains), **not enabled in debroxy v1** (DEC-16) | `UNSUPPORTED_SITE` for direct links; RD-N06 |
| `PerFileDelivery` | ✓ | single-file items only | RAR packaging |
| `BundleDelivery` | ✓ zip (no Range) | ✓ rar (multi-file items) | observed |
| `CachedCheck` | ✓ | ✗ (`disabled_endpoint`) | observed |
| `Webhooks` | ✓ refresh hint only | ✗ | observed |
| `ProviderDedup` | ✓ (same ID for known content) | ✗ (new item per add) | observed; informational |

The API advertises kind capabilities, whether debroxy enables them, and the account state. Errors:
- the provider lacks the capability → `capability-not-supported` (501);
- the provider has it but debroxy v1 doesn't enable it → `capability-not-enabled` (501);
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
| 429, or rate budget exhausted | wait/retry inside the call's deadline, then `provider-unavailable` with `retryAfter` |
| Network errors, 5xx without a known code, open circuit breaker | `provider-unavailable` |
| Any other envelope error code | `provider-rejected` with sanitized detail; unknown codes logged (RISK-06) |

### 6.5 Adapter rules

- Provider JSON types are unexported; native→neutral state mapping is one table per provider (§8).
- **CDN URLs and API keys never appear in logs, errors, metrics, traces or API responses.** URLs are logged as scheme + host only. TorBox `requestdl` needs the key as a query parameter, so request URLs are redacted too.
- Real-Debrid timestamps are ignored for logic (RD-N03); TorBox `eta = 8640000` means unknown.

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
5. `CDNLinkResolver.ResolveCDNLink` (in-memory cache per target, max 30 min; the cache holds secrets and is never dumped or logged).
6. Upstream request with the provider's User-Agent and egress. Forward `Range` and `If-Range` **only if `RangeSupported`**. For targets without range support, a client `Range` is ignored: respond 200 with the full body and `Accept-Ranges: none` (C-16).
7. Copy status (200/206/416) and only `Content-Length`, `Content-Range`, `Content-Type`, `Last-Modified`, `ETag`; set `Accept-Ranges` from `RangeSupported`, not from upstream. Always set `Content-Disposition: attachment; filename*=UTF-8''<name>` (Real-Debrid sends no filename).
8. Stream with a bounded buffer; client disconnect cancels upstream.
9. Upstream 401/403/404/410 before the first byte → drop the cache entry, resolve once more, retry; otherwise 502.

`user_ip` is **not** sent to TorBox: links aren't IP-bound, and omitting it lets TorBox choose the CDN region for the proxy's own IP (resolves OQ-05).

### 7.4 CDN URL secrecy (C-02)

- `CDNLink` is an opaque type whose `String()` and `MarshalJSON` return a redacted form.
- CDN URLs exist only in memory, in the resolver cache and during an upstream request.
- If a CDN URL is suspected leaked, the operator rotates the provider API key. Deleting the item does not revoke the link.

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
- Endpoints: `torrents/createtorrent` (multipart `magnet` | `file`, `allow_zip`), `torrents/mylist`, `torrents/controltorrent` (JSON `torrent_id`, `operation`), `torrents/requestdl` (`torrent_id`, `file_id` | `zip_link`), `torrents/checkcached`; `webdl/createwebdownload` (`link`), `webdl/mylist`, `webdl/controlwebdownload` (`webdl_id`), `webdl/requestdl` (`web_id`), `webdl/checkcached`, `webdl/hosters`; `queued/getqueued`; `user/me?settings=true`. Usenet endpoints ASM-U01.
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

- Each Provider Account MUST pin its egress: `ipFamily` (`ipv4` default) and optionally `localAddress`. API calls and CDN downloads use the same dialer.
- Startup check: resolve the API host **and the provider's known CDN domains** (TorBox `*.tb-cdn.st`, `*.tb-cdn.io`; Real-Debrid `*.download.real-debrid.com`; list kept in the adapter, overridable in config) in the pinned family and open a TCP connection to one representative of each; otherwise refuse to start.
- At runtime, a CDN host that has no address in the pinned family fails the stream with 502 `provider-unavailable`, logs an operator alert, and is never retried over the other family.

### 9.4 Rate budgets

| Provider | Budget | Value | Source |
|---|---|---|---|
| TorBox | general API, per endpoint | 300 / 60 s | observed headers |
| TorBox | uncached create (torrent, web) | **60 / hour** | observed headers |
| TorBox | CDN zip | 60 / 60 s | observed headers |
| Real-Debrid | whole API | 250 / 60 s | ASM-R11 (no headers seen) |

- Budgets are tracked per Provider Account and updated from `x-ratelimit-*` headers when present.
- **Startup:** each header-reporting bucket is **single-flight until its first headers arrive** after a restart; from then on the provider's own counters drive it. Header-less buckets (Real-Debrid) get 50 % of their static budget in the first window after a restart, since the provider may still count calls from before it.
- **DEC-24** Per-user submit rate (create path only; joins are free): default 10 per hour. With `CachedCheck`, a submit whose hash is cached counts as **cheap** and is limited separately (default 60 per hour).

---

## 10. External API (OpenAPI-first)

### 10.1 Endpoints

| Method & path | Context / use case | Auth |
|---|---|---|
| `POST /api/v1/auth/login`, `POST /api/v1/auth/logout` | identity | anonymous / user |
| `GET /api/v1/auth/oidc/login`, `GET /api/v1/auth/oidc/callback` | identity | anonymous |
| `GET /api/v1/me` | identity `UserDetails` + job `QuotaUsage` | user |
| `PUT /api/v1/me/password` | identity | user |
| `GET /api/v1/providers` | provider `ListProviders` | user |
| `GET /api/v1/providers/{id}/hosters` | provider `HosterCatalogue` | user |
| `POST /api/v1/providers/{id}/cached-availability` | job `CachedAvailability` | user |
| `POST /api/v1/providers/{id}:refresh` | job `RefreshProviderState{Forced}` (rate limited) | user |
| `POST /api/v1/jobs` | job `SubmitJob` → 201, or **202 `submission-pending`** | user |
| `GET /api/v1/jobs`, `GET /api/v1/jobs/{id}` | job queries (after refresh-on-read) | user / owner |
| `GET /api/v1/jobs/history` | job `JobHistory` | user |
| `PATCH /api/v1/jobs/{id}`, `DELETE /api/v1/jobs/{id}` | rename / release | owner (admin may release) |
| `POST /api/v1/jobs/{id}/links` | `{target: {fileId} \| {bundle: true}}` → `{url, expiresAt, rangeSupported}` | owner |
| `POST /api/v1/jobs/{id}/links:all` | links for all files, or the bundle | owner |
| `/api/v1/admin/users…`, `PUT /api/v1/admin/users/{id}/slot-quota` | identity / job | admin |
| `GET /api/v1/admin/jobs`, `DELETE /api/v1/admin/jobs/{id}` | job | admin |
| `GET /api/v1/admin/unmanaged-items`, `GET /api/v1/admin/outbox`, `POST /api/v1/admin/providers/{id}:probe-capabilities` | job / provider | admin |
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

`shared` never reveals who else holds a claim. `slotCharged` tells the user whether this job currently counts toward their quota; it can turn `true` when the charge holder releases (C3-01). Jobs on an item whose submission isn't confirmed yet show `"state": "submitting"`.

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

All revision 1 slugs remain.

---

## 11. Configuration (changes vs revision 1)

```yaml
job:
  defaultSlotQuota: 3
  submitRate: { perHour: 10, cachedPerHour: 60 }
  forcedRefresh: { perUserPerMinute: 2 }
  refreshMaxAge: "10s"
  safetyRefresh: { active: "5m", idle: "30m" }
  stallThreshold: "30m"
  submissionTimeout: "20m"
  submitRequestDeadline: "10s"
  absenceGrace: "60s"            # overridable per provider
  followUpRefreshes: ["30s", "2m", "5m", "10m", "15m"]
  webhookRefreshInterval: "30s"
  retention: { jobHistory: "4320h", unmanagedItemsUnseen: "168h", processedOutbox: "720h" }
delivery:
  signedLinkTTL: "6h"
  cdnLinkCacheMax: "30m"
  maxStreamsPerUser: 4
  maxConcurrentStreams: 64
providers:
  - id: "torbox"
    kind: "torbox"
    apiKeyEnv: "DEBROXY_TORBOX_API_KEY"
    userAgent: "debroxy/0.1"
    egress: { ipFamily: "ipv4", localAddress: "" }
    rateBudgets: { general: { requests: 300, per: "60s" }, uncachedCreate: { requests: 60, per: "1h" } }
    webhook: { enabled: true, secretEnv: "DEBROXY_TORBOX_WEBHOOK_SECRET" }   # >= 32 random bytes
  - id: "realdebrid"
    kind: "realdebrid"
    apiKeyEnv: "DEBROXY_REALDEBRID_API_KEY"
    userAgent: "debroxy/0.1"
    egress: { ipFamily: "ipv4" }
    rateBudgets: { general: { requests: 250, per: "60s" } }
```

Startup validation adds: webhook secret length, egress reachability (§9.3), rate budgets > 0. All other settings are as in revision 1 §11.

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
| DEC-08 | Slot quota counts active items on which the user holds a slot charge; checked in the charged-claim transaction by reading other aggregates (no D-11 exception) | owner decisions on the slot charge; one aggregate changed per transaction (D-11) | recommended (storage assumption ASM-S01) |
| DEC-09 | Forbidden sync transitions are ignored and logged | tolerant reader | |
| DEC-09b | Observations stamped and ordered by debroxy's clock; older ones dropped | out-of-order observations; RD-N03 | |
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
| DEC-20 | Provider side effects via transactional outbox with verified deletion | pattern; TorBox delete errors need verification | |
| DEC-21 | `provider` persists only capability state; budgets and hoster catalogue live in memory | capability state must survive restarts | |
| DEC-22 | Generic auth middleware in `common/server`, wired in `cmd` | shared authentication without coupling contexts to `identity` | |
| DEC-23 | Acyclic context graph; webhooks enter through `job` | no dependency cycle between `provider` and `job` | |
| DEC-24 | Per-user submit rate; cached submits limited separately | protect the shared create budget; T13 | |
| DEC-25 | Real-Debrid `/downloads` history entries are deleted by the adapter when their Remote Item is deleted and by a daily sweep of entries older than 24 h | RD-N05 | |
| DEC-26 | Egress pinned per Provider Account (IP family / local address) | dual-stack finding | |
| DEC-27 | Submissions are a `RemoteItem` state (`submitting`), completed by the create response or, after an ambiguous failure, by provider-hash matching during refresh | interrupted creates must not leave untracked provider items; one aggregate per transaction (D-11) | recommended |
| DEC-28 | Charge holder pays while unreleased; once released during an active run, every remaining (and later) claimant pays | owner decision | recommended |
| DEC-31 | Uniqueness only among claimable items; no cross-aggregate revive or cancellation; creates blocked by a `deleting` holder of the same provider ID are deferred; `Revive` only for reappearing `failed`/`gone` items in sync | cross-aggregate revive and cancellation changed several aggregates at once | recommended |
| DEC-32 | Same-content submissions serialize on the claimable `submitting` item (unique fingerprint and provider hash) | concurrent same-content submissions must not create duplicates | |
| DEC-33 | Definite create failures fail the submission immediately (silently removed if only the creator claimed it); ambiguous ones get follow-up refreshes, cancelled on resolution | definite and ambiguous create failures need different handling | |
| DEC-34 | Refresh pipeline: live items → submissions by provider hash → revive → ignore deleting → unmanaged → absence on complete listings | one ordered pipeline as single source of truth | |
| DEC-36 | A create deduplicated by the provider into another tracked item fails as `merged-into`; its claims move to that item one transaction per claim (eventual consistency) | D-11 | |
| DEC-37 | Outbox deletion: verify before and after the provider call, never hold a transaction across it; supersede when a claimable item holds the provider ID | no transaction held across a provider call | |
| DEC-38 | Idempotent `SubmitJob` by `JobID` | client retries; A-3 | |
| DEC-35 | Stall detection only in `resolving_metadata` and `downloading` | false stalls in states without progress | |
| DEC-29 | Delivery targets `file` / `bundle`; Real-Debrid multi-file items are bundle-only | RD packaging | |
| DEC-30 | `CDNLink` is an opaque, self-redacting type | C-02 | |

---

## 14. Assumption register

Discovery verdicts per original assumption are in the discovery documents. Assumptions **still open**:

| ID | Assumption | Status |
|---|---|---|
| ASM-T20 | TorBox lists provider-queued items in `queued/getqueued` and they move into `mylist` when started | Unverified |
| ASM-T21 | TorBox states `metaDL`, `metaDL_done`, `checkingResumeData`, `paused`, `uploading`, `compressing` exist with the mapped meaning | Unverified |
| ASM-T22 | TorBox failure surfaces as `error`/`failed*` states or the item `error` field | Unverified |
| ASM-T23 | `cooldown_until` does not block creates for this use | Unverified |
| ASM-T24 | TorBox CDN links stay valid for at least the CDN cache maximum (30 min) | Partly verified (27 min observed) |
| ASM-T25 | TorBox account slot limit enforced by queueing rather than rejection | Unverified |
| ASM-U01 | TorBox usenet endpoints mirror the torrent ones (`createusenetdownload`, `mylist`, `controlusenetdownload`, `requestdl` with `usenet_id`) | Unverified (plan) |
| ASM-U02 | Usenet items report a hash usable for reconciliation | Unverified |
| ASM-U03 | Dedup of NZB files by SHA-256 of raw bytes is good enough (whitespace variants are separate) | Design assumption |
| ASM-U04 | Usenet post-processing can move back to downloading | Unverified |
| ASM-R11 | Real-Debrid allows ~250 requests per minute | Unverified |
| ASM-R15 | Real-Debrid RAR bundle links support Range | Unverified (file links do) |
| ASM-R16 | Real-Debrid failure states as mapped in §8.2 | Unverified |
| ASM-G01 | debroxy has exclusive use of each Provider Account | Operator requirement |
| ASM-T26 | TorBox `webdl/createwebdownload` accepts a form field `password` for protected hoster links | Secondary evidence (pyload, stremthru, Raycast extension, debrify send it); not tested live |
| ASM-S01 | SQLite with `BEGIN IMMEDIATE` serializes all writers, so read-then-write quota checks are race-free | Storage assumption; breaks if the store changes |

---

## 15. Next steps

1. ADRs for the decisions marked **recommended** in §13 (no decision currently requires one), plus the core decisions DEC-06, DEC-20, DEC-29, DEC-32, DEC-36, DEC-37.
2. Verify ASM-T26 (web password) with a protected link on a supported hoster, if the user can supply one.
3. Optional discovery top-ups: usenet with a plan that allows it; Real-Debrid failure states; TorBox queueing; RAR range support.

---

## 16. Open questions

| ID | Question |
|---|---|
| OQ-01 | Should admins be able to adopt unmanaged items? |
| OQ-03 | Default Signed Link TTL (6 h assumed) |
| OQ-04 | OIDC linking by e-mail? Just-in-time provisioning later? |
| OQ-06 | Per-provider outbound proxy beyond IP pinning? |
| OQ-07 | Metrics backend and tracing |
| OQ-08 | Container image and deployment shape |
| OQ-10 | Pause/resume/reannounce for users in v1? |
| OQ-11 | Per-user bandwidth limits later? |
| OQ-12 | Real-Debrid per-file delivery via one provider item per selected file (allowed, since RD doesn't dedup), at the cost of more items and slots? |
| OQ-13 | Should debroxy unpack nothing ever (current), or document that Real-Debrid bundles are RAR and need client-side extraction? |
| OQ-14 | Should a long stall release the slot charge so the user can submit something else? |

Resolved since revision 3: none (revision 4 made design fixes only, see C4-*).

Resolved since revision 2: slot charge after the creator releases (DEC-28).

Resolved since revision 1: OQ-02 (webhook authenticity: secret path only, refresh-hint semantics), OQ-05 (`user_ip` omitted), OQ-09 (history read model).
