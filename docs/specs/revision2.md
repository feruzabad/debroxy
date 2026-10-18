# debroxy — Specification, Revision 2

| | |
|---|---|
| Revision | 2 |
| Status | Draft. Based on live provider discovery (2026-10-11) and design iterations since revision 1 |
| Date | 2026-10-11 |
| Supersedes | [`revision1.md`](revision1.md) |
| Inputs | [`revision1.md`](revision1.md), [`../discovery/torbox.md`](../discovery/torbox.md), [`../discovery/realdebrid.md`](../discovery/realdebrid.md) |
| Architecture rules | [`docs/ddd-rules.md`](../ddd-rules.md) (binding) |

## 0. About this document

### 0.1 Revision workflow

1. Each revision is a new file `docs/specs/revisionN.md`. Past revisions are never edited.
2. Every revision starts with a **changelog** against the previous one and keeps an **assumption register** (§14).
3. ADRs are written next, from this revision's decision log (§13). Decisions marked **ADR required** bend a binding rule and must not be implemented before their ADR is accepted.

### 0.2 Conventions

- **MUST / SHOULD / MAY** as in RFC 2119.
- `ASM-*` = an assumption that is not verified. `DEC-*` = a design decision (§13). `TB-N*` / `RD-N*` = new discovery findings.
- **Evidence** links point to discovery documents. Provider facts without an `ASM` tag are **observed**.

### 0.3 Changelog (revision 1 → 2)

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
| Job kinds | Torrents (magnet, `.torrent`), web downloads (provider-listed hosters), usenet (`.nzb`, NZB URL) **where the account's plan allows it** |
| Providers | TorBox, Real-Debrid. Several providers per deployment, one account each |
| Provider selection | Explicit per job |
| Users | Semi-trusted, strictly isolated; roles Admin and User |
| Authentication | Username + password; external OIDC |
| Sharing | Same content held once at the provider, shared through **Claims** |
| Quota | Per-user **Slot Quota** (charged to creators only, C-01); per-user submit rate; per-user concurrent streams |
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
| RISK-07 | Webhooks are unauthenticated at the provider side (no signature) | Secret path, constant-time check; webhooks only trigger a refresh, never carry state (§5.6) |

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
| **Capability State** | Whether this account can currently use a Kind Capability: `available`, `unavailable` (e.g. plan restriction), `unknown`. |
| **Hoster Catalogue** | The provider's list of supported web hosters (domains/regex, status, daily limits). |
| **Source** | What a User submits: magnet, `.torrent` bytes, `.nzb` bytes, NZB URL, or web URL (+ optional password). |
| **Job Kind** | `torrent`, `nzb`, `web`. |
| **Fingerprint** | Identity of a Source's content used for deduplication (§5.1). |
| **Pending Submission** | A record written before a provider create call, so an interrupted call can be reconciled. |
| **Remote Item** | The single item at the Provider Account for one Fingerprint; shared by all Jobs that claim it. |
| **Provider Item ID** | The provider's identifier of a Remote Item (string). |
| **Job** | A User's claim on a Remote Item; the core noun. |
| **Claim** | The relation between an unreleased Job and its Remote Item. |
| **Slot Charge** | The one Claim of an active Remote Item that counts against its owner's Slot Quota. |
| **Slot Quota** | Maximum number of active Remote Items carrying a User's Slot Charge. |
| **Lifecycle** | Provider-neutral state machine of a Remote Item (§5.2). |
| **Remote File** | A file inside a Remote Item: Provider File ID (string), path, size. Known only once the provider reports it. |
| **Delivery Target** | What a Signed Link points to: `file` (one Remote File) or `bundle` (the provider-built archive of the whole item). |
| **Bundle Format** | `zip` (TorBox) or `rar` (Real-Debrid). |
| **Telemetry** | Volatile progress data (progress, speed, ETA, peers, stalled). Not part of the Lifecycle. |
| **Stall** | No progress for a configured time while in an active state. |
| **Sync** | Updating Remote Items from provider state. |
| **Observation** | One provider snapshot of a Remote Item, translated to neutral terms, stamped with debroxy's observation time. |
| **Unmanaged Item** | An item at the Provider Account that debroxy does not track and no Pending Submission matches. |
| **Signed Link** | Expiring HMAC-signed URL to one Delivery Target, served through debroxy. |
| **CDN URL** | The provider's direct download URL. A **secret**: never shown, never logged. |
| **Outbox Message** | A side effect recorded in the same transaction as a state change, executed later at least once. |

---

## 3. Bounded contexts

### 3.1 Overview

| Context | Subdomain | Owns | Persistent state |
|---|---|---|---|
| `identity` | Supporting | Users, roles, credentials, OIDC links, sessions | yes |
| `provider` | Generic (anti-corruption layer) | Provider Accounts, capability state, hoster catalogues, per-provider HTTP behaviour, translation of APIs and webhooks | in-memory caches only (DEC-21) |
| `job` | **Core** | Remote Items, Jobs, Pending Submissions, Lifecycle, dedup, quotas, sync, outbox | yes |
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

**Aggregate `RemoteItem`** (`job/domain/remoteitem`): the consistency boundary for dedup, claims and the slot charge (DEC-06).

| Field | Type | Notes |
|---|---|---|
| id | `RemoteItemID` (UUIDv7) | |
| providerID | `ProviderID` | |
| providerItemID | `ProviderItemID` (string) | unique per provider among non-deleted items |
| kind | `Kind` | `torrent`, `nzb`, `web` |
| fingerprint | `Fingerprint` | unique per provider among **claimable** items (§5.2) |
| name, sizeBytes | | from observations |
| lifecycle | `Lifecycle` | §5.2 |
| failureReason | string? | |
| files | `[]RemoteFile{FileID string, Path, SizeBytes}` | empty until the provider reports files (TorBox: when finished or when cached; Real-Debrid: right after creation) |
| bundle | `BundleAvailability{Format zip\|rar, RangeSupported bool}`? | present when the provider can serve a bundle (§5.9) |
| perFileDelivery | bool | false for Real-Debrid items with more than one selected file (C-04) |
| expiresAt | time? | provider expiry (TorBox: +24 h torrents, +30 days web on the observed plan) |
| telemetry | `Telemetry{Progress 0–1, BytesPerSecond, ETA?, Peers?, Stalled}` | `ETA` absent when the provider reports its "unknown" sentinel |
| lastProgressAt | time | debroxy clock; drives stall detection |
| lastObservationAt | time | debroxy clock; drives stale-observation rejection (DEC-09b) |
| claims | `[]Job` | **unreleased** jobs only |
| slotCharge | `JobID?` | the claim charged for the active slot (C-01) |

**Entity `Job`** (inside `RemoteItem`): `id` (UUIDv7, chosen by the caller), `ownerID`, `ownerUsername` (snapshot), `displayName`, `claimedAt`, `origin` (`created` | `joined`).
Released jobs move to the `job_history` read model (owner, display name, provider, kind, claimedAt, releasedAt, final state). They are visible to their owner and admins (resolves OQ-09).

**Value object `Fingerprint`** (DEC-07, updated)

| Source | Fingerprint | Evidence |
|---|---|---|
| Magnet | `btih:<40 hex lowercase>` (base32 converted); `btmh:<hex>` only if no btih | TorBox matched uppercase bare magnets to the same item |
| `.torrent` | SHA-1 of the bencoded `info` dictionary → `btih:`; v2-only → `btmh:` | both providers report the same hash |
| `.nzb` | `nzb:<sha256 of raw bytes>` (ASM-U03) | unverified |
| NZB URL | `nzburl:<normalized URL>` | unverified |
| Web URL | `url:<normalized URL>` + `#pw:<sha256(password)>` when a password is given | TorBox's own key is MD5 of the URL **string as submitted** |

URL normalization: scheme and host lowercased, default port and fragment removed, query kept. debroxy submits the URL exactly as the user gave it; the normalized form is only for dedup.

**Aggregate `PendingSubmission`** (`job/domain/pendingsubmission`, C-05)

| Field | Notes |
|---|---|
| jobID, ownerID, ownerUsername, displayName | the job to create |
| providerID, kind, fingerprint, providerHash | `providerHash` = the hash the provider will report (btih; MD5 of the URL for TorBox web), used for reconciliation |
| startedAt | |
| state | `in_flight` → `completed` \| `abandoned` |

**Aggregate `SlotQuota`**: `{UserID, MaxActiveJobs}`; default from configuration.

### 5.2 Lifecycle state machine (ddd-rules SM-1…SM-4)

| State | Meaning | Active | Claimable | Terminal |
|---|---|---|---|---|
| `queued` | Waiting at the provider | ✓ | ✓ | |
| `resolving_metadata` | Magnet or NZB being resolved | ✓ | ✓ | |
| `awaiting_file_selection` | Real-Debrid waits for selection (the adapter selects immediately) | ✓ | ✓ | |
| `downloading` | Provider downloading (incl. stalled, see Telemetry) | ✓ | ✓ | |
| `processing` | Post-download work (TorBox `processing`/`uploading`, Real-Debrid `uploading`/`compressing`) | ✓ | ✓ | |
| `ready` | Delivery targets can be downloaded | | ✓ | |
| `failed` | Provider gave up | | | |
| `gone` | Disappeared upstream (expired or deleted outside debroxy) | | | |
| `deleting` | Last claim released; provider deletion pending | | | |
| `deleted` | | | | ✓ |

**Sync transitions** (`Observe`; the only table sync may use):

| From \ To | queued | resolving_metadata | awaiting_file_selection | downloading | processing | ready | failed |
|---|---|---|---|---|---|---|---|
| queued | | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| resolving_metadata | ✓ | | ✓ | ✓ | ✓ | ✓ | ✓ |
| awaiting_file_selection | ✓ | | | ✓ | ✓ | ✓ | ✓ |
| downloading | ✓ | | | | ✓ | ✓ | ✓ |
| processing | | | | ✓ | | ✓ | ✓ |
| ready | | | | | | | |
| failed | | | | | | | |

**Command and reconciliation transitions** (each with its own method, rule and test):

| Method | From | To |
|---|---|---|
| `MarkGone` (authoritative absence or provider not-found) | any active state, `ready`, `failed` | `gone` |
| `RequestDeletion` (last claim released) | any active state, `ready`, `failed` | `deleting` |
| `RequestDeletion` | `gone` | `deleted` (no provider call) |
| `ConfirmDeleted` | `deleting` | `deleted` |

Rules:
- A forbidden sync transition is not applied; the anomaly is logged with both states (DEC-09).
- **Claimable** states form the dedup scope. `failed`, `gone`, `deleting`, `deleted` are not claimable, so a new submission of the same content creates a new item. The partial unique index on (`provider_id`, `fingerprint`) covers claimable states only.
- `processing → downloading` exists for usenet repair (ASM-U04).

### 5.3 Domain behaviour of `RemoteItem`

| Method | Rules |
|---|---|
| `NewRemoteItem(id, provider, providerItemID, source, firstJob, observation, now)` | First claim has `origin=created` and carries the slot charge if the item is active |
| `Claim(job, now)` | Only in claimable states (`ErrRemoteItemUnavailable` otherwise). One unreleased job per owner (`JobAlreadyExistsError{ExistingJobID}`). `origin=joined`; **never charged** (C-01) |
| `Release(jobID, actor, now)` | Owner or admin (`CanActorReleaseJob`). Records `JobReleased{job, finalState}` (history read model). If the charged job is released and claims remain while active, the charge moves to the **oldest remaining claim** and `SlotChargeTransferred` is recorded. If no claims remain → `RequestDeletion` and `LastClaimReleased` |
| `Observe(observation)` | Rejects observations whose `ObservedAt` < `lastObservationAt` (DEC-09b). Applies the sync table, name, size, files (replace when reported), bundle, perFileDelivery, expiresAt, telemetry. Updates `lastProgressAt` when progress increases. On leaving the active states, clears the slot charge. Records `LifecycleAdvanced` |
| `DetectStall(now, threshold)` | Active, no progress since `threshold` → `Telemetry.Stalled = true` (unless the provider already says so); records `StallDetected` once |
| `MarkGone(now)`, `ConfirmDeleted(now)` | as in §5.2 |

Policy `CanActorSeeJob(actor, job)`: owner or admin; repositories enforce it (ddd-rules R-3).

### 5.4 Use cases

| Use case | Type | Actor | Summary |
|---|---|---|---|
| `SubmitJob` | command | user | §5.5 |
| `ReleaseJob` | command | owner / admin | `RemoteItem.Release`; outbox `DeleteRemoteItem` when the last claim goes |
| `RenameJob` | command | owner | |
| `RefreshProviderState` | command | system / user (forced) | §5.6 |
| `SyncRemoteItems` | command | system | §5.6 |
| `HandleProviderWebhook` | command | webhook adapter | §5.6 |
| `ReconcilePendingSubmissions` | command | system (after each authoritative sync) | §5.6 |
| `DetectStalls` | command | system (periodic) | §5.3 |
| `ExecuteRemoteItemDeletion`, `ExecuteProviderItemDeletion` | command | outbox processor | §5.7 |
| `SetSlotQuota` | command | admin | |
| `JobsOfUser`, `JobDetails`, `JobHistory` | query | user | |
| `AllJobs`, `UnmanagedItems`, `ParkedOutboxMessages` | query | admin | |
| `CachedAvailability` | query | user | needs capability `CachedCheck` |
| `DeliveryTarget` | query | delivery | `{UserID, JobID, Target}` → provider reference + target details (§5.9) |
| `QuotaUsage` | query | user | `{slotsUsed, slotsMax, submitsLeftThisHour, streamsMax}` |

### 5.5 `SubmitJob` flow

Command: `SubmitJob{Actor, JobID, ProviderID, Source, DisplayName?}`

1. **Validate.** Provider exists and is enabled. The kind capability exists and the account capability state is not `unavailable` (else `capability-not-supported` / `capability-unavailable-for-plan`). For web URLs: the URL matches the provider's Hoster Catalogue and the hoster is enabled (else `unsupported-hoster`). Compute the Fingerprint.
2. **Join path.** In one transaction: find a claimable Remote Item for (provider, fingerprint). If found → `Claim`. **No quota check, no provider call** (C-01). Done.
3. **Create path.**
   1. Check the user's **submit rate** (DEC-24) and **slot quota** (count of active items carrying the user's slot charge).
   2. In one transaction: insert a `PendingSubmission(in_flight)` (C-05).
   3. Call `ProviderGateway.CreateRemoteItem` outside any transaction (ddd-rules A-9) → provider item ID + first Observation.
   4. In one transaction:
      - Look up by **provider item ID** first (TorBox returns the existing ID for known content; ASM-T04 confirmed), then by (provider, fingerprint).
      - Found and claimable → `Claim` it with `origin=joined`, mark the pending submission `completed`. If our provider item ID differs from the found item's, write `DeleteProviderItem` for ours.
      - Not found → re-check the slot quota; insert the Remote Item with the job (`origin=created`, charged); mark the pending submission `completed`.
      - Re-checked quota fails → write `DeleteProviderItem` for the new provider item; mark the pending submission `abandoned`; return `slot-quota-exceeded`.
   5. If step 3.3 fails ambiguously (timeout, connection reset), the pending submission stays `in_flight` and the user gets `submission-pending` (HTTP 202). Reconciliation resolves it (§5.6).
4. A unique-constraint violation in 3.4 is never surfaced; it triggers the look-up again.

Errors: `invalid-source`, `upload-too-large`, `capability-not-supported`, `capability-unavailable-for-plan`, `unsupported-hoster`, `submit-rate-exceeded`, `slot-quota-exceeded`, `job-already-exists`, `provider-unavailable`, `provider-rejected`.

**Slot-quota consistency.** The count reads other `RemoteItem` aggregates inside the same `BEGIN IMMEDIATE` transaction. This relies on SQLite's single-writer serialization. **ADR required** (D-11 exception).

### 5.6 Sync

**Observation** = `{ProviderItemID, Kind, ProviderHash, State, Name, SizeBytes, Files?, Bundle?, PerFileDelivery, ExpiresAt?, Telemetry, FailureReason?, ObservedAt}`, where `ObservedAt` is **debroxy's clock at request start** (C-09). Provider timestamps are never used for ordering.

**`RefreshProviderState{ProviderID, MaxAge, Forced}`**
- No-op if the last authoritative sync is younger than `MaxAge` (default 10 s), unless `Forced` (then `MaxAge` = 0; user-forced refreshes are rate limited per user).
- Calls `ProviderGateway.ListRemoteItems` (authoritative; covers every kind the account can use), then `SyncRemoteItems{Authoritative=true}`, then `ReconcilePendingSubmissions`.
- Single-flight per provider.

**`SyncRemoteItems{ProviderID, Observations, Authoritative}`**
- Matches by provider item ID → `Observe`.
- Unmatched observations are offered to reconciliation first, then recorded as Unmanaged Items (never adopted by default, DEC-10).
- Authoritative: claimable items missing from the observations → `MarkGone`. Exceptions: items created less than 60 s ago, and items with an in-flight provider call.

**`ReconcilePendingSubmissions`**
- An `in_flight` pending submission whose `providerHash` matches an unmatched observation → complete it as in §5.5 step 3.4 (TorBox and Real-Debrid both report the btih; TorBox reports the URL MD5 for web).
- `in_flight` older than 15 min (configurable) with no match → `abandoned`; the job is reported as failed in the user's history with `submission-lost`.

**Triggers**
- **Refresh-on-read:** the job HTTP adapter calls `RefreshProviderState` before `JobsOfUser`, `JobDetails`, `AllJobs`. This stays active in webhook mode, because TorBox webhooks only report some completions (C-07).
- **Webhook:** `POST /webhooks/{providerId}/{secret}` → `HandleProviderWebhook` → `ProviderGateway.TranslateWebhook` (verifies the secret in constant time, parses) → `RefreshProviderState{MaxAge=0}`. Webhook payloads never change state directly.
- **Safety refresh:** periodic (default 5 min while any item is active, 30 min otherwise).
- **Stall detection:** periodic `DetectStalls` (threshold default 30 min).

**Per-provider sync strategy** (inside the provider adapter):
- TorBox: `mylist?bypass_cache=true` for each usable kind (torrents, web, usenet when available) plus the queued list. One request per kind.
- Real-Debrid: walk `/torrents` pages for IDs (authoritative presence), then `/torrents/info/{id}` for every non-terminal item, because list status is stale (RD-N04).

### 5.7 Outbox (pattern: transactional outbox)

| Message | Written by | Executed by | Success |
|---|---|---|---|
| `DeleteRemoteItem{RemoteItemID}` | `RequestDeletion` | `ExecuteRemoteItemDeletion` → `ProviderGateway.DeleteRemoteItem` → `ConfirmDeleted` | provider confirms, or a follow-up lookup reports not-found |
| `DeleteProviderItem{ProviderID, Kind, ProviderItemID}` | submit race / quota re-check | `ExecuteProviderItemDeletion` → `ProviderGateway.DeleteRemoteItem` | same |

- Retries with exponential back-off (1 s … 1 h); parked after 20 attempts; admins see parked messages.
- **Deletion is verified, not assumed**: a delete error (TorBox answers a repeated delete with 500 `DATABASE_ERROR`) is followed by a lookup. Only a lookup that returns not-found counts as success.
- Real-Debrid download-history entries are cleaned by the adapter (DEC-25).

### 5.8 Persistence (SQLite, sketch)

| Table | Key columns |
|---|---|
| `remote_items` | `id`, `provider_id`, `provider_item_id`, `kind`, `fingerprint`, `provider_hash`, `state`, `name`, `size_bytes`, `failure_reason`, `bundle_format`, `bundle_range_supported`, `per_file_delivery`, `expires_at`, `telemetry_json`, `last_progress_at`, `last_observation_at`, `slot_charge_job_id`, `created_at`. Unique (`provider_id`, `provider_item_id`) where state ≠ `deleted`; unique (`provider_id`, `fingerprint`) where state is claimable |
| `remote_files` | `remote_item_id`, `file_id`, `path`, `size_bytes` |
| `jobs` | unreleased claims: `id`, `remote_item_id`, `owner_id`, `owner_username`, `display_name`, `origin`, `claimed_at` |
| `job_history` | released jobs (read model) |
| `pending_submissions` | `job_id`, `owner_id`, `provider_id`, `kind`, `fingerprint`, `provider_hash`, `state`, `started_at` |
| `slot_quotas`, `submit_rate_windows` | |
| `provider_sync_state` | `provider_id`, `last_authoritative_sync_at` |
| `unmanaged_items` | `provider_id`, `provider_item_id`, `provider_hash`, `name`, `kind`, `first_seen_at`, `last_seen_at` |
| `outbox_messages` | `id`, `type`, `payload_json`, `created_at`, `attempts`, `next_attempt_at`, `last_error`, `processed_at`, `parked_at` |

Each context owns its tables (DEC-13).

### 5.9 Delivery targets

| Target | Available when | Source at provider |
|---|---|---|
| `file:<FileID>` | item `ready`, file reported, `perFileDelivery = true` | TorBox `requestdl` with `file_id`; Real-Debrid unrestrict of the single link (single selected file only) |
| `bundle` | item `ready`, `bundle` present | TorBox `requestdl … zip_link=true` (**Range not supported**); Real-Debrid unrestrict of the single link when more than one file is selected (RAR, range assumed supported, ASM-R15) |

File identity is the **provider file ID**, never the position in the `.torrent` (TorBox IDs don't follow torrent order).

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
| capabilityState | map Kind Capability → `available` \| `unavailable{reason, since}` \| `unknown` (C-03) |
| hosterCatalogue | cached list (refresh 1 h) |
| webhook | `{Enabled, Secret}` |

**Capability state rules**
- Starts `unknown` for plan-dependent capabilities (`SubmitNZBFile`, `SubmitNZBURL`, `SubmitWebURL`), `available` for the rest.
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
| `SubmitNZBFile`, `SubmitNZBURL` | plan-dependent | ✗ | TorBox 500 `PLAN_RESTRICTED_FEATURE` on plan 1 |
| `SubmitWebURL` | ✓ hoster catalogue only | ✗ in v1 (DEC-16) | `UNSUPPORTED_SITE` for direct links |
| `PerFileDelivery` | ✓ | single-file items only | RAR packaging |
| `BundleDelivery` | ✓ zip (no Range) | ✓ rar (multi-file items) | observed |
| `CachedCheck` | ✓ | ✗ (`disabled_endpoint`) | observed |
| `Webhooks` | ✓ refresh hint only | ✗ | observed |
| `ProviderDedup` | ✓ (same ID for known content) | ✗ (new item per add) | observed; informational |

The API advertises kind capabilities **and** capability state per provider. A missing kind capability → `capability-not-supported` (501); an `unavailable` state → `capability-unavailable-for-plan` (409).

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
- Startup check: resolve the provider hosts and confirm the pinned family is reachable; otherwise refuse to start.

### 9.4 Rate budgets

| Provider | Budget | Value | Source |
|---|---|---|---|
| TorBox | general API, per endpoint | 300 / 60 s | observed headers |
| TorBox | uncached create (torrent, web) | **60 / hour** | observed headers |
| TorBox | CDN zip | 60 / 60 s | observed headers |
| Real-Debrid | whole API | 250 / 60 s | ASM-R11 (no headers seen) |

- Budgets are tracked per Provider Account and updated from `x-ratelimit-*` headers when present.
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

`shared` never reveals who else holds a claim. `slotCharged` tells the user whether this job counts toward their quota.

### 10.3 Slug catalogue (additions and changes vs revision 1)

| Slug | Type | When |
|---|---|---|
| `capability-unavailable-for-plan` | conflict | account plan lacks the feature |
| `unsupported-hoster` | incorrect-input | URL not in the Hoster Catalogue, or hoster disabled |
| `submit-rate-exceeded` | quota-exceeded | per-user submit rate |
| `submission-pending` | (HTTP 202, not an error) | ambiguous provider failure; reconciliation running |
| `submission-lost` | — (history state) | pending submission abandoned |
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
  pendingSubmissionTimeout: "15m"
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
| NFR-09 | **Resilience**: per-provider budgets, retries with jitter on idempotent calls, circuit breaker. Creates are never blindly retried; ambiguous failures go through Pending Submissions (§5.5) |
| NFR-10 | **Testability**: every driven port has a memory or fake implementation; provider adapters are tested against the recorded fixtures in `docs/discovery/` |
| NFR-11 | **Tolerant reader**: unknown provider fields are ignored; unknown states and error codes are logged once per value |

---

## 13. Decision log

| ID | Decision | Basis | ADR |
|---|---|---|---|
| DEC-01 | Cross-context calls only via consumer ports and supplier `ports/in` | DEP-4 | |
| DEC-02 | `provider` exposes operations (A-10) | ACL pattern | **required** (A-10 is a MAY rule but changes the CQRS default) |
| DEC-03 | Usernames immutable; jobs keep a username snapshot | simplicity | |
| DEC-04 | OIDC logs in pre-linked users only | isolation | |
| DEC-05 | Login throttling per username and per IP | pattern | |
| DEC-06 | Claims are entities inside `RemoteItem` | one consistency boundary for the ref-count | |
| DEC-07 | Fingerprint rules (§5.1), incl. the web password | content addressing; password-protected links differ | |
| DEC-08 | Slot quota counts active items carrying the user's slot charge; checked in the create transaction by reading other aggregates | owner decision; the quota check reads other aggregates | **required** (D-11 exception) |
| DEC-09 | Forbidden sync transitions are ignored and logged | tolerant reader | |
| DEC-09b | Observations stamped and ordered by debroxy's clock; older ones dropped | stale observations could overwrite newer state; RD-N03 | |
| DEC-10 | Unmanaged items are listed, not adopted (except pending-submission matches) | isolation | |
| DEC-11 | Refresh-on-read stays on in webhook mode; webhooks only trigger refresh | T12 refuted | |
| DEC-12 | Periodic safety refresh and stall detection | reconciliation loop | |
| DEC-13 | Each context owns its tables | pattern | |
| DEC-14 | User-Agent per Provider Account, for API and CDN | user requirement | |
| DEC-15 | Real-Debrid adapter selects all files immediately | provider-neutral `job` | |
| DEC-16 | Real-Debrid web links unsupported in v1 | no persistent item; narrow hoster coverage | |
| DEC-17 | Signed Links are HMAC capability URLs with key rotation | pattern | |
| DEC-18 | Global and per-user stream caps | resource protection; one user must not take all streams | |
| DEC-19 | IDs are UUIDv7 | pattern | |
| DEC-20 | Provider side effects via transactional outbox with verified deletion | pattern; submit-race duplicates; TorBox delete errors are ambiguous | |
| DEC-21 | `provider` keeps capability state, budgets and hoster catalogue in memory only (rebuilt at startup) | no persistence needed | |
| DEC-22 | Generic auth middleware in `common/server`, wired in `cmd` | `common/server` must not depend on `identity` | |
| DEC-23 | Acyclic context graph; webhooks enter through `job` | no dependency cycle between contexts | |
| DEC-24 | Per-user submit rate; cached submits limited separately | one user could drain the shared create budget; T13 | |
| DEC-25 | Real-Debrid `/downloads` history entries are deleted by the adapter when their Remote Item is deleted and by a daily sweep of entries older than 24 h | RD-N05 | |
| DEC-26 | Egress pinned per Provider Account (IP family / local address) | dual-stack finding | |
| DEC-27 | Pending Submissions with hash-based reconciliation | interrupted submits can create untracked items | |
| DEC-28 | Slot charge moves to the oldest remaining claim when the charged job is released | C-01 | |
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

---

## 15. Next steps

1. ADRs for every decision marked **ADR required**, plus the core decisions DEC-06, DEC-08, DEC-20, DEC-27, DEC-29.
2. Optional discovery top-ups: usenet with a plan that allows it; Real-Debrid failure states; TorBox queueing; RAR range support.

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

Resolved since revision 1: OQ-02 (webhook authenticity: secret path only, refresh-hint semantics), OQ-05 (`user_ip` omitted), OQ-09 (history read model).
