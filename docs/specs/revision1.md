# debroxy — Specification, Revision 1

| | |
|---|---|
| Revision | 1 |
| Status | Draft. Assumption-based; written **before** provider API discovery |
| Date | 2026-10-11 |
| Supersedes | — |
| Architecture rules | [`docs/ddd-rules.md`](../ddd-rules.md) (binding) |

## 0. About this document

### 0.1 Revision workflow

1. Each revision is a new file `docs/specs/revisionN.md`. Past revisions are never edited.
2. Revision N+1 starts with a **changelog** against revision N and an updated **assumption register** (§14).
3. Revision 2 follows live discovery of the TorBox and Real-Debrid APIs (§15). Every `ASM-*` item is then marked **Confirmed**, **Refuted** (with the observed behaviour), or **Unverified**.
4. Architecture Decision Records (ADRs) are written after discovery, not in revision 1.

### 0.2 Conventions

- **MUST / SHOULD / MAY** as in RFC 2119.
- `ASM-xx` marks an assumption about a provider or the environment that is not yet verified. Any requirement that depends on one cites it.
- `DEC-xx` marks a design decision taken in this revision (§13). Decisions marked *(pattern)* follow a common design pattern rather than an explicit user choice.
- Terms in **bold** at first use are glossary entries (§2).

---

## 1. Vision and scope

### 1.1 Problem

Debrid services (TorBox, Real-Debrid, …) fetch torrents, usenet posts and hoster files on their own infrastructure and serve the results over fast HTTP. One account costs money and is tied to one API key. Providers watch for account sharing, for example many IP addresses using one account.

### 1.2 Goal

debroxy is a self-hosted proxy:

- Several **Users** share one **Provider Account** per **Provider**.
- All provider traffic, both API calls and file bytes, leaves from the proxy's single IP address.
- Users are isolated. They see and control only their own **Jobs**.
- debroxy stores **no files**. Bytes stream from the provider's CDN through debroxy to the user.
- debroxy offers only what the provider API can do. Provider differences show up as **Capabilities**, not as emulation.

### 1.3 In scope (v1)

| Area | Included |
|---|---|
| Job kinds | Torrents (magnet, `.torrent` file), usenet (`.nzb` file, NZB link), web downloads (hoster/direct links) |
| Providers | TorBox (first), Real-Debrid (second). Several providers per deployment, **one account each** |
| Provider selection | The user names the provider explicitly for every new job |
| Users | Semi-trusted, strictly isolated. Roles: **Admin** and **User** |
| Authentication | Username + password, and external OIDC |
| Sharing | Identical content submitted by several users is held once at the provider and shared through **Claims** (reference counting) |
| Quota | Per-user **Slot Quota**: the maximum number of active jobs |
| File access | **Signed Links**: HMAC-signed, expiring URLs that work in any download tool, with Range/resume support |
| Zip download | Only when the provider offers it (capability `ZipDownload`) |
| Status updates | Provider webhooks when configured, otherwise an on-demand refresh when clients query |
| Persistence | Embedded SQLite (metadata only) |
| API | debroxy's own unified HTTP API, OpenAPI-first |

### 1.4 Out of scope (v1)

- Search, RSS, notifications other than provider webhooks, streaming or transcoding endpoints.
- debroxy-side job queueing. When the account's slots are full, the provider's own queue is used.
- debroxy-side retention or expiry. debroxy mirrors the provider.
- Emulating missing provider features (e.g. building zips locally).
- Pooling several accounts of the same provider, and automatic provider routing.
- Imitating any provider's API (compatibility facades).
- A web UI. The API is designed so one can be added later.
- Billing, self-registration.

### 1.5 Risks

| ID | Risk | Mitigation |
|---|---|---|
| RISK-01 | Provider terms of service may forbid sharing an account. Operating debroxy may violate them. | The operator's responsibility; documented in the README. debroxy does not try to evade detection beyond using one IP. |
| RISK-02 | Concurrent downloads from one IP may hit provider limits on connections or bandwidth. | Discovery (§15) measures this; rate limits per provider (§9). |
| RISK-03 | All file bytes pass through the proxy host, so its bandwidth is the bottleneck. | Operator sizing; future bandwidth quotas (not v1). |
| RISK-04 | A Signed Link is a bearer capability; a leaked link works until it expires. | Short configurable TTL; each request re-checks job ownership and user status. |

---

## 2. Ubiquitous language (glossary)

| Term | Definition |
|---|---|
| **User** | A person allowed to use debroxy. Has exactly one **Role**. |
| **Role** | `admin` or `user`. Admins manage users, quotas and all jobs. Users manage only their own jobs. |
| **Actor** | The authenticated User performing an action (ID + Role), as seen by a bounded context. |
| **Session** | A logged-in state, identified by an opaque **Session Token**. |
| **Provider** | A debrid service type: `torbox`, `realdebrid`. |
| **Provider Account** | One configured account of a Provider, identified by a **Provider ID** (e.g. `torbox`), with its own API key, User-Agent and limits. |
| **Capability** | A feature a Provider Account supports, e.g. `SubmitNZBFile`, `ZipDownload`. |
| **Source** | What a User submits: a magnet URI, `.torrent` bytes, `.nzb` bytes, an NZB URL, or a web URL. |
| **Job Kind** | `torrent`, `nzb` or `web`. Derived from the Source. |
| **Fingerprint** | Identity of a Source's content, used to find duplicates: BitTorrent infohash, NZB content hash, or normalized URL. |
| **Remote Item** | The single item held at the Provider Account for one Fingerprint (e.g. one TorBox torrent). Shared by every Job that claims it. |
| **Provider Item ID** | The provider's own identifier of a Remote Item. |
| **Job** | A User's claim on a Remote Item: what the user submitted and owns. The core noun of debroxy. |
| **Claim** | The relation between a Job and its Remote Item. The Remote Item lives on the provider while at least one Claim is active. |
| **Lifecycle** | The provider-neutral state machine of a Remote Item (§5.2). |
| **Remote File** | A file inside a Remote Item (path, size, provider file ID). |
| **Telemetry** | Volatile progress data of a Remote Item (progress, speed, ETA, peers). Not part of the Lifecycle. |
| **Slot Quota** | The maximum number of a User's Jobs whose Remote Items are in an active Lifecycle state at once. |
| **Sync** | Updating Remote Items from the provider's observed state. |
| **Observation** | One provider-reported snapshot of a Remote Item, already translated to neutral terms. |
| **Unmanaged Item** | An item present at the Provider Account that debroxy did not create and does not track. |
| **Signed Link** | An expiring, HMAC-signed URL that lets its holder download one Remote File (or the zip of a Remote Item) through debroxy. |
| **CDN Link** | The provider's direct download URL, resolved by debroxy just in time. Never shown to Users. |
| **Outbox Message** | A side effect recorded in the same transaction as a state change, executed later at least once. |

---

## 3. Bounded contexts

### 3.1 Overview

| Context | Subdomain type | Owns | Persistent state |
|---|---|---|---|
| `identity` | Supporting | Users, roles, credentials, OIDC links, sessions | yes |
| `provider` | Generic (anti-corruption layer) | Provider Account configuration, capabilities, per-provider HTTP behaviour, translation of provider APIs and webhooks | no (configuration only) |
| `job` | **Core** | Remote Items, Jobs (claims), Lifecycle, dedup, slot quota, sync, outbox | yes |
| `delivery` | Supporting | Signed Links, byte streaming | no (signing keys from configuration) |

### 3.2 Context map

```
                ┌────────────┐
   HTTP ───────►│  identity  │◄──────────── (all HTTP adapters: session → Actor)
                └─────▲──────┘
                      │ UserStatusChecker
┌──────────┐  JobFiles │          ┌────────────┐
│ delivery │──────────┼─────────►│    job     │
└────┬─────┘          │          └─────┬──────┘
     │ CDNLinkResolver│                │ ProviderGateway
     ▼                │                ▼
┌─────────────────────┴────────────────────────┐
│                  provider                    │──► TorBox / Real-Debrid HTTP APIs
└──────────────────────────────────────────────┘
      ▲ webhook (adapters/in) ──► job: SyncRemoteItems
```

| Consumer → Supplier | Consumer port (`ports/out`) | Supplier port (`ports/in`) | Relationship |
|---|---|---|---|
| job → provider | `ProviderGateway` | `provider` operations | Customer/Supplier; provider is the ACL |
| delivery → job | `JobFiles` | `job` query `DownloadableTarget` | Customer/Supplier |
| delivery → provider | `CDNLinkResolver` | `provider` operation `ResolveCDNLink` | Customer/Supplier |
| delivery → identity | `UserStatusChecker` | `identity` query `UserStatus` | Customer/Supplier |
| provider → job | `ObservationSink` (driven by `HandleWebhook`) | `job` commands `SyncRemoteItems`, `RefreshProviderState` | Published language: Observation |
| HTTP (all contexts) → identity | — (composition) | `identity` query `AuthenticatedActor` | Conformist; see below |

Authentication across contexts: `identity/adapters/in/http` provides the authentication middleware. `cmd` mounts it in front of every context's HTTP adapter. The middleware stores a shared-kernel `common/auth.Principal{UserID, Role}` in the request context. Each context's HTTP adapter converts the `Principal` into its own `ports/in.Actor` (ddd-rules DEP-6), so no context's application layer sees a transport or identity type.

- **DEC-01** All cross-context calls follow ddd-rules DEP-4: the consumer declares a port, and an adapter in the consumer calls the supplier's `ports/in`.
- **DEC-02** The `provider` context holds no persistent state. Its driving port is a set of **operations** on the external system rather than CQRS commands and queries (ddd-rules A-10).

---

## 4. Context: `identity`

### 4.1 Model

**Aggregate `User`** (`identity/domain/user`)

| Field | Type | Rules |
|---|---|---|
| id | `UserID` (UUIDv7) | immutable |
| username | `Username` | 3–32 chars, `[a-z0-9._-]`, lowercase, unique, **immutable** (DEC-03) |
| passwordHash | `PasswordHash` (optional) | argon2id; absent for OIDC-only users |
| oidcIdentity | `OIDCIdentity{Issuer, Subject}` (optional) | unique per (issuer, subject) |
| role | `Role` (`admin`, `user`) | |
| status | `UserStatus` (`active`, `disabled`) | |
| createdAt | time | |

Invariants:
- A user has at least one way to log in (password or OIDC identity).
- The last active admin cannot be disabled or demoted (`ErrLastAdmin`).
- Passwords: at least 12 characters; checked by `NewPassword` before hashing.

**Aggregate `Session`** (`identity/domain/session`)

| Field | Rules |
|---|---|
| id | UUIDv7 |
| userID | |
| tokenHash | SHA-256 of the token; the raw token is never stored |
| createdAt, lastSeenAt, expiresAt | absolute lifetime and idle timeout from a configured `Factory` (ddd-rules D-5) |
| revokedAt (optional) | |

### 4.2 Use cases

| Use case | Type | Actor | Behaviour |
|---|---|---|---|
| `BootstrapAdmin` | command | CLI (no actor) | Allowed only while no user exists. Creates the first admin. |
| `CreateUser` | command | admin | Username, role, and an initial password and/or OIDC identity. |
| `ChangeUserRole` | command | admin | Last-admin rule. |
| `DisableUser` / `EnableUser` | command | admin | Disabling revokes all of the user's sessions. Jobs are kept; signed links stop working (delivery re-checks status). |
| `ResetUserPassword` | command | admin | Revokes the user's sessions. |
| `ChangeOwnPassword` | command | user | Requires the current password. Revokes the user's other sessions. |
| `LinkOIDCIdentity` / `UnlinkOIDCIdentity` | command | admin | Sets or clears the issuer/subject. |
| `LogInWithPassword` | command | anonymous | Input: username, password, session token (generated by the driving adapter, ddd-rules A-3). Uniform `invalid-credentials` error for unknown user, wrong password or disabled user. |
| `LogInWithOIDC` | command | anonymous | The driving adapter completes the authorization code + PKCE flow and passes the verified issuer, subject and a session token. Logs in only users linked beforehand (DEC-04). |
| `LogOut` | command | user | Revokes the current session. |
| `AuthenticatedActor` | query | — | Session token → Actor (user ID, role), or `authentication` error. Updates `lastSeenAt` at most once per minute (handled as a separate command `TouchSession` by the adapter). |
| `UserStatus` | query | — | For delivery: active/disabled. |
| `Users`, `UserDetails` | query | admin | |

### 4.3 Transport details

- Session token: 32 random bytes, base64url.
- Accepted from `Authorization: Bearer <token>` (API clients) or the cookie `debroxy_session` (HttpOnly, Secure, SameSite=Strict) for a future UI.
- Cookie-authenticated state-changing requests MUST carry a matching `Origin` header (CSRF defence).
- Password login is throttled per username and per client IP: exponential back-off after 5 failures (DEC-05, pattern).

---

## 5. Context: `job` (core)

### 5.1 Model

**Aggregate `RemoteItem`** (`job/domain/remoteitem`). It is the consistency boundary for deduplication and claims (DEC-06, ddd-rules D-11).

| Field | Type | Notes |
|---|---|---|
| id | `RemoteItemID` (UUIDv7) | debroxy's own ID |
| providerID | `ProviderID` | |
| providerItemID | `ProviderItemID` | provider's ID; unique per provider |
| kind | `Kind` (`torrent`, `nzb`, `web`) | |
| fingerprint | `Fingerprint` | unique per provider among non-terminal items |
| name | string | from the provider; may change on sync |
| sizeBytes | int64 | 0 until known |
| lifecycle | `Lifecycle` | §5.2 |
| failureReason | string (optional) | set when `failed` |
| files | `[]RemoteFile{FileID, Path, SizeBytes}` | replaced as a whole on sync |
| telemetry | `Telemetry{ProgressPercent, BytesPerSecond, ETA, Peers, Stalled}` | volatile |
| zipAvailable | bool | provider can serve a zip of this item |
| jobs | `[]Job` | the claims |
| lastObservedAt | time | |

**Entity `Job`** (inside `RemoteItem`)

| Field | Notes |
|---|---|
| id | `JobID` (UUIDv7), generated by the client of the command (ddd-rules A-3) |
| ownerID | User ID |
| ownerUsername | snapshot (usernames are immutable, DEC-03) |
| displayName | user-chosen; defaults to the Remote Item name |
| claimedAt | |
| releasedAt (optional) | once set, the job is history only |

**Value object `Fingerprint`** (DEC-07)

| Source | Fingerprint |
|---|---|
| Magnet URI | `btih:<40 hex lowercase>`. Base32 btih is converted to hex. A `btmh` (v2) value is used only if no btih is present: `btmh:<hex>` |
| `.torrent` file | SHA-1 of the bencoded `info` dictionary → `btih:<hex>`. v2-only torrents → SHA-256 → `btmh:<hex>` |
| `.nzb` file | `nzb:<sha256 of raw bytes>` |
| NZB URL, web URL | `url:<normalized URL>`. Scheme and host lowercase, default port removed, fragment removed, query kept as is |

**Value object `Source`**: `{Kind, Variant (magnet|torrentFile|nzbFile|nzbURL|webURL), Payload}`, validated by `NewSource`. The upload size limit comes from configuration (default 10 MiB) through a `Factory`. Optional `Password` for web links (ASM-T11).

**Aggregate `SlotQuota`** (`job/domain/slotquota`): `{UserID, MaxActiveJobs}`. Users without a row use the configured default (DEC-08).

### 5.2 Lifecycle state machine (ddd-rules SM-1…SM-4)

| State | Meaning | Active (counts to quota) | Terminal |
|---|---|---|---|
| `queued` | Accepted by the provider, waiting (provider queue) | ✓ | |
| `resolving_metadata` | Magnet metadata or NZB being resolved | ✓ | |
| `awaiting_file_selection` | Provider needs a file selection (Real-Debrid) | ✓ | |
| `downloading` | Provider is downloading (includes stalled/paused; see `Telemetry.Stalled`) | ✓ | |
| `processing` | Post-download work at the provider (upload to storage, compress, repair/extract) | ✓ | |
| `ready` | Files can be downloaded | | |
| `failed` | Provider gave up (error, virus, dead torrent) | | |
| `gone` | Disappeared upstream (expired, deleted outside debroxy) | | |
| `deleting` | Last claim released; provider deletion pending (outbox) | | |
| `deleted` | Deleted at provider, or never needed deleting | | ✓ |

Allowed transitions (the table is the source of truth, mirrored in code):

| From \ To | queued | resolving_metadata | awaiting_file_selection | downloading | processing | ready | failed | gone | deleting | deleted |
|---|---|---|---|---|---|---|---|---|---|---|
| queued | | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | |
| resolving_metadata | ✓ | | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | |
| awaiting_file_selection | ✓ | | | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | |
| downloading | ✓ | | | | ✓ | ✓ | ✓ | ✓ | ✓ | |
| processing | | | | ✓ | | ✓ | ✓ | ✓ | ✓ | |
| ready | | | | | | | | ✓ | ✓ | |
| failed | | | | | | | | ✓ | ✓ | |
| gone | | | | | | | | | | ✓ |
| deleting | | | | | | | | | | ✓ |
| deleted | | | | | | | | | | |

Rules:
- Sync transitions come from Observations. An Observation that maps to a forbidden transition is **not** applied: the current state is kept and an anomaly is logged with both states (DEC-09). Repeated anomalies are fixed by correcting the mapping table in a later revision.
- `processing → downloading` exists because usenet repair can re-download (ASM-T14).
- Only commands lead to `deleting`; sync never does.
- `deleting → deleted` happens when the provider confirms deletion, or reports the item as not found.
- `gone → deleted` happens when the last claim is released; no provider call is needed.

### 5.3 Domain behaviour of `RemoteItem`

| Method | Rules |
|---|---|
| `NewRemoteItem(id, providerID, providerItemID, source, firstJob, observation, now)` | Creates the item with its first claim. Initial state comes from the creation Observation. |
| `Claim(job, now)` | Rejected if the item is `deleting`/`deleted` (`ErrRemoteItemBeingDeleted`), `failed`/`gone` (`ErrRemoteItemUnavailable`), or the owner already has an unreleased job on this item (`JobAlreadyExistsError{ExistingJobID}`). |
| `Release(jobID, actor, now)` | Allowed for the owner or an admin (policy `CanActorReleaseJob`). Already released → `ErrJobAlreadyReleased`. If no unreleased jobs remain: from `gone` → `deleted`; otherwise → `deleting`, and the event `ProviderDeletionRequested` is recorded. |
| `Observe(observation, now)` | Applies the state (§5.2 rules), name, size, files, telemetry, zip availability. Records `LifecycleAdvanced` on a state change. |
| `MarkGone(now)` | Item missing from an authoritative full listing. |
| `ConfirmDeleted(now)` | `deleting → deleted`. |
| `ActiveJobOf(userID)`, `UnreleasedJobs()`, `IsActive()` | predicates and queries |

Policy `CanActorSeeJob(actor, job)`: the owner or an admin. Repositories enforce it (ddd-rules R-3).

### 5.4 Use cases

| Use case | Type | Actor | Summary |
|---|---|---|---|
| `SubmitJob` | command | user | §5.5 |
| `ReleaseJob` | command | owner / admin | `RemoteItem.Release`; the outbox carries the provider deletion |
| `RenameJob` | command | owner | Changes `displayName` |
| `RefreshProviderState` | command | system (HTTP adapter before list/get queries) | §5.6 |
| `SyncRemoteItems` | command | system (webhook, refresh) | §5.6 |
| `ExecuteProviderDeletion` | command | system (outbox processor) | §5.7 |
| `SetSlotQuota` | command | admin | `{UserID, MaxActiveJobs ≥ 0}` |
| `JobsOfUser` | query | user | Own unreleased jobs with Remote Item state, telemetry, size, provider |
| `JobDetails` | query | owner / admin | Job plus files and zip availability |
| `AllJobs` | query | admin | Every unreleased job, with owner |
| `UnmanagedItems` | query | admin | Items seen at providers that debroxy does not track |
| `CachedAvailability` | query | user | `{ProviderID, Fingerprints[]}` → cached flag per fingerprint. Needs `CachedCheck`. Pass-through to the provider |
| `DownloadableTarget` | query | delivery | `{UserID, JobID, FileID or zip}` → provider ID, provider item ID, provider file ID, file name, size; checks ownership, `ready` state, file existence, zip availability |
| `SlotUsage` | query | user | `{used, max}` for the caller |

### 5.5 `SubmitJob` flow

Command: `SubmitJob{Actor, JobID, ProviderID, Source, DisplayName?}`

1. Validate: the provider exists and is enabled; the provider has the Capability for the Source variant (`unsupported` otherwise); compute the Fingerprint.
2. **Join path.** In one transaction, find a non-terminal Remote Item for (provider, fingerprint).
   - If one exists: if it is active, check the slot quota; then `Claim`. Done. **No provider call.**
3. **Create path.** If none exists:
   - Check the slot quota (fast fail).
   - Call `ProviderGateway.CreateRemoteItem(provider, source)` **outside** any transaction (ddd-rules A-9). The result is a provider item ID plus the first Observation.
   - In one transaction:
     - Look up (provider, fingerprint) again, in case another submission raced this one.
       - If it now exists: claim it. If its provider item ID differs from ours, also record a `ProviderDeletionRequested` outbox message for our orphan.
       - Otherwise: check the quota again and insert the new Remote Item with the job.
     - If the quota check fails here: write an outbox message deleting the just-created provider item, and return `slot-quota-exceeded`.
4. Provider-side duplicates: if the provider returns an existing item for the same content (ASM-T04), the provider item ID uniqueness constraint finds the existing Remote Item, and step 3 joins it.

Errors: `incorrect-input` (bad source, oversized upload), `unsupported`, `slot-quota-exceeded`, `job-already-exists`, `remote-item-being-deleted` (retry later), `provider-unavailable`, `provider-rejected` (provider refused the source, with the provider's message passed through after sanitizing).

Slot-quota check: count the distinct Remote Items in active states on which the user holds unreleased jobs. The count runs inside the same `BEGIN IMMEDIATE` transaction as the claim. Reading other aggregates as policy input is allowed; changing them is not (DEC-08).

### 5.6 Sync

**`SyncRemoteItems{ProviderID, Observations[], Authoritative bool}`**
- For each Observation, find the Remote Item by provider item ID and apply `Observe`.
- Observations with no matching Remote Item are upserted into the **Unmanaged Items** read model. They are never adopted (DEC-10).
- When `Authoritative` (a complete listing), non-terminal Remote Items missing from the Observations get `MarkGone`. Exceptions: items created within the last 60 s, and items in `deleting` (ASM-T05).

**`RefreshProviderState{ProviderID, MaxAge}`**
- If the provider's last authoritative sync is younger than `MaxAge` (configured, default 10 s), do nothing.
- Otherwise call `ProviderGateway.ListRemoteItems`, which spans every kind the provider supports, then call `SyncRemoteItems` with `Authoritative=true`.
- Concurrent refreshes for one provider are collapsed (single-flight).
- The last sync time is stored per provider.

**Triggers:**
- **Webhook configured:** the provider webhook adapter verifies authenticity and translates the payload. If the payload has full state, it produces Observations; otherwise it calls `RefreshProviderState` with `MaxAge=0`. When webhooks are enabled, the HTTP adapter skips the refresh before list/get queries (DEC-11).
- **No webhook:** the job HTTP adapter calls `RefreshProviderState` before `JobsOfUser`, `JobDetails` and `AllJobs`.
- A periodic safety refresh (default 15 min, configurable) runs in both modes so `gone` detection and webhook gaps are covered (DEC-12, pattern).

### 5.7 Outbox and provider deletion (pattern: transactional outbox)

- Outbox Messages are written in the same transaction as the aggregate change (ddd-rules R-6).
- A processor (a driving adapter in `job/adapters/in/outbox`) polls due messages and dispatches `ExecuteProviderDeletion{RemoteItemID}`.
- `ExecuteProviderDeletion` calls `ProviderGateway.DeleteRemoteItem`. On success or `not-found`: `ConfirmDeleted`, and the message is marked processed.
- On failure: retry with exponential back-off (1 s, 2 s, … up to 1 h, configurable). After 20 attempts the message is parked and shown to admins (`/admin/outbox`). The item stays `deleting`.
- Messages are idempotent: deleting a deleted item is a success.

### 5.8 Persistence (SQLite, sketch)

| Table | Key columns |
|---|---|
| `remote_items` | `id`, `provider_id`, `provider_item_id` (unique with provider), `kind`, `fingerprint`, `state`, `name`, `size_bytes`, `failure_reason`, `zip_available`, `telemetry_json`, `last_observed_at`, `created_at`. Partial unique index on (`provider_id`, `fingerprint`) where state is not `deleted` |
| `remote_files` | `remote_item_id`, `file_id`, `path`, `size_bytes` |
| `jobs` | `id`, `remote_item_id`, `owner_id`, `owner_username`, `display_name`, `claimed_at`, `released_at` |
| `slot_quotas` | `user_id`, `max_active_jobs` |
| `provider_sync_state` | `provider_id`, `last_authoritative_sync_at` |
| `unmanaged_items` | `provider_id`, `provider_item_id`, `name`, `kind`, `first_seen_at`, `last_seen_at` |
| `outbox_messages` | `id`, `type`, `payload_json`, `created_at`, `attempts`, `next_attempt_at`, `last_error`, `processed_at`, `parked_at` |

Identity tables: `users`, `sessions`. Each context owns its tables, and no context reads another's tables (DEC-13).

---

## 6. Context: `provider` (anti-corruption layer)

### 6.1 Model

**`ProviderAccount`** (loaded from configuration at startup, read-only at runtime)

| Field | Notes |
|---|---|
| id | `ProviderID`, slug, unique (e.g. `torbox`) |
| kind | `ProviderKind`: `torbox`, `realdebrid` |
| displayName | |
| enabled | |
| capabilities | `CapabilitySet` = the kind's defaults (§6.3) minus any the operator disables |
| credentials | API key reference (resolved from the environment or a file, never stored in the DB) |
| httpProfile | `{BaseURL, UserAgent, Timeout, RateLimit{RequestsPerSecond, Burst}, Retry{MaxAttempts, BaseDelay}, CircuitBreaker{FailureThreshold, OpenDuration}}` |
| webhook | `{Enabled, Secret}` |

- **User-Agent is configurable per Provider Account** and MUST be sent on every request to that provider, both API calls and CDN downloads (DEC-14). Default: `debroxy/<version>`.
- One HTTP client per Provider Account, so limits are isolated (ddd-rules OUT-4).

### 6.2 Operations (driving port, DEC-02)

| Operation | Input | Output | Capability |
|---|---|---|---|
| `ListProviders` | — | ID, kind, display name, enabled, capabilities | — |
| `CreateRemoteItem` | provider ID, Source | provider item ID, Observation | per Source variant |
| `ListRemoteItems` | provider ID | Observations (all kinds), `Authoritative` flag | — |
| `DescribeRemoteItem` | provider ID, kind, provider item ID | Observation | — |
| `DeleteRemoteItem` | provider ID, kind, provider item ID | — (`not-found` is reported distinctly) | — |
| `ResolveCDNLink` | provider ID, kind, provider item ID, file ID or zip | CDN URL, expiry hint | `ZipDownload` for zip |
| `CheckCached` | provider ID, kind, fingerprints | cached per fingerprint | `CachedCheck` |
| `HandleWebhook` | provider ID, path secret, headers, body | — (verifies, translates, then forwards Observations or a refresh request to `ObservationSink`) | `Webhooks` |

Internal provider-specific steps stay inside the adapter, invisible to `job`. For example, Real-Debrid's file selection step: the adapter selects all files right after creation (DEC-15, ASM-R03).

### 6.3 Capabilities

| Capability | Meaning | TorBox (assumed) | Real-Debrid (assumed) |
|---|---|---|---|
| `SubmitMagnet` | Create from a magnet URI | ✓ (ASM-T03) | ✓ (ASM-R02) |
| `SubmitTorrentFile` | Create from `.torrent` bytes | ✓ (ASM-T03) | ✓ (ASM-R02) |
| `SubmitNZBFile` | Create from `.nzb` bytes | ✓ (ASM-T10) | ✗ (ASM-R10) |
| `SubmitNZBURL` | Create from an NZB URL | ✓ (ASM-T10) | ✗ |
| `SubmitWebURL` | Create from a hoster/direct URL | ✓ (ASM-T11) | ✗ in v1 (DEC-16) |
| `ZipDownload` | Provider serves one zip of all files | ✓ (ASM-T17) | ✗ (ASM-R12) |
| `CachedCheck` | Ask whether content is instantly available | ✓ (ASM-T09) | ✗ (ASM-R08) |
| `Webhooks` | Provider pushes state changes | ✓ (ASM-T12) | ✗ (ASM-R13) |

- The API advertises capabilities per provider (`GET /api/v1/providers`).
- A request needing a missing capability fails with `unsupported` (HTTP 501) and the slug `capability-not-supported`, naming the capability.

### 6.4 Error translation

| Provider condition | Neutral error |
|---|---|
| Network error, timeout, 5xx, open circuit breaker | `provider-unavailable` |
| 429 / rate-limit response | retried by the adapter, then `provider-unavailable` with a retry-after hint |
| Invalid or expired API key | `provider-unavailable`, logged as an operator alert |
| Source refused (bad magnet, banned hoster, …) | `provider-rejected`, with a sanitized message |
| Item not found | `not-found` |
| Account limit reached (e.g. storage) | `provider-rejected` with slug `provider-account-limit` |

### 6.5 Adapter rules

- Provider JSON types are unexported and live only in `provider/adapters/out/<torbox|realdebrid>`.
- Native-to-neutral state mapping is one table per provider (§8).
- API keys never appear in logs, errors or traced URLs. TorBox's `requestdl` takes the key as a query parameter (ASM-T07), so the URL is redacted before logging.

---

## 7. Context: `delivery`

### 7.1 Signed Link

- **Format** (DEC-17, pattern: HMAC capability URL): `<publicBaseURL>/dl/<token>/<url-escaped file name>`.
  - The file name segment exists only so tools save the right name; it is not verified.
  - `token = base64url(payload) "." base64url(HMAC-SHA256(key[kid], payload))`.
- **Payload** (JSON, compact): `v` (format version, 1), `kid` (key ID), `uid`, `jid`, `fid` (file ID) or `zip: true`, `exp` (Unix seconds).
- **Keys:** the configuration holds an ordered list of `{ID, Secret}`. The first key signs; every key verifies, which allows rotation.
- **TTL:** configurable, default 6 h (OQ-03).
- A link carries no provider data and no CDN URL.

### 7.2 Use cases

| Use case | Type | Actor | Behaviour |
|---|---|---|---|
| `IssueSignedLink` | query (pure computation) | owner | Input: Actor, job ID, file ID or zip. Checks through `JobFiles` (`job.DownloadableTarget`) and returns the URL and expiry. |
| `StreamDownload` | — driving adapter flow, §7.3 | link holder | |

### 7.3 Streaming flow (`GET` and `HEAD /dl/{token}/{name}`)

1. Verify the token: format, key ID, HMAC (constant-time comparison), expiry → otherwise 403 `invalid-signed-link`, or 403 `signed-link-expired`.
2. `UserStatusChecker`: the user must be active → otherwise 403.
3. `JobFiles.DownloadableTarget(uid, jid, fid|zip)`: the job is unreleased and owned, the item is `ready`, and the file exists → otherwise 404 or 409.
4. `CDNLinkResolver.ResolveCDNLink`. Results are cached in memory per (provider, item, file|zip) until `min(expiry hint, configured max, default 30 min)` (ASM-T08).
5. Send the upstream request with the provider's User-Agent, forwarding `Range` and `If-Range`, plus `HEAD` for `HEAD`.
6. Return status 200/206/416 and copy only these headers: `Content-Length`, `Content-Range`, `Accept-Ranges`, `Content-Type`, `Last-Modified`, `ETag`. Set `Content-Disposition: attachment; filename*=UTF-8''<name>`.
7. Stream with a fixed-size buffer. Nothing touches disk. A client disconnect cancels the upstream request.
8. If the upstream answers 401/403/404/410 **before any byte has been sent**: drop the cache entry, resolve again once, and retry. Otherwise return 502 `provider-unavailable`.

Concurrency: there is no per-user stream limit in v1 (only slot quotas, §1.3). The server enforces a global maximum of concurrent streams (configurable, default 64) and answers 503 above it (DEC-18).

Which client IP TorBox's `requestdl` should receive (`user_ip`, if anything) is an open question for discovery (ASM-T07, OQ-05).

---

## 8. Provider state mapping (all assumed)

### 8.1 TorBox → Lifecycle (ASM-T14)

| TorBox signal | Lifecycle | Telemetry |
|---|---|---|
| item listed in the queue (`as_queued` / queued list) | `queued` | |
| `metaDL` | `resolving_metadata` | |
| `checkingResumeData`, `downloading` | `downloading` | |
| `stalled`, `stalled (no seeds)`, `paused` | `downloading` | `Stalled = true` |
| `uploading`, `compressing`, usenet repair/extract states | `processing` | |
| `completed`, `cached`, or `download_finished && download_present` | `ready` | |
| `error`, `failed`, any state starting with `failed` | `failed` | |
| missing from the authoritative listing | `gone` (via `MarkGone`) | |

### 8.2 Real-Debrid → Lifecycle (ASM-R09)

| Real-Debrid status | Lifecycle |
|---|---|
| `magnet_conversion` | `resolving_metadata` |
| `waiting_files_selection` | `awaiting_file_selection` (the adapter selects all files immediately, DEC-15) |
| `queued` | `queued` |
| `downloading` | `downloading` |
| `compressing`, `uploading` | `processing` |
| `downloaded` | `ready` |
| `magnet_error`, `error`, `virus`, `dead` | `failed` |

---

## 9. Provider profiles (assumptions to verify)

### 9.1 TorBox

| ID | Assumption |
|---|---|
| ASM-T01 | Base URL `https://api.torbox.app/v1/api`; authentication with `Authorization: Bearer <api key>`. |
| ASM-T02 | Every JSON response uses the envelope `{success, error, detail, data}`; errors have `success=false` and a machine-readable `error` code. |
| ASM-T03 | `POST /torrents/createtorrent` (multipart) accepts `magnet` or `file`, plus optional `name`, `seed`, `allow_zip`, `as_queued`; returns `torrent_id`, `hash`. |
| ASM-T04 | Submitting content already in the account returns the existing item instead of an error. |
| ASM-T05 | `GET /torrents/mylist` (with `bypass_cache=true`, optional `id`) returns all items with `id`, `hash`, `name`, `size`, `download_state`, `download_finished`, `download_present`, `progress`, `download_speed`, `eta`, `seeds`, `files[{id, name, short_name, size, mimetype}]`, `expires_at`. A fresh item may be missing from the listing for a short time. |
| ASM-T06 | `POST /torrents/controltorrent` `{torrent_id, operation}` with `delete`, `pause`, `resume`, `reannounce`. |
| ASM-T07 | `GET /torrents/requestdl?token=&torrent_id=&file_id=&zip_link=&user_ip=` returns the CDN URL in `data`; the API key goes in the `token` query parameter. |
| ASM-T08 | CDN URLs support HTTP Range, are valid for several hours, and are not bound to the requesting IP unless `user_ip` is given. |
| ASM-T09 | `GET /torrents/checkcached?hash=<h1,h2>&format=object&list_files=` reports cached state per hash. |
| ASM-T10 | Usenet: `POST /usenet/createusenetdownload` (`file` or `link`, `name`), `/usenet/mylist`, `/usenet/controlusenetdownload` `{usenet_id, operation}`, `/usenet/requestdl` (`usenet_id`, `file_id`, `zip_link`), `/usenet/checkcached`. |
| ASM-T11 | Web: `POST /webdl/createwebdownload` (`link`, optional `password`, `name`), `/webdl/mylist`, `/webdl/controlwebdownload`, `/webdl/requestdl`, `/webdl/checkcached`, `/webdl/hosters`. Direct HTTP links are accepted as well as hosters. |
| ASM-T12 | Webhooks: one URL per account, set in account settings. Events (e.g. download ready, failed) carry a type and item identifiers. No signature header, so authenticity relies on a secret in the URL. |
| ASM-T13 | Rate limits: a general per-second limit and stricter hourly limits on create endpoints; exceeding them returns 429. Exact values unknown. |
| ASM-T14 | The `download_state` values listed in §8.1; queued items appear through a separate queued listing; usenet post-processing may move back to downloading. |
| ASM-T15 | Items expire after plan-dependent inactivity and then disappear from `mylist`. |
| ASM-T16 | When the account's active slots are full, a new item is queued by TorBox (with `as_queued` or automatically) rather than rejected. |
| ASM-T17 | Zip download (`zip_link=true`) works for torrents, usenet and web downloads with more than one file. |
| ASM-T18 | Custom User-Agent values are accepted. Some default client User-Agents may be blocked. |
| ASM-T19 | Account info (`GET /user/me`) exposes plan, active-slot limit and expiry, useful for `/readyz` and admin display. |

### 9.2 Real-Debrid

| ID | Assumption |
|---|---|
| ASM-R01 | Base URL `https://api.real-debrid.com/rest/1.0`; authentication with `Authorization: Bearer <api token>`. |
| ASM-R02 | `POST /torrents/addMagnet` (form `magnet`) and `PUT /torrents/addTorrent` (raw body) return `{id, uri}`. |
| ASM-R03 | `POST /torrents/selectFiles/{id}` (`files=all` or comma-separated IDs) is required before downloading starts. |
| ASM-R04 | `GET /torrents/info/{id}` returns `status`, `progress`, `hash`, `filename`, `bytes`, `files[{id, path, bytes, selected}]`, `links[]` (one hoster link per selected file, in file order). |
| ASM-R05 | A CDN URL is obtained with `POST /unrestrict/link` (`link=<hoster link>`) → `{download, filename, filesize}`. |
| ASM-R06 | `DELETE /torrents/delete/{id}` returns 204. |
| ASM-R07 | `GET /torrents` is paginated (`page`/`limit` or `offset`) with an `X-Total-Count` header. A full walk counts as an authoritative listing. |
| ASM-R08 | `instantAvailability` is disabled; there is no cached check. |
| ASM-R09 | Status values as listed in §8.2. |
| ASM-R10 | No usenet support. |
| ASM-R11 | Rate limit of about 250 requests per minute; 429 when exceeded. |
| ASM-R12 | No zip download of a whole torrent. |
| ASM-R13 | No webhooks. |
| ASM-R14 | CDN links work from the IP that unrestricted them; downloads from other IPs may be refused or flagged. |

---

## 10. External API (OpenAPI-first)

Contract files: `api/openapi/{identity,job,delivery,provider}.yml`. All endpoints are under `/api/v1` except `/dl`, `/webhooks`, `/healthz` and `/readyz`. JSON everywhere except multipart uploads and file streams. Errors use `{"slug", "message"}` with the status mapping of ddd-rules IN-3.

### 10.1 Endpoints

| Method & path | Context / use case | Auth |
|---|---|---|
| `POST /api/v1/auth/login` | identity `LogInWithPassword` → `{token, expiresAt}` plus cookie | anonymous |
| `POST /api/v1/auth/logout` | identity `LogOut` | user |
| `GET /api/v1/auth/oidc/login` | redirect to the OIDC provider (state, PKCE) | anonymous |
| `GET /api/v1/auth/oidc/callback` | identity `LogInWithOIDC` | anonymous |
| `GET /api/v1/me` | identity `UserDetails` (self) + job `SlotUsage` | user |
| `PUT /api/v1/me/password` | identity `ChangeOwnPassword` | user |
| `GET /api/v1/providers` | provider `ListProviders` | user |
| `POST /api/v1/providers/{providerId}/cached-availability` | job `CachedAvailability` | user |
| `POST /api/v1/jobs` | job `SubmitJob`. JSON `{id?, providerId, magnet \| nzbUrl \| webUrl, password?, displayName?}`, or multipart with `torrentFile` / `nzbFile`. The server generates `id` if absent. → 201 with `Location` | user |
| `GET /api/v1/jobs` | job `JobsOfUser` (after refresh, §5.6) | user |
| `GET /api/v1/jobs/{jobId}` | job `JobDetails` | owner/admin |
| `PATCH /api/v1/jobs/{jobId}` | job `RenameJob` | owner |
| `DELETE /api/v1/jobs/{jobId}` | job `ReleaseJob` → 204 | owner/admin |
| `POST /api/v1/jobs/{jobId}/links` | delivery `IssueSignedLink`. `{fileId}` or `{zip: true}` → `{url, expiresAt}` | owner |
| `POST /api/v1/jobs/{jobId}/links:all` | one signed link per file (for download managers) | owner |
| `GET /api/v1/admin/users`, `POST`, `GET/PATCH /{userId}` | identity admin use cases | admin |
| `PUT /api/v1/admin/users/{userId}/slot-quota` | job `SetSlotQuota` | admin |
| `GET /api/v1/admin/jobs` | job `AllJobs` | admin |
| `DELETE /api/v1/admin/jobs/{jobId}` | job `ReleaseJob` as admin | admin |
| `GET /api/v1/admin/unmanaged-items` | job `UnmanagedItems` | admin |
| `GET /api/v1/admin/outbox` | parked outbox messages | admin |
| `GET, HEAD /dl/{token}/{name}` | delivery streaming | signed link |
| `POST /webhooks/{providerId}/{secret}` | provider `HandleWebhook` → job `SyncRemoteItems` / `RefreshProviderState` | webhook secret |
| `GET /healthz` | process liveness | none |
| `GET /readyz` | DB reachable; configured providers authenticated (cached, ASM-T19) | none |

### 10.2 Job representation (sketch)

```json
{
  "id": "0192…",
  "displayName": "ubuntu-24.04.1-desktop-amd64.iso",
  "provider": "torbox",
  "kind": "torrent",
  "state": "downloading",
  "telemetry": { "progressPercent": 42.5, "bytesPerSecond": 31457280, "etaSeconds": 120, "peers": 37, "stalled": false },
  "sizeBytes": 6203355136,
  "zipAvailable": false,
  "shared": true,
  "files": [ { "id": "0", "path": "ubuntu-24.04.1-desktop-amd64.iso", "sizeBytes": 6203355136 } ],
  "createdAt": "2026-10-11T10:00:00Z",
  "failureReason": null
}
```

`shared` tells the user that other users also claim the item. It never reveals who (isolation). Admin views add `owner`.

### 10.3 Slug catalogue (initial)

| Slug | Type | When |
|---|---|---|
| `invalid-request` | incorrect-input | malformed body |
| `invalid-source` | incorrect-input | unparsable magnet / torrent / NZB / URL |
| `upload-too-large` | incorrect-input | upload above limit |
| `invalid-credentials` | authentication | login failure |
| `session-expired` | authentication | |
| `forbidden` | authorization | role/ownership violation |
| `invalid-signed-link` | authorization | bad token |
| `signed-link-expired` | authorization | |
| `job-not-found` | not-found | also used for other users' jobs (no existence leak) |
| `provider-not-found` | not-found | |
| `job-already-exists` | conflict | same user, same content |
| `remote-item-being-deleted` | conflict | |
| `remote-item-not-ready` | conflict | link requested before `ready` |
| `last-admin` | conflict | |
| `slot-quota-exceeded` | quota-exceeded | |
| `capability-not-supported` | unsupported | |
| `provider-rejected` | incorrect-input | provider refused the source |
| `provider-account-limit` | conflict | provider storage or account limit |
| `provider-unavailable` | provider-unavailable | |
| `too-many-streams` | service-unavailable | global stream cap |

---

## 11. Configuration

YAML file (`--config`), with environment variables overriding individual secret fields. Validated once at startup (ddd-rules S-3).

```yaml
server:
  listen: ":8080"
  publicBaseURL: "https://debroxy.example.org"
  trustedProxies: ["10.0.0.0/8"]      # X-Forwarded-* trusted only from these
  maxConcurrentStreams: 64
database:
  path: "/var/lib/debroxy/debroxy.db"
identity:
  session: { absoluteLifetime: "720h", idleTimeout: "168h" }
  oidc:
    enabled: true
    issuerURL: "https://auth.example.org"
    clientID: "debroxy"
    clientSecretEnv: "DEBROXY_OIDC_CLIENT_SECRET"
    scopes: ["openid", "profile", "email"]
job:
  defaultSlotQuota: 3
  uploadLimitBytes: 10485760
  refreshMaxAge: "10s"
  safetyRefreshInterval: "15m"
  outbox: { maxAttempts: 20, maxBackoff: "1h" }
delivery:
  signedLinkTTL: "6h"
  cdnLinkCacheMax: "30m"
  signingKeys:
    - { id: "k1", secretEnv: "DEBROXY_SIGNING_KEY_K1" }
providers:
  - id: "torbox"
    kind: "torbox"
    displayName: "TorBox"
    enabled: true
    apiKeyEnv: "DEBROXY_TORBOX_API_KEY"
    userAgent: "debroxy/0.1"
    baseURL: "https://api.torbox.app/v1/api"
    timeout: "30s"
    rateLimit: { requestsPerSecond: 4, burst: 8 }
    retry: { maxAttempts: 3, baseDelay: "500ms" }
    circuitBreaker: { failureThreshold: 5, openDuration: "30s" }
    disabledCapabilities: []
    webhook: { enabled: false, secretEnv: "DEBROXY_TORBOX_WEBHOOK_SECRET" }
  - id: "realdebrid"
    kind: "realdebrid"
    displayName: "Real-Debrid"
    enabled: true
    apiKeyEnv: "DEBROXY_REALDEBRID_API_KEY"
    userAgent: "debroxy/0.1"
    baseURL: "https://api.real-debrid.com/rest/1.0"
    timeout: "30s"
    rateLimit: { requestsPerSecond: 3, burst: 5 }
```

Startup fails on: duplicate provider IDs, an unknown kind, a missing secret, no signing key, a `publicBaseURL` that is not absolute, or a disabled capability the kind doesn't have.

---

## 12. Non-functional requirements

| ID | Requirement |
|---|---|
| NFR-01 | **No file storage.** Uploads (`.torrent`, `.nzb`) are held in memory up to the upload limit and never written to disk. Streaming uses bounded buffers. No temp files. |
| NFR-02 | **Single egress IP.** All provider calls (API and CDN) originate from the debroxy process. |
| NFR-03 | **Secrets.** API keys, session tokens, signing keys and webhook secrets never appear in logs, errors, metrics or API responses. |
| NFR-04 | **Isolation.** A user can neither see nor infer another user's jobs. Other users' jobs return `job-not-found`. Only the `shared` flag reveals that sharing exists. |
| NFR-05 | **Single binary**, pure Go (no CGO), Linux amd64/arm64 and macOS builds. |
| NFR-06 | **Graceful shutdown.** Stop accepting requests, finish or cancel streams within a timeout, stop the outbox processor, close the DB. |
| NFR-07 | **Logging.** `log/slog` JSON; request ID per request; use case name and duration from decorators. |
| NFR-08 | **Deployment behind a TLS reverse proxy.** debroxy speaks plain HTTP. `Secure` cookies assume HTTPS at the edge. |
| NFR-09 | **Resilience.** Per-provider rate limiting, retries with jittered back-off on idempotent calls, and a circuit breaker. Create calls are not retried automatically after an ambiguous failure; the next sync reconciles via ASM-T04 or the unmanaged-item list. |
| NFR-10 | **Testability.** Every driven port has a memory or fake implementation. Provider adapters are tested against recorded fixtures (ddd-rules T-5). |

---

## 13. Decision log (revision 1)

| ID | Decision | Basis |
|---|---|---|
| DEC-01 | Cross-context calls only through consumer-declared ports and supplier `ports/in` | ddd-rules DEP-4 |
| DEC-02 | `provider` exposes operations, not CQRS (ddd-rules **A-10**) | pattern: anti-corruption layer |
| DEC-03 | Usernames are immutable, so jobs keep a username snapshot | simplicity, wild-workouts precedent |
| DEC-04 | OIDC logs in only users pre-linked by an admin (no just-in-time provisioning) | semi-trusted isolation |
| DEC-05 | Login throttling per username and per IP | pattern |
| DEC-06 | Claims (`Job`) are entities inside the `RemoteItem` aggregate | ref-count invariant needs one consistency boundary |
| DEC-07 | Fingerprint rules of §5.1 | pattern: content addressing |
| DEC-08 | Slot quota counts distinct active Remote Items per user; checked in the claim transaction | user choice + consistency |
| DEC-09 | Forbidden sync transitions are ignored and logged | pattern: tolerant reader |
| DEC-10 | Unmanaged items are listed, never adopted | isolation; OQ-01 |
| DEC-11 | Webhook mode disables refresh-on-read | user choice |
| DEC-12 | Periodic safety refresh | pattern: reconciliation loop |
| DEC-13 | Each context owns its tables | pattern: database per bounded context (logical) |
| DEC-14 | User-Agent configurable per Provider Account, used for API and CDN | user requirement |
| DEC-15 | Real-Debrid file selection is done automatically by the adapter (select all) | keep `job` provider-neutral |
| DEC-16 | Real-Debrid web links are unsupported in v1. Unrestrict creates no persistent item, so the Job model does not fit | revisit after discovery |
| DEC-17 | Signed Links are HMAC capability URLs with key rotation | pattern |
| DEC-18 | Global concurrent stream cap | resource protection |
| DEC-19 | IDs are UUIDv7 (time-ordered) | pattern |
| DEC-20 | State-changing side effects at providers go through a transactional outbox | pattern |

---

## 14. Assumption register

| ID | Topic | Status |
|---|---|---|
| ASM-T01 … ASM-T19 | TorBox API (§9.1) | Unverified |
| ASM-R01 … ASM-R14 | Real-Debrid API (§9.2) | Unverified |

---

## 15. Discovery plan (input to revision 2)

### 15.1 Method

- A discovery harness behind the `discovery` build tag. It is not part of the production binary.
- API keys come from the environment (supplied by the user, e.g. through 1Password). They are never written to files or fixtures.
- Every request and response is recorded and **sanitized**: keys, account IDs, e-mails and CDN URL tokens are removed. The results are stored as fixtures in `docs/discovery/<provider>/` for adapter tests.
- Mutating experiments use only test content supplied by the user (Linux ISO torrents and NZBs). Every created item is deleted at the end.
- Each experiment confirms or refutes specific `ASM-*` entries.

### 15.2 Experiments

| ID | Provider | Experiment | Verifies |
|---|---|---|---|
| E-01 | both | Authenticate; fetch account info; send custom User-Agent | T01, T02, T18, T19, R01 |
| E-02 | both | Add an ISO magnet; poll item state every 2 s until ready; record every state | T03, T05, T14, R02, R03, R04, R09 |
| E-03 | both | Add the same magnet again; add the same content as a `.torrent` | T04 (provider dedup), fingerprint rules |
| E-04 | both | Resolve a CDN link; HEAD and ranged GET; request the same link from a second IP if available; measure validity | T07, T08, R05, R14 |
| E-05 | TorBox | Zip link for a multi-file torrent | T17 |
| E-06 | TorBox | Cached check for a known-popular ISO hash and a random hash | T09, R08 |
| E-07 | TorBox | NZB upload and NZB link; record usenet states including post-processing | T10, T14 |
| E-08 | TorBox | Web download of a direct HTTP ISO link (and a hoster link if supplied) | T11 |
| E-09 | both | Delete items; delete again (idempotency); observe listing afterwards | T06, R06, T05 |
| E-10 | both | Read rate-limit headers; deliberately **no** limit-exhaustion testing without approval | T13, R11 |
| E-11 | TorBox | Fill active slots (if the plan allows) and observe queueing | T16 |
| E-12 | TorBox | Webhook delivery to a temporary endpoint (needs a publicly reachable URL; to be agreed with the user) | T12 |
| E-13 | Real-Debrid | Paginated listing with several items | R07 |

### 15.3 Test material needed from the user

- Linux ISO torrents: at least one single-file and one **multi-file** torrent, both as magnet and as `.torrent`.
- At least one `.nzb` file and, if possible, an NZB URL.
- A direct HTTP download URL and, optionally, a link on a hoster TorBox supports.
- A way to receive webhooks (public URL or tunnel) for E-12, or approval to skip it.

---

## 16. Open questions

| ID | Question |
|---|---|
| OQ-01 | Should admins be able to **adopt** unmanaged items (assign them to a user)? |
| OQ-02 | How to authenticate TorBox webhooks if there is no signature (secret path only?) |
| OQ-03 | Default Signed Link TTL: 6 h is a guess; long downloads over slow links may need more |
| OQ-04 | OIDC: match by e-mail for linking, or only by issuer/subject? Just-in-time provisioning later? |
| OQ-05 | Which `user_ip` to send to TorBox `requestdl`: omit, proxy IP, or the end user's IP (which would break the single-IP goal)? |
| OQ-06 | Is a per-provider outbound HTTP proxy (egress via a VPN or another IP) needed? |
| OQ-07 | Metrics backend (Prometheus endpoint?) and tracing (OpenTelemetry?) |
| OQ-08 | Container image and deployment shape (Docker image, systemd unit) |
| OQ-09 | Should released jobs remain visible as history to their owner? |
| OQ-10 | Should a user be able to pause/resume or reannounce (TorBox `controltorrent`) in v1? |
| OQ-11 | Per-user bandwidth or stream limits in a later version? |
