# 0005. A submission is a RemoteItem state (`submitting`)

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 7 (unchanged sections from revisions 5–6 apply), §5.1 (`Submission.NotBefore`, `AttemptStartedAt`), §5.2 (`DiscardSubmission`), §5.3, §5.4 (`ExecuteSubmission`, `ExpireSubmissions`, `ResumeDeferredSubmissions`), §5.5, §5.6 step 3, §5.7, §6.4 (HTTP 429), §9.4 ; decisions DEC-24, DEC-27, DEC-32, DEC-33, DEC-36, DEC-37, DEC-38, DEC-39, DEC-43 ; changes C5-01, C5-02, C5-04, C5-08, C5-11, C6-02, C6-03, C6-08, C6-09 |
| Related | ADR 0002 (SQLite), ADR 0004 (claims), ADR 0006 (slot charge), ADR 0007 (uniqueness, deferral, late deletion check), ADR 0008 (outbox deletion), ADR 0009 (refresh pipeline), ADR 0013 (provider rate budgets and per-user submit limits); ddd-rules D-10, D-11, D-12, SM-1…SM-4, R-2, R-10, A-3, A-9; evidence torbox.md T04, §5 "Interrupted submit still created the item", realdebrid.md RD-N01 |

Revised 2026-10-11 for revision 6.

## Context
Creating an item at the provider is a remote call that creates state elsewhere. A-9 requires it to run outside the local transaction.

Forces:
- **Orphans.** An interrupted TorBox create still created the item (torbox.md §5). If debroxy records nothing before the call, a crash leaks a provider item that nobody tracks.
- **Duplicate creates.** Real-Debrid creates a new item on every add (RD-N01). Two concurrent submissions of the same content must not both call the provider.
- **Ambiguity.** A timeout or network error does not tell whether the item exists. Creates must never be retried blindly (NFR-09).
- **D-11.** Revision 3 kept a separate `PendingSubmission` aggregate. Its completion changed two aggregates in one transaction.
- **Provider dedup.** TorBox returns the existing item ID for known content (T04). That ID may belong to an item debroxy already tracks, or to one that is being deleted.
- **Deletion race.** S's create can return the ID P of item X's content just before X's provider delete runs. X is then confirmed `deleted`, and S would complete onto deleted content.
- **Client retries.** A client that lost the response retries with the same `JobID`.
- **Slow outcomes.** The client waits at most the request deadline (10 s); the provider call can take longer, and its outcome must still be recorded.
- **Budget waits.** The TorBox create bucket (60 per hour, §9.4) can refuse a create for up to an hour, longer than the 20 min submission timeout. A create that was never sent must neither expire nor be lost by a restart.

## Decision
debroxy records a submission as the `submitting` state of a new `RemoteItem`, created **before** the provider call (DEC-27). Every step changes exactly one `RemoteItem` per transaction (D-11). Facts about other items (look-ups, quota count) are read inside the same write transaction through a read-only reads port passed to `updateFn` (R-10, ADR 0002).

**Start (§5.5 steps 1–3)**
1. **Idempotency (DEC-38).** A plain read before any write transaction: if the `JobID` exists as a claim or in `job_history`, return its current representation: 201/202 for the same owner and the same fingerprint, `conflict` otherwise. Nothing else runs. A concurrent duplicate that passes this read is caught inside the transaction by `ErrDuplicateJobID` (ADR 0004) or the job key, and is answered the same way.
2. **Join (one transaction).** If a claimable item exists for (provider, fingerprint), **including a `submitting` one**, the use case calls `Claim` (ADR 0004; quota per ADR 0006). A `submitting` item answers **202 `submission-pending`**, others 201. No provider call and no submit-rate check (DEC-32, ADR 0013).
3. **Start (one transaction).** Otherwise check the submit rate (DEC-24, ADR 0013) and the slot quota (ADR 0006), then `StartSubmission(id, provider, source, fingerprint, providerHash, firstJob, now)`. The item stores the Source, `Submission{StartedAt, AttemptStartedAt?, Attempts, NotBefore?, FollowUpsDue, DeferredBehind}` (§5.1, C5-02, C6-02), and the first job as `origin = created` and charge holder. `StartSubmission` also opens the first attempt (rule 5).
4. A unique-index violation in a start or outcome transaction is never shown to the user; the transaction retries from its look-up.

**Attempt (§5.5 step 4)**
5. Every provider create call is one **attempt**. `AttemptStartedAt` is the start of the current attempt and is empty while no attempt is in progress (§5.1, C6-02). Before the call, a short transaction on the item starts an attempt **only if none is in progress** (`AttemptStartedAt` empty and `NotBefore`, if set, passed): it sets `AttemptStartedAt = now` (debroxy clock, D-10) and increments `Attempts`. If an attempt is already in progress, the transaction changes nothing and this runner does not call the provider, so two runners (the request, a resume after restart, a scheduler tick) never send the same create twice. For the first attempt this happens inside `StartSubmission`.
   - **Budget-blocked, never sent (C5-08, C6-02, DEC-43).** If the provider rate budget refuses the create before it is sent, the gateway returns `not-sent{retryAfter}` (§6.4, ADR 0013). Nothing happened at the provider. A second short transaction clears `AttemptStartedAt`, undoes the increment of `Attempts` and sets `NotBefore = now + retryAfter`. The item stays `submitting` with no attempt in progress, so `ExpireSubmissions` does not count it (rule 14). This is neither a definite nor an ambiguous failure.
   - **Re-run.** `ResumeDeferredSubmissions` (every minute) calls `ExecuteSubmission` again for `submitting` items without an attempt in progress whose `NotBefore` has passed (§5.4). `NotBefore` is persisted in `submission_json`, so the re-run also happens after a restart.
   - **HTTP 429 is not `not-sent`** (C6-03, §6.4). A 429 answer to a create that was sent is an **ambiguous failure** (rule 12); the attempt stays in progress.
   - **End of an attempt.** An attempt ends when its outcome is known: a definite failure, a completion, a deferral (rule 8) or a fired late deletion check (rule 9). Revision 7 states this for every such outcome (C7-02). An ambiguous failure leaves the attempt in progress, because the create may still exist.
6. **`ExecuteSubmission` runs detached from the request** (C5-11). `SubmitJob` starts it with a context that the request's cancellation does not cancel (bounded by the provider call timeout and by shutdown), and waits for it at most the request deadline (`job.submitRequestDeadline`, 10 s). If the outcome is there in time, the answer is 201 with the completed item (or the synchronous error of rule 11); otherwise 202 `submission-pending`. A create that finishes after the deadline still runs the outcome transaction of rules 7–10 itself. `ProviderGateway.CreateRemoteItem(source)` is called outside any transaction (A-9). If the process stops during the call, the outcome is found by rule 13 or ends in rule 14. If it stops while the item waits for `NotBefore`, rule 5's re-run picks the item up after the restart.

**Outcome (one transaction on the same item S).** On success with provider item ID P, S is `submitting`. The checks run in this order:

7. Another **claimable** item holds P (the provider deduplicated into a tracked item): `FailSubmission(merged-into{otherItem})`. The use case then claims the other item for each of S's claims, **one transaction per claim**, and releases each claim on S (DEC-36). Joins follow the normal join rules of ADR 0006. A claim that the other item refuses (its owner already holds a job there, or the claim would be charged and the owner has no free quota) is released into the owner's history with `merged-into-refused{reason}` (C6-09).
8. A **`deleting`** item holds P: `DeferSubmission(blocker)`. `ResumeDeferredSubmissions` (every minute) starts a new attempt once the blocker is `deleted` (ADR 0007). If the blocker's deletion is parked instead, rule 14 fails S.
9. **Late deletion check (DEC-39, C5-01).** An item that held P has `deletedAt ≥ S.AttemptStartedAt`: its provider delete may have removed P after this create returned it. Do not complete; end this attempt and start a new one at once (rule 5). The check only reads the other item (D-11). This check **replaces** the revision 4 fallback "re-submit the taken-over item"; ADR 0007 rule 13 explains why that fallback cannot be reached except by provider ID reuse, now that `Revive` is blocked while a `deleting` item holds the provider ID (C6-01, DEC-44).
10. Otherwise `CompleteSubmission(P, observation)`. It records P, clears `submission` and `source`, applies the observation as `Observe` would, and records `SubmissionCompleted` and `LifecycleAdvanced`. A `failed`/`gone` item with P stays as it is.

**Failures**
11. **Definite failure** (classified provider error, DEC-33):
    - only the creator's job claims S: `DiscardSubmission(provider-rejected{detail})`, `submitting` → `deleted` (`deletedAt` set; no provider item exists, so no outbox message). The creator's job goes to history as failed with that reason (§5.2, C6-08). Within the request deadline the submitter gets the error synchronously instead of a job representation; after the deadline (the submitter already got 202) they find the failed job in their history;
    - otherwise `FailSubmission(provider-rejected{detail})`; joined users see a failed job.
12. **Ambiguous failure** (timeout, network error, and HTTP 429 on a sent create, C6-03): the item stays `submitting` and the attempt stays in progress. Follow-up refreshes run at +30 s, +2 min, +5 min, +10 min, +15 min, stored in `FollowUpsDue` so they survive a restart. The submitter has 202.
13. **Completion by refresh** (§5.6 step 3): a listed observation whose provider hash matches a `submitting` item runs the same outcome logic, rules 7–10, including the late deletion check. Uniqueness of provider hash among claimable items makes the pairing 1:1 (ADR 0007). The remaining follow-ups are cancelled. A `deleting` item without a provider ID follows rule 15.
14. **Expiry** (`ExpireSubmissions`, every minute, §5.4, C5-04):
    - a `submitting` item **without** `DeferredBehind` **with an attempt in progress** whose `AttemptStartedAt` is older than the submission timeout (20 min, longer than the follow-up schedule) → `FailSubmission(submission-lost)`. Expiry counts only from a started attempt: an item waiting for `NotBefore` has none and is not expired (C6-02, DEC-43);
    - a **deferred** item is exempt from the timeout. Its limit is its blocker's deletion: when the blocker's `DeleteRemoteItem` message is parked (ADR 0008), the item fails with `FailSubmission(blocked-by-deletion)`. Each such item is failed in its own transaction; the parked state is read through the reads port (R-10, D-11);
    - a `deleting` item without a provider ID past the timeout → `FailSubmission` (→ `deleted`). Such an item with no attempt in progress goes to `deleted` at the next run (C7-03).

**Released while submitting (S is `deleting` without a provider ID).** `RequestDeletion` on a `submitting` item keeps `submission` and `source` (§5.1, C5-02), because a create may still be in flight. Its outcome:

15. Success with P:
    - if the late deletion check of rule 9 fires, `FailSubmission` (→ `deleted`, `deletedAt` set): P's content is already gone, so there is nothing to delete and no new attempt starts;
    - otherwise `CompleteSubmission(P)` records P, clears `submission` and `source`, stays `deleting` and writes the outbox `DeleteRemoteItem` (ADR 0008). If a claimable item holds P, the deletion's transaction 1 supersedes it; if another `deleting` item holds P, both deletions run and verified deletion turns the second into a lookup.
    - Definite failure → `FailSubmission` (→ `deleted`). Ambiguous failure → stays `deleting`; rule 13 or rule 14 resolves it.
    - An item released while deferred, or while waiting for `NotBefore`, has no create in flight. `ResumeDeferredSubmissions` handles only `submitting` items, so it never re-runs the create; rule 14 moves the item to `deleted` (C7-03).

**Transitions** (SM-1…SM-4; full table in spec §5.2):

| Method | From | To |
|---|---|---|
| `StartSubmission` | (new) | `submitting` |
| `CompleteSubmission` | `submitting` | the observation's mapped state |
| `CompleteSubmission` | `deleting` (no provider item) | `deleting` (P recorded, outbox `DeleteRemoteItem`) |
| `DeferSubmission` | `submitting` | `submitting` (`DeferredBehind` set) |
| `FailSubmission` | `submitting` | `failed` |
| `DiscardSubmission` | `submitting` (only the creator's claim) | `deleted` (`deletedAt` set; creator's job to history as failed) |
| `FailSubmission` | `deleting` (no provider item) | `deleted` (`deletedAt` set) |
| `RequestDeletion` | `submitting` | `deleting` (no provider item yet; `submission` and `source` kept) |

Invariants: `submission` and `source` are present iff the state is `submitting`, or `deleting` without a provider ID (§5.1). `providerItemID` is absent while `submitting`. `deletedAt` is set iff the state is `deleted`. `AttemptStartedAt` is empty while no attempt is in progress; at most one attempt is in progress per item.

## Consequences
### Positive
- No provider item can exist without a `RemoteItem`. The revision 2 `DeleteProviderItem` message is gone.
- Same-content submissions serialize on one item; Real-Debrid gets one create, not two.
- Joiners on a slow submission get an ordinary claim and see `submitting` (202).
- Every transaction changes one aggregate (D-11).
- Creates are re-run only on definite knowledge: deferral, the late deletion check, or a call that was never sent. Never blindly.
- A never-sent create neither expires nor gets lost on restart: `NotBefore` is persisted and `ExpireSubmissions` counts only started attempts.
- Every claimant learns the outcome: a late definite failure and a refused `merged-into` claim both end as a history entry with a reason.
- A slow create's outcome is recorded even after the client got 202.

### Negative
- A `RemoteItem` can exist for content that never reaches the provider (`failed`, `submission-lost`, `blocked-by-deletion`).
- The Source (up to the upload limit) is stored while `submitting`, and while `deleting` without a provider ID.
- `merged-into` moves claims in several transactions. For a moment a user may see the failed job before the moved claim appears (DEC-36).
- The late deletion check costs one extra upload in a rare interleaving.
- A deferred submission can wait as long as its blocker's deletion retries (back-off up to 1 h, parked after 20 attempts: up to about 8 h, ADR 0008).
- A budget-blocked submission can stay `submitting` for up to an hour (TorBox create bucket) without expiring; its creator holds a slot charge meanwhile (ADR 0006).
- A provider 429 on a create costs up to 20 min of follow-ups before `submission-lost`, although the provider created nothing (C6-03 chooses safety over speed).

### Risks and mitigations
- *Ambiguous create that never appears*: follow-ups, then `submission-lost` after 20 min.
- *A create that succeeds after its item expired or was deleted*: the provider item is no longer matched (rule 13 looks only at `submitting` and `deleting` items) and appears as an Unmanaged Item (DEC-10). Rare; accepted.
- *Provider re-uses an item ID for different content*: not observed with either provider. Residual of the deletion race; accepted.
- *Open points:* none. Revision 7 settles the former open points: `AttemptStartedAt` is cleared whenever an attempt ends without completing (C7-02); a `deleting` item without a provider ID and without an attempt in progress goes to `deleted` at the next `ExpireSubmissions` run (C7-03); `DiscardSubmission` records `SubmissionFailed`, `JobReleased{finalState = submitting}` (the history entry) and `LifecycleAdvanced` (C7-04).

## Alternatives considered
**Separate `PendingSubmission` aggregate** (revision 3). Completion had to change the pending record and the new `RemoteItem` in one transaction. That breaks D-11. It also needed waiting jobs with their own quota re-check and its own retention.

**Call the provider first, then persist** (revision 1). A crash between the call and the commit leaks the provider item as an Unmanaged Item that is never deleted. TorBox shows that even an interrupted call creates the item.

**Retry creates blindly** on ambiguous failures. On Real-Debrid each retry creates another item (RD-N01) and spends API budget. On TorBox it hides the real outcome and spends the 60-per-hour create budget (§9.4). Rejected by NFR-09.

**Tie `ExecuteSubmission` to the request context.** A client that disconnects or hits the deadline would cancel the call; the provider may still create the item (torbox.md §5) while debroxy records nothing until a refresh. Rejected for rule 6.

## Compliance
- Domain unit tests: every row of the transition table above, and every forbidden pair (SM-4); `CompleteSubmission` clears `submission` and `source` and the charge fields when the result is not active; `CompleteSubmission` and `FailSubmission` on a `deleting` item; `DiscardSubmission` only with the creator's claim alone; `deletedAt` set on every path to `deleted`; starting an attempt fails while one is in progress or before `NotBefore`; events `SubmissionCompleted`, `SubmissionFailed{reason, claimants}`, `SubmissionDeferred`, `LifecycleAdvanced` (D-12); invariants after every method.
- App tests with a fake `ProviderGateway`: definite failure with only the creator within the deadline → synchronous error, item `deleted`; after the deadline → 202 first, then a failed job with `provider-rejected` in the creator's history; with joiners → `failed` jobs; ambiguous → 202 and five follow-ups scheduled; HTTP 429 on a create → treated as ambiguous, no re-send; follow-ups cancelled on completion; budget-blocked create (`not-sent{retryAfter}`) → `Attempts` unchanged, `AttemptStartedAt` empty, `NotBefore` set, item still `submitting`, re-run by `ResumeDeferredSubmissions` once `NotBefore` passes; `merged-into` with a refused claim → that claim in history with `merged-into-refused{reason}`, the others moved.
- Single-attempt test: two concurrent `ExecuteSubmission` runs for one item → exactly one provider call.
- Deadline test: the fake gateway answers after the deadline; the client gets 202, the request context is cancelled, and the item still completes.
- `ExpireSubmissions` tests with a fake clock: an undeferred item expires 20 min after its current attempt started; a budget-blocked item waiting 60 min for `NotBefore` does not expire; a deferred item does not expire while its blocker is `deleting`; it fails with `blocked-by-deletion` once the blocker's message is parked; a `deleting` item without provider ID becomes `deleted` after the timeout.
- Idempotency test: the same `SubmitJob` twice → one item, one provider call, same response; same `JobID` with a different fingerprint → `conflict`.
- Race test: N concurrent `SubmitJob` with one fingerprint and different users → exactly one provider call, N claims, one charge holder.
- Interleaving tests (fake gateway with controllable ordering): create returns P while X is `deleting` → deferred, resumed after X is `deleted`; late deletion order (create returns P, X confirmed `deleted`, then the outcome) → a second create runs; the same order for a `deleting` S → `deleted`, no second create; create returns P held by a claimable item → `merged-into` and all claims moved.
- Crash tests: kill between `StartSubmission` and the provider response; the next refresh completes the item by provider hash. Kill while an item waits for `NotBefore`; after the restart it is re-run once `NotBefore` passes.
- Review checklist: no transaction spans `CreateRemoteItem`; `ExecuteSubmission` never uses the request context for the provider call; no code path re-runs a create except rule 5 (never sent, after `NotBefore`), rule 8 (resume after deferral) and rule 9 (late deletion check); every re-run goes through the attempt-start transaction of rule 5.
