# 0004. Jobs are entities inside the RemoteItem aggregate; a claim is their relation to it

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 7 (unchanged sections from revisions 5–6 apply), §2 (Job, Claim), §5.1, §5.2, §5.3, §5.4, §5.5 step 3, §5.8 ; decisions DEC-06, DEC-07 ; changes C5-11 |
| Related | ADR 0002 (SQLite), ADR 0005 (submission state), ADR 0006 (slot charge), ADR 0007 (uniqueness), ADR 0008 (outbox deletion); ddd-rules D-1, D-2, D-10, D-11, D-12, R-2, R-3, A-3; evidence torbox.md T04, T11, T13, realdebrid.md RD-N01, R11 |

Revised 2026-10-11 for revision 5.

## Context
Several isolated users may submit the same content to the same Provider Account. debroxy holds that content once at the provider and lets each user see it as their own Job.

Forces:
- **Reference counting.** The provider item may be deleted only when no user still wants it. The count of unreleased jobs and the deletion decision must be consistent, or a release can delete content another user just claimed.
- **Slot charge.** Who pays for an active item depends on which jobs are still unreleased (ADR 0006). That rule also needs a consistent view of all claims of one item.
- **One aggregate per transaction (D-11).** Join, release and charge changes must each touch one aggregate.
- **Provider behaviour differs.** TorBox deduplicates by infohash and by MD5 of the web URL and returns the same item ID (torbox.md T04, T11). Real-Debrid creates a new item on every add (realdebrid.md RD-N01). debroxy's own fingerprint is the only dedup that works for both.
- **Growth.** If released claims stayed in the aggregate, a popular item would grow without bound and become a hot row under `BEGIN IMMEDIATE`.
- **Web passwords.** TorBox keeps one item per URL. Password variants of one URL cannot be held apart at the provider. Whether TorBox accepts a password at all is only secondary evidence (ASM-T26).
- **NZB URLs.** Usenet items may report no hash usable for reconciliation (ASM-U02).

## Decision
debroxy models each Job as an entity inside the `RemoteItem` aggregate. A Claim is not a separate object: it is the relation between an unreleased Job and the Remote Item that contains it (§2). The aggregate is the consistency boundary for dedup, claims, submission and slot charge (DEC-06).

1. `RemoteItem` (package `job/domain/remoteitem`) is the aggregate root. `Job` is an entity inside it with `id`, `ownerID`, `ownerUsername` (snapshot, DEC-03), `displayName`, `claimedAt`, `origin` (`created` | `joined`). Fields are unexported (D-1).
2. The aggregate holds **only unreleased jobs**. Each of them is one claim on the item; "the item's claims" and "the item's unreleased jobs" are the same set.
3. The caller generates the `JobID` (UUIDv7, DEC-19) and passes it in the command (A-3).
4. All claim changes go through aggregate methods, inside one `UpdateRemoteItem` transaction per item (R-2). No flow changes two `RemoteItem`s in one transaction (D-11).
5. `Claim(job, now)` (`now` is a parameter, D-10):
   - allowed only in claimable states (`submitting`, `queued`, `resolving_metadata`, `awaiting_file_selection`, `downloading`, `processing`, `ready`); otherwise `ErrRemoteItemUnavailable`;
   - at most one unreleased job per owner per item; otherwise `JobAlreadyExistsError{ExistingJobID}`;
   - a `JobID` already present returns `ErrDuplicateJobID`; the use case turns it into an idempotent answer (ADR 0005, DEC-38);
   - sets `origin = joined`. Whether the claim is charged follows ADR 0006.
6. `Release(jobID, actor, now)` (`now` is a parameter, D-10):
   - only the owner or an admin (policy `CanActorSeeJob`, enforced by the repository before `updateFn`, R-3);
   - removes the job from the aggregate and records `JobReleased{job, finalState}` (D-12). **`finalState` is the Remote Item's Lifecycle state at the moment of release**, read before `RequestDeletion` runs, so it is never `deleting` or `deleted` (§5.1, C5-11). The `job_history` row stores it;
   - **first** checks whether any claims remain. If none remain, it calls `RequestDeletion` and records `LastClaimReleased`. The provider deletion, if one is needed, runs through the outbox (ADR 0008);
   - otherwise applies the charge rule of ADR 0006.
7. The repository writes the released job to the `job_history` read model in the same transaction as the release. History is visible to the owner and to admins. Retention: 180 days by default (§5.8).
8. **Fingerprint** is a value object built by a constructor that validates it (D-2). Rules (DEC-07, §5.1):

   | Source | Fingerprint |
   |---|---|
   | Magnet | `btih:<40 hex lowercase>` (base32 converted); `btmh:<hex>` only if no btih |
   | `.torrent` | SHA-1 of the bencoded `info` dictionary → `btih:`; v2-only → `btmh:` |
   | `.nzb` | `nzb:<sha256 of raw bytes>` (ASM-U03) |
   | NZB URL | `nzburl:<normalized URL>`; submission **disabled** until ASM-U02 is verified |
   | Web URL | `url:<normalized URL>`; the **password is not part of the fingerprint** |

9. URL normalization lowercases scheme and host, removes the default port and the fragment, and keeps the query. The provider always receives the URL exactly as the first submitter gave it.
10. For web URLs, the first submission's password is used. Later joiners' passwords are ignored, and the API tells them so.
11. Fingerprints are unique per provider **among claimable items only** (ADR 0007).
12. When the last claim is released, `RequestDeletion` moves the item out of the claimable states, as §5.2 defines (no other outcome is possible):

    | State at release of the last claim | Result |
    |---|---|
    | `submitting` | `deleting` without a provider item; the in-flight create is resolved by ADR 0005 |
    | any other active state, `ready`, or `failed` **with** a provider item ID | `deleting`; the outbox `DeleteRemoteItem` deletes the provider item (ADR 0008) |
    | `gone`, or `failed` **without** a provider item ID | `deleted` at once; no provider call |

    So `failed` and `gone` items without claims do not accumulate (§5.8).

Invariants (domain unit tests):
- I-1: no two claims share a `JobID` or an owner.
- I-2: a claimable item has at least one claim, except transiently inside `Release` before `RequestDeletion`.
- I-3: a `deleting` or `deleted` item has no claims.

## Consequences
### Positive
- Ref-count, deletion trigger and charge are decided against one consistent set of claims. No cross-aggregate lock is needed.
- Join and release are each one small transaction on one item.
- The aggregate stays small: released jobs live in `job_history`.
- Dedup works the same for both providers, independent of provider-side dedup.

### Negative
- Every claim operation on a popular item serializes on that item's row. With SQLite's single writer (ADR 0002) this costs nothing extra.
- A Job has no repository of its own. Queries by `JobID` go through a read model or an item lookup by job.
- Users with different web passwords for the same URL share one item and the first password.

### Risks and mitigations
- *Equal content under different fingerprints.* Example (an assumption, not observed in discovery): a v2-only `.torrent` (`btmh:`) and a magnet of the hybrid torrent with the same files (`btih:`). The provider may still deduplicate both into one item. Handled as `merged-into` (ADR 0005, DEC-36). The observed TorBox case (magnet and `.torrent` of the same v1 torrent, T04) gives equal fingerprints and needs no merge.
- *NZB byte variants* (whitespace) give different fingerprints (ASM-U03). Accepted; it only costs an extra item.
- *A history row is lost on crash*: prevented by writing it in the release transaction (rule 7).

## Alternatives considered
**Job as its own aggregate, with a ref-count on RemoteItem.** A join or release would change the Job and the RemoteItem counter in one transaction. That breaks D-11. Splitting it into two transactions with events makes the count eventually consistent, so a release could delete content that a concurrent join has just claimed.

**No sharing (one provider item per user).** Simple and isolated. On TorBox it cannot be enforced: the provider returns the same item ID for the same content (T04), so separate submissions collapse into one provider item anyway. On Real-Debrid it would work, but every duplicate uses a provider slot and API calls; Real-Debrid's rate limit is itself unverified (R11, ASM-R11). The expensive uncached-create budget (60 per hour) is a TorBox observation (T13) and is not an argument for Real-Debrid.

**Shared, but never deleted by users.** Avoids the ref-count. But the shared account fills up with items nobody wants, and released users' content stays at the provider until expiry. debroxy stores no retention policy of its own (§1.4), so nothing would clean up.

**Web password as part of the fingerprint** (revision 2). Rejected because TorBox keeps one item per URL. Two fingerprints would point to one provider item and break the 1:1 pairing by provider hash.

## Compliance
- Domain unit tests (`package remoteitem_test`, T-1): `Claim` in every non-claimable state returns `ErrRemoteItemUnavailable`; second claim by the same owner returns `JobAlreadyExistsError{ExistingJobID}`; same `JobID` returns `ErrDuplicateJobID`; releasing the last claim calls `RequestDeletion` and records `LastClaimReleased` and **no** `ChargeSpreadToClaimants`; `JobReleased.finalState` equals the state before release in every claimable state and in `failed`/`gone`; every row of the rule 12 table; invariants I-1…I-3 after every method.
- Fingerprint table test: every row of rule 8, including base32 magnets, uppercase hex, v2-only torrents, and URL normalization cases (port, fragment, case, query kept). Web URLs with different passwords give the same fingerprint.
- `SubmitJob` with an NZB URL returns `capability-not-enabled`.
- Repository contract tests (T-3): release writes the `job_history` row, with `finalState`, in the same transaction (rollback on `updateFn` error leaves both unchanged); `UpdateRemoteItem` with a non-owner actor fails before `updateFn` (R-3).
- Race test (T-3, T-7): N goroutines claim and release on one item; afterwards the claim count matches the successful operations, and exactly one `RequestDeletion` happens when the last claim goes.
- Review checklist: no repository for `Job`; no code path changes two `RemoteItem`s in one transaction; no domain code calls `time.Now()` (D-10).
