# 0007. Uniqueness only among claimable items; deferral instead of cross-aggregate revive

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 7 (unchanged sections from revisions 5–6 apply), §5.1 (`deletedAt`, `Submission.AttemptStartedAt`), §5.2 rules and `Revive`, §5.3, §5.4 (`ExpireSubmissions`, `ResumeDeferredSubmissions`), §5.5 step 5, §5.6 steps 2–5, §5.7, §5.8 ; decisions DEC-31, DEC-32, DEC-34, DEC-36, DEC-37, DEC-39, DEC-44 ; changes C5-01, C5-02, C5-04, C5-10, C6-01, C6-02 |
| Related | ADR 0002 (SQLite), ADR 0004 (claims), ADR 0005 (submission), ADR 0006 (Revive charge), ADR 0008 (outbox deletion), ADR 0009 (refresh pipeline); ddd-rules D-11, SM-1…SM-4, R-2, R-5, R-10; evidence torbox.md T04, T06, realdebrid.md RD-N01 |

Revised 2026-10-11 for revision 6.

## Context
debroxy needs uniqueness so that the same content maps to one shared item (ADR 0004), and so that a provider observation maps to at most one live item.

Forces:
- **TorBox returns existing IDs.** Re-adding known content returns the same item ID (torbox.md T04). That ID may belong to a `failed` or `gone` item, or to one that is being deleted.
- **Real-Debrid never does.** Every add creates a new item (RD-N01).
- **Revision 2/3 uniqueness over all non-deleted states.** A `failed`/`gone` item kept its fingerprint and provider ID reserved. A resubmit then hit the index and looped.
- **Revision 3 revive and cancellation.** A new submission revived the old item and cancelled its pending deletion. That changed another aggregate and raced with an outbox delete already sent to the provider.
- **Deletion race.** S's create can return P because X's content still exists. X's delete then removes the content and X is confirmed `deleted` before S completes.
- **D-11.** A flow may read other aggregates but change only one. R-10 runs those reads inside the write transaction.

## Decision
debroxy enforces uniqueness only among **claimable** items (DEC-31). A submission never changes another item. Collisions with an item being deleted are resolved by waiting, not by reviving, and a completion checks whether the content it got was deleted meanwhile (DEC-39).

**Uniqueness**
1. Claimable states: `submitting`, `queued`, `resolving_metadata`, `awaiting_file_selection`, `downloading`, `processing`, `ready`.
2. Three partial unique indexes on `remote_items`, each `WHERE state IN (claimable states)`:
   - (`provider_id`, `fingerprint`);
   - (`provider_id`, `provider_hash`), where the provider hash is known. NZB items may report no usable hash (ASM-U02) and are then outside this index;
   - (`provider_id`, `provider_item_id`), where `provider_item_id IS NOT NULL`.
3. `failed`, `gone`, `deleting` and `deleted` items never block a new item for the same fingerprint, provider hash or provider item ID. Their claimants keep their failed jobs; a resubmit joins or starts a new item.
4. Join look-ups (ADR 0005 rule 2) use the same claimable predicate as the indexes.
5. A unique-index violation is never shown to the user; the transaction retries from its look-up.

**No cross-aggregate change**
6. No flow revives, cancels or otherwise changes an item other than the one it loaded. A new submission does not touch a `failed`, `gone` or `deleting` item that holds the same content. Look-ups of other items run inside the write transaction through a read-only reads port (R-10).
7. The outbox deletion protects a takeover by **reading** only. Transaction 1 calls `SupersedeDeletion` when a claimable item holds the same provider item ID (ADR 0008, DEC-37).

**Create outcome with provider item ID P** (in the submission's transaction, ADR 0005 rules 7–10, in this order)
8. A claimable item holds P → `merged-into` (DEC-36).
9. A `deleting` item holds P → `DeferSubmission(blocker)`. `ResumeDeferredSubmissions` starts a new attempt once the blocker is `deleted`. On TorBox the provider then returns a **new ID** (observed: torbox.md §7, fixtures `30-readd-after-delete-*`). Correctness does not depend on this: the new attempt starts after the blocker's `deletedAt`, so whatever ID it returns refers to content that exists after the deletion.
10. **Deferral and expiry are reconciled** (C5-04). A deferred item is still `submitting`, but `ExpireSubmissions` skips items with `DeferredBehind`; its 20 min timeout applies only to undeferred items with an attempt in progress (C6-02). A deferred item's limit is its blocker's deletion: when the blocker's `DeleteRemoteItem` message is parked (ADR 0008), `ExpireSubmissions` fails the item with `blocked-by-deletion` (ADR 0005 rule 14). If the blocker is superseded instead (a claimable item took P), the blocker is `deleted`, the resumed create returns P held by that claimable item, and rule 8 merges.
11. **Late deletion check** (DEC-39, C5-01). An item that held P has `deletedAt ≥ submission.AttemptStartedAt` (both fields in §5.1, C5-02) → do not complete; start a new attempt at once. The check only **reads** the other item (D-11). `deleted` rows are kept 30 days (§5.8), far longer than any attempt, so the row is always there to read.
12. Otherwise `CompleteSubmission(P)`. A `failed`/`gone` item holding P stays unchanged.

**No re-submission fallback** (DEC-39)
13. Revision 4 added a fallback: if the deletion's transaction 2 found a claimable item holding the deleted provider ID, it would request a re-submission of that item. Revision 5 drops it, because with rules 7, 9 and 11 that state cannot be reached except by provider ID reuse. Under the single writer (ADR 0002), a claimable item Y can hold X's provider ID P at transaction 2 only if Y got P in one of these ways:
    - **Y completed while X was `deleting`.** Impossible: X is `deleting` from its `RequestDeletion` until transaction 2, and Y's outcome transaction then defers (rule 9).
    - **Y completed before X became `deleting`.** Then X was not claimable (rule 2 forbids two claimable holders of P), so X was `failed` or `gone`. A `gone` X goes straight to `deleted` without a provider call (§5.2). A `failed` X goes to `deleting`, and its transaction 1 finds Y and supersedes (rule 7).
    - **Y completed after X's transaction 2.** Then transaction 2 is over and found nothing; the late deletion check (rule 11) protects Y.
    - **Y revived onto P.** While X is `deleting`, impossible: `Revive` is blocked while a `deleting` item holds P (rule 14, C6-01, DEC-44), and §5.6 step 5 ignores the observation. Before X became `deleting`, the second case applies (Y claimable, so X was `failed` or `gone`, and transaction 1 supersedes). After transaction 2, the third case applies.

    What remains is a provider that re-uses P for different content. Transaction 2 then logs the anomaly, still confirms X, and the next complete refresh marks Y `gone` (§5.7). There is no `ResubmitRemoteItem` message, no return transition to `submitting`, and the Source is not kept after completion (§5.1).

**Revive** (§5.2, §5.3, §5.6 step 4, C5-10)
14. `Revive(observation)` runs only in the refresh pipeline, for an observation that steps 2 and 3 did not consume, when all of these hold:
    - its provider ID belongs to a `failed` or `gone` item;
    - its state maps to an active state or `ready`;
    - **no claimable item conflicts on any of the three unique keys** of the candidate: provider item ID, fingerprint, or provider hash. A fingerprint or hash conflict can arise when a newer claimable item for the same content exists under another provider ID (e.g. Real-Debrid, RD-N01);
    - **the observation is newer than the `deletedAt` of every item that held that provider item ID** (C7-01): a listing fetched before a confirmed deletion cannot revive onto removed content.
    - **no `deleting` item holds the observation's provider item ID** (C6-01, DEC-44). The observation may show content whose provider delete is already in flight; reviving onto it would let that delete remove the revived item's content (rule 13).
15. **Several candidates.** When several `failed`/`gone` items hold the observation's provider ID, only the **newest by `created_at`** is revived; the others stay as they are.
16. A blocked revive changes nothing and is logged. If a `deleting` item holds the provider ID, the observation is ignored (§5.6 step 5); otherwise (a key conflict) it continues to §5.6 step 6 (Unmanaged Items, DEC-10). The pipeline checks the keys and the `deleting` holder through the reads port before calling `Revive` (R-10), so the index never aborts the refresh transaction; the index stays the backstop (rule 5).
17. Revive is never triggered by a submission and never applies to `deleting` or `deleted` items. Observations of `deleting` items are ignored (§5.6 step 5), also when a `failed`/`gone` item holds the same provider ID (rule 14).
18. Revive adds no job. Charge fields follow ADR 0006 rule 8.

**Transitions touched** (SM-4 table in spec §5.2)

| Method | From | To |
|---|---|---|
| `DeferSubmission` | `submitting` | `submitting` (`DeferredBehind` set) |
| `Revive` | `failed`, `gone` (no key conflict; no `deleting` holder of the provider item ID; newest candidate) | the observation's mapped state |
| `SupersedeDeletion` | `deleting` | `deleted` (`deletedAt` set) |

Invariant: at any moment, for each provider, at most one claimable item per fingerprint, per provider hash and per provider item ID.

## Consequences
### Positive
- A resubmit after a failure or disappearance always works; it never loops on an index.
- Every flow changes one aggregate. Revision 3's revive-versus-delete races are gone.
- Provider-hash uniqueness among claimable items keeps the pairing of interrupted submissions 1:1 (ADR 0005 rule 13).
- The deletion-takeover race is closed by one extra read on completion; no lease or TTL machinery.
- No fallback machinery: no re-submission message type, processor, return transition or retained Source (DEC-39).
- `Revive` can never hit a unique index, and its target is deterministic.
- `Revive` never resurrects content that an in-flight deletion is about to remove, so the deletion's transaction 2 anomaly is reachable only by provider ID reuse (rule 13).

### Negative
- Several non-claimable rows can hold the same fingerprint or provider ID. Queries for history must expect that.
- A deferred submission is exempt from the 20 min timeout and waits for its blocker's deletion outcome: with retries from 1 s to 1 h and parking after 20 attempts, that can take up to about 8 h before it fails with `blocked-by-deletion` (ADR 0008).
- The late deletion check costs one extra upload in the rare interleaving.
- If a provider re-used an item ID for different content, the affected item ends `gone` and its users must resubmit; nothing re-creates it automatically.
- A revive blocked by a key conflict leaves the content visible only as an Unmanaged Item; one blocked by a `deleting` holder leaves it invisible until the deletion ends. In both cases its former claimants must resubmit (they then join or start a claimable item).

### Risks and mitigations
- *Provider re-uses an item ID for different content*: not observed with either provider. The only path to the transaction 2 anomaly (rule 13); logged, then `gone`. Accepted.
- *Resolved by discovery top-up (2026-10-11):* re-added content gets a new provider item ID after deletion (torbox.md §7). No open point remains.

## Alternatives considered
**Uniqueness over all non-deleted states** (revisions 2 and 3). A `failed`/`gone` item kept its keys. TorBox returned the same ID on resubmit, the insert hit the index, and the flow looped. Fixing it needed revive or supersede logic on another aggregate.

**Cross-aggregate revive with outbox cancellation** (revision 3, old DEC-31). The submission revived the `failed`/`gone`/`deleting` item and cancelled its deletion. That changes two aggregates per transaction (D-11). It also raced: the provider delete could already be in flight when the cancellation committed, or the item could reach `deleted`, which has no revive edge.

**Lease row per (provider, provider item ID)** (considered after revision 4). The deleter holds the lease from transaction 1 to 2; a completion must acquire it or defer. It adds lease and TTL machinery and still fails when a provider call outlives the TTL. Rejected in favour of deferral and the late deletion check.

**Re-submission fallback in the deletion's transaction 2** (revision 4). It needed a new message type, the Source kept after completion, and a transition from a completed state back to `submitting`, for a state that rule 13 shows unreachable except by provider ID reuse. Dropped (DEC-39).

## Compliance
- Repository contract tests (T-3): for each of the three keys, two claimable items fail to insert; a claimable item next to a `failed`, `gone`, `deleting` or `deleted` item with the same key succeeds; a `Revive` that would create a duplicate claimable key fails and rolls back (backstop).
- Migration check: the SQLite schema defines the three partial indexes with exactly the claimable predicate of rule 1 (a test compares the index `WHERE` clause with the domain's claimable set).
- Domain unit tests: `Revive` allowed only from `failed`/`gone`; rejected from every other state (SM-4).
- Pipeline tests (fake gateway): an observation with P held by a claimable item and a `failed` item → `Observe` on the claimable one, no revive; P of a `deleting` item → ignored; P held by a `gone` item and by a `deleting` item → no revive, logged, observation ignored (no Unmanaged Item); P of a `gone` item, active → `Revive`; P of a `gone` item whose fingerprint or provider hash a claimable item holds → no revive, logged, Unmanaged Item; P held by two `gone` items → only the newer is revived.
- `ExpireSubmissions` tests with a fake clock: a deferred item older than 20 min is not expired while its blocker is `deleting`; it fails with `blocked-by-deletion` when the blocker's message is parked.
- Race tests with controllable ordering: (1) create returns P while X is `deleting` → deferred, then resumed; (2) late deletion order → no completion onto P, second create runs; (3) Y's create returns P while X is between transaction 1 and transaction 2 → Y defers, and X's transaction 2 finds no claimable holder; (4) N concurrent submissions after a failure → one new item, no index error reaches the caller; (5) an observation of P arrives while X is between transaction 1 and transaction 2 and a `failed` item Y holds P → Y is not revived, and X's transaction 2 finds no claimable holder.
- Review checklist: no use case loads and saves two `RemoteItem`s in one transaction; no code revives an item outside the refresh pipeline; no re-submission message exists.
