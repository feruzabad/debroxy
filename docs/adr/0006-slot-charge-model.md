# 0006. Slot charge model: charge holder, then all claimants

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 7 (unchanged sections from revisions 5–6 apply), §2 (Active Run, Charge Holder, Slot Charge, Slot Quota), §5.1, §5.2 (active states), §5.3, §5.4 `QuotaUsage`, §5.5 step 3 and slot-quota consistency, §5.8 (`charge_holder_job_id`, `charge_holder_released`, `slot_quotas`), §10.2, §11 (`job.defaultSlotQuota`), §14 ASM-S01 ; decisions DEC-08, DEC-24, DEC-28 ; changes C5-11 |
| Related | ADR 0002 (SQLite single writer, ASM-S01), ADR 0004 (claims), ADR 0005 (submission), ADR 0007 (Revive), ADR 0013 (submit limits); ddd-rules D-8, D-10, D-11, D-12, R-5, R-10; spec NFR-04; user decisions (C-01, C3-01, C4-06) |

Revised 2026-10-11 for revision 5.

## Context
The shared provider account has a limited number of active downloads. Each user gets a **Slot Quota**: a maximum number of active Remote Items they are charged for.

Forces:
- **Joining adds no provider work.** The user decided that joining an existing item is free.
- **Fairness after the creator leaves.** If the creator releases while the item is still downloading, someone must keep paying, or users can start downloads, release them, and leave free work running for others. The user decided that from then on every claimant pays (C3-01).
- **Being charged by someone else's action.** That decision means a user can go over quota because another user released. Over quota must therefore never stop a running download.
- **Consistency.** The quota count reads other `RemoteItem` aggregates while one is changed. D-11 forbids changing two aggregates, not reading them. R-10 gives such reads a home: the repository runs them in the write transaction and hands plain values to `updateFn`. Correctness relies on SQLite serializing writers (ASM-S01, ADR 0002).
- **Ready items cost nothing.** A `ready` item uses no provider download capacity.

## Decision
debroxy charges the job that started the current Active Run. Once that job is released during the run, every remaining and every later claimant is charged (DEC-28). The quota counts active items with a charge (DEC-08).

**State on `RemoteItem`**
1. `chargeHolder JobID?`: the job whose submission started the current Active Run.
2. `chargeHolderReleased bool`: the charge holder was released during the current Active Run.
3. `IsChargedTo(userID)` = item is active (`submitting`, `queued`, `resolving_metadata`, `awaiting_file_selection`, `downloading`, `processing`) **and** (the owner of `chargeHolder` is the user, **or** `chargeHolderReleased` and the user holds a claim).

**Invariants** (checked after every method)
- CH-1: `chargeHolder` set ⇒ it is one of the item's claims and `chargeHolderReleased = false`.
- CH-2: item not active ⇒ `chargeHolder` empty and `chargeHolderReleased = false`.
- CH-3: item active ⇒ exactly one of `chargeHolder` set or `chargeHolderReleased = true`.

**Rules**
4. `StartSubmission`: the first job (`origin = created`) becomes `chargeHolder`. The use case checks the creator's quota before, in the same transaction (§5.5 step 3). This is the only quota check on the create path.
5. `Claim` (join):
   - item active and `chargeHolderReleased` → the claim is charged. The use case first checks the joiner's quota (rule 10; `slot-quota-exceeded`). There is **no submit-rate check**: a join makes no provider call (DEC-24, ADR 0013);
   - otherwise the claim is free: no quota check and no submit-rate check.
6. `Release(jobID)`:
   - **first**, if no claims remain → `RequestDeletion`; no spread;
   - otherwise, if the released job is `chargeHolder` and the item is active → clear `chargeHolder`, set `chargeHolderReleased = true`, record `ChargeSpreadToClaimants{userIDs}`. The spread is **unconditional**: it applies even when a claimant goes over quota. The event is internal to `job`: it carries other users' IDs, so it is never exposed through the API and never written to any user-visible log or message (NFR-04);
   - otherwise the charge fields do not change.
7. **End of the Active Run.** Any transition from an active to a non-active state (`Observe`, `CompleteSubmission`, `FailSubmission`, `MarkGone`, `RequestDeletion`) clears both fields. There is no sync edge from `ready` back to an active state, so a run never restarts from `ready`.
8. **`Revive`** (ADR 0007) adds no job:
   - result active → `chargeHolder` empty, `chargeHolderReleased = true`: every claimant is charged;
   - result not active → both fields cleared.
9. **Over quota never stops a download.** It only blocks new charged claims: a new submission (rule 4) and a charged join (rule 5).
10. **Quota check.** `slotsUsed` is the number of active items where `IsChargedTo(user)` holds, `submitting` included. `MaxActiveJobs` comes from the user's `SlotQuota` row, or from `job.defaultSlotQuota` when the user has none (§5.4, C5-11). A new charged claim (rule 4 or the charged branch of rule 5) is allowed only while **`slotsUsed < MaxActiveJobs`**; at equality it fails with `slot-quota-exceeded`. The split of responsibilities:
    - `IsChargedTo(userID)` is the **domain policy** (D-8): a pure predicate on one item, with no I/O;
    - the **count** and the quota look-up are **repository reads** across other aggregates. The repository runs them inside the same `BEGIN IMMEDIATE` write transaction that changes the one item (R-5, ADR 0002) and passes plain values to `updateFn` through a read-only reads port declared in `ports/out` (R-10, e.g. `ActiveChargedItemCount(userID)` and `SlotQuotaOf(userID)`). The count's SQL predicate must match `IsChargedTo`; a contract test compares both (Compliance). Race-freedom rests on ASM-S01.
11. **`SlotQuota`** is its own aggregate `{UserID, MaxActiveJobs}`. A user without a row uses `job.defaultSlotQuota` (default 3, §11). Admins change it with `SetSlotQuota`. Lowering it never releases or stops anything.
12. **Visibility.** `QuotaUsage.slotsUsed` uses the same count as rule 10. Each job shows `slotCharged`; it can turn `true` when the charge holder is released (§10.2). The API never reveals who else holds a claim (NFR-04).

Examples:

| Situation | Charged |
|---|---|
| A submits, B joins while downloading | A |
| A releases, B and C remain, still downloading | B and C (even if B is now over quota) |
| D joins after that | D, only if D has free quota |
| Item becomes `ready` | nobody |
| `gone` item revived as `downloading` | every claimant |
| `gone` item revived as `ready` | nobody (both fields cleared, rule 8) |

## Consequences
### Positive
- Joining is free in the common case, as the user decided.
- Nobody can leave free download work running by releasing their own job.
- All charge state lives on one aggregate. Join, release and spread are each one transaction (D-11).
- `ready` and cached items never use quota.

### Negative
- A user can be pushed over quota by another user's release. They then cannot submit new content until a charged item ends.
- The quota check depends on SQLite's single writer. Moving to a store with concurrent writers breaks it (ASM-S01).
- Rule 5 means a join can be refused after a release, while the same join was free a minute earlier.

### Risks and mitigations
- *Surprise charges*: `slotCharged` per job makes the change visible to each affected user; `ChargeSpreadToClaimants` stays internal (rule 6). Notifying users is out of scope (§1.4).
- *Permanently stalled downloads keep charging* (TorBox stalls at 98 %, torbox.md TB-N07): users can release the job. Whether a long stall should drop the charge is OQ-14.
- *Two concurrent charged claims in different items race on the quota*: prevented by `BEGIN IMMEDIATE` (ADR 0002); a contract test proves it.

## Alternatives considered
**Every claim always counts** (revision 1). Joining an active item uses a slot. It charges users for work the provider does once, and contradicts the user's decision that joining is free.

**Charge moves to the oldest remaining claimant** (revision 2, C-01). Both models move the charge only when the charge holder is released, and both can push a joiner over quota by someone else's release; that cost is accepted in this ADR too (Negative). The differences:
- **One joiner vs all claimants.** C-01 puts the whole charge on one user, picked only by join order; the others ride free. This ADR spreads it over every remaining claimant, so no single user carries an item that several still want.
- **Later joiners.** Under C-01 a user who joins after the transfer is free; under this ADR every later joiner is charged and needs free quota (rule 5).
- **Chains.** Under C-01, when the newly charged user releases, the charge moves again to the next oldest claimant; this ADR needs no such chain, because one flag (`chargeHolderReleased`) covers all claimants.

The user chose the spread (C3-01).

**Charge disappears when the creator releases.** Simple and never charges joiners. But a user can submit, release at once, and leave free downloads running for others. That defeats the quota.

**Creator keeps the charge until the run ends.** The released user pays for an item they no longer see or control, and cannot free the slot. Rejected as unfair to the releasing user.

## Compliance
- Domain unit tests: every row of the example table; invariants CH-1…CH-3 after every method, including `Revive` to active and to non-active, `CompleteSubmission` to `ready`, `MarkGone`, and release of the last claim (no `ChargeSpreadToClaimants`).
- Policy test for `IsChargedTo` over every Lifecycle state.
- Repository contract test (T-3): for a fixture set of items in every state and charge configuration, `ActiveChargedItemCount(user)` equals the number of items for which the domain's `IsChargedTo(user)` is true; a user without a `SlotQuota` row reads `job.defaultSlotQuota`.
- Boundary test: with `slotsUsed = MaxActiveJobs - 1` a new submission succeeds; with `slotsUsed = MaxActiveJobs` it fails with `slot-quota-exceeded`.
- Log and API test: `ChargeSpreadToClaimants` never appears in a response or a user-visible log entry.
- App tests: charged join without free quota → `slot-quota-exceeded`; charged join does not consume submit rate; free join with zero free quota succeeds; spread makes a user exceed quota and their next submission fails with `slot-quota-exceeded`.
- Repository contract and race test (T-3, T-7): a user with one free slot runs two concurrent `SubmitJob` calls for different content → exactly one succeeds. Same for two concurrent charged joins.
- `QuotaUsage` test: `submitting` items count; `ready` items don't.
