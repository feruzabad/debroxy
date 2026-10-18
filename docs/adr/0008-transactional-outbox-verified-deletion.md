# 0008. Transactional outbox with verified provider deletion

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 7 (unchanged sections from revisions 5–6 apply), §5.1 (`deletedAt`, `submission`), §5.2 (incl. `Revive`), §5.4 (`ExpireSubmissions`, `ResumeDeferredSubmissions`), §5.5 step 5, §5.6 steps 3–5, §5.7, §5.8, §6.2 `DeleteRemoteItem`, §6.4, NFR-03, NFR-09 ; decisions DEC-20, DEC-25, DEC-37, DEC-39, DEC-44 (with DEC-31) ; changes C5-01, C5-02, C5-04, C5-06, C5-11, C6-01 |
| Related | ADR 0002, ADR 0004, ADR 0005, ADR 0007, ADR 0009; ddd-rules R-2, R-5, R-6, R-10, A-8, A-9, D-10, D-11, OUT-3, OUT-4, OUT-6; evidence TorBox T02, T04, T06, TB-N02, Real-Debrid R05, R06, R14, RD-N03, RD-N05 |

Revised 2026-10-11 for revision 6.

## Context
When the last claim on a Remote Item is released, the provider item must be deleted. Otherwise it keeps using a provider slot and stays visible as account content. The deletion is a call to an external system that may fail, time out, or succeed without debroxy seeing the answer.

Observed provider behaviour:
- TorBox delete is **not idempotent**. A second delete of the same item returns HTTP 500 with `error`/`detail` swapped (`"detail":"DATABASE_ERROR"`), not "not found" (torbox.md T02, T06; fixture `09-delete-torrent-again.http`). A lookup of a deleted item returns 404 `ITEM_NOT_FOUND` with `success:true` (`09-mylist-id-after-delete.http`).
- Real-Debrid delete returns 204, a second delete 404 `unknown_ressource` (7), and `info` after delete the same 404 (realdebrid.md R06; `09-delete-again.http`, `09-info-after-delete.http`).
- Each Real-Debrid unrestrict creates a `/downloads` history entry that accumulates unless deleted (R05, RD-N05; `09-downloads-history.http`, `09-delete-download-history.http`). A history entry carries the CDN URL (`download`) and a hoster link (`link`), and its `generated` timestamp is local time labelled `Z` (RD-N03).
- Deleting an item, or its history entry, does not revoke CDN links at either provider (TB-N02, R14). Deletion is housekeeping, not a security control.

Constraints:
- ddd-rules R-6 requires outbox messages in the same transaction as the aggregate change. A-8 says post-commit side effects go through the outbox. A-9 says remote calls happen outside the local transaction.
- SQLite has one writer (ADR 0002, ASM-S01). Holding a write transaction across a provider call blocks every other writer for the call's duration.
- The same provider item ID can be held by a `deleting` item and, later, by a claimable item. TorBox deduplicates known content to the same ID (torbox.md T04), and uniqueness applies only among claimable items (DEC-31, ADR 0007).
- A race was found: a submission gets provider item ID P while another item X still holds P. X's delete then removes the content, and the submission completes onto deleted content. Revision 5 closed it on the completion side: completion defers while X is `deleting`, and the **late deletion check** creates again when X's deletion was confirmed after the current create attempt started (§5.5 step 5, DEC-39, C5-01). The revision 4 fallback "re-submit the taken-over item" (`ResubmitRemoteItem` in an earlier draft of this ADR) is dropped. Revision 6 closes the same race on the revive side: `Revive` is blocked while a `deleting` item holds the provider item ID (C6-01, DEC-44).

## Decision
debroxy executes provider deletions through a transactional outbox, verifies the outcome by lookup, and never holds a transaction across the provider call.

**Writing the message**
1. `RequestDeletion` on an item with a provider item ID, and `CompleteSubmission` on a `deleting` item, write `DeleteRemoteItem{RemoteItemID}` in the same transaction as the item change (R-6). The refresh pipeline does the same when a `deleting` item without a provider ID learns its provider ID by provider-hash match (§5.6 step 3, ADR 0009).
2. There is no message that deletes by provider reference alone. Every provider item has a `RemoteItem` from the start of its create (C4-01, ADR 0005), so no such message is needed. `DeleteRemoteItem` is the only outbox message type.

**Executing the message** (`ExecuteRemoteItemDeletion`, fed by the outbox processor, a driving adapter)
3. **Transaction 1** (`BEGIN IMMEDIATE`, R-5): load the item; the look-up for other holders of its provider item ID is a read inside the same transaction (R-10).
   - Not `deleting` → mark the message done.
   - A **claimable** item now holds the same provider item ID (a new submission took it over, §5.7; for example this item was released while submitting and its late create returned an ID that a claimable item already holds, ADR 0005) → `SupersedeDeletion` (`deleting → deleted`, sets `deletedAt`, no provider call) → message done.
   - Otherwise commit and continue. Transaction 1 changes nothing in this case.
4. **Provider call**, outside any transaction (A-9): `ProviderGateway.DeleteRemoteItem(provider, kind, providerItemID)`. Its contract (§6.2, C5-11): it returns `deleted` or an error. `not-found` is never returned to the caller; it is folded into `deleted` by rule 5.
5. **Verified deletion**, inside the provider adapter. Only a success answer (TorBox 200, Real-Debrid 204) counts as deleted without lookup. On **any** error, including "not found" and TorBox `DATABASE_ERROR`, the adapter calls `DescribeRemoteItem`:
   - lookup says not-found → `deleted`;
   - lookup finds the item → error; the message is retried;
   - lookup fails → error; the message is retried.
6. **Transaction 2** (`BEGIN IMMEDIATE`): re-load the item.
   - Not `deleting` any more → message done. Only this command moves a `deleting` item that has a provider item ID (rule 3 or this rule), so this is a defensive check against a duplicate execution.
   - Otherwise `ConfirmDeleted`: `deleting → deleted`, `deletedAt` = debroxy clock (D-10). `deletedAt` is recorded after the provider call returns, so it is never earlier than the moment the provider removed the item. The late deletion check (rule 7) depends on this.
   - **Anomaly check:** if a claimable item now holds the same provider item ID, debroxy logs it at warn level as an anomaly (`provider-item-id-reuse`, with both `RemoteItemID`s and the provider item ID; no secret is involved). That item is not changed here (D-11). The next complete listing no longer contains the provider ID, and absence handling marks the item `gone` (§5.6 step 7, ADR 0009).
7. **Why the anomaly is not a normal path**. While X is `deleting` and holds P, a claimable item can come to hold P only by completing a submission onto P or by `Revive`:
   - **Completion** (§5.5 step 5, and the interrupted-submission step §5.6 step 3) defers while a `deleting` item holds P (`DeferSubmission`, DEC-31). It completes only after X is `deleted`, and then the late deletion check compares X's `deletedAt` with the submission's `AttemptStartedAt`. If X's provider DELETE could have run after the create attempt started, `deletedAt ≥ AttemptStartedAt` holds and the create runs again instead of completing (DEC-39, C5-01). So completion onto P never meets transaction 2 of X.
   - **Revive** (§5.6 step 4) is blocked while a `deleting` item holds P (§5.2, C6-01, DEC-44). The observation of X's not-yet-deleted content is dropped (step 5 ignores it), so a `failed`/`gone` item Y holding P cannot be revived while X's DELETE is in flight. Y can revive only after X's transaction 2 has committed, so the anomaly check there cannot see Y.

   With both paths closed, the anomaly in rule 6 is reachable only if a provider re-uses an item ID for different content, which has not been observed.

   An earlier draft's fallback (`ResubmitRemoteItem`) is therefore not needed. It could not be implemented anyway: completion clears `source`, and §5.2 has no transition back to `submitting`.

**Retries and parking**
8. Failed executions retry with exponential back-off from 1 s up to 1 h, with jitter. After 20 failed attempts the message is **parked**. Parked messages are listed by the admin query `ParkedOutboxMessages` (`GET /api/v1/admin/outbox`), raise an operator alert, and are not retried automatically.
9. **Parking fails deferred submissions** (DEC-37, C5-04). Deferred submissions are exempt from the submission timeout; their limit is the blocker's deletion outcome. When the message of item X is parked, `ExpireSubmissions` (every minute) fails each `submitting` item whose `DeferredBehind` is X with `FailSubmission(blocked-by-deletion)`, one transaction per item. It reads the blocker's message state inside that transaction (R-10) and changes only the deferred item (D-11).
10. Delivery is at least once. Every step above is safe to repeat: transaction 1 ends early once the item is `deleted`, and verified deletion turns a repeated delete into a lookup.
11. Processed messages are pruned after 30 days (`retention.processedOutbox`). `deleted` items, with their `deletedAt`, stay 30 days (§5.8). That is far longer than any create attempt the late deletion check has to compare against (an attempt is at most the 20-min submission timeout old, and a deferred submission starts a new attempt when it resumes).

**Real-Debrid download history** (DEC-25, owner decision)
12. debroxy does **not** track `/downloads` entry IDs per item. A **daily sweep** inside `provider` (a timer started by `provider/service`, not an outbox message and not a `job` concern) lists `/downloads` and deletes every entry older than 24 h (`DELETE /downloads/delete/{id}`).
13. The 24 h threshold is approximate: `generated` is local time labelled `Z` (RD-N03), so the cutoff can shift by the provider's UTC offset. This is acceptable: deleting a history entry does not revoke its CDN link (R14), and debroxy caches a CDN link for at most 30 min (ADR 0010).
14. The sweep deletes **every** old entry, not only debroxy's. It relies on exclusive use of the account (ASM-G01). Its calls take tokens from the account's `general` budget (ADR 0013).
15. History cleanup is best effort. A failure is logged and retried at the next sweep. It never touches the outbox or any Remote Item.
16. **Secrets (OUT-6, NFR-03).** History listings contain CDN URLs and hoster links. The adapter decodes only `id` and `generated`, never logs response bodies, and logs failures with the entry ID and a sanitized error (no URL, no key). Entry IDs are not secrets.

## Consequences
### Positive
- Releasing a job never waits for the provider. A crash between commit and call loses nothing; the message survives.
- No write lock is held during network I/O, so slow provider calls do not stall submissions or refreshes.
- TorBox's non-idempotent delete and Real-Debrid's 404 are handled by one rule: on error, look it up.
- The deletion race is closed on the completion side (deferral plus the late deletion check) and on the revive side (no revive while a `deleting` item holds the provider item ID). No message type exists only for a race.
- Real-Debrid history cleanup needs no per-item state and survives restarts.

### Negative
- Two transactions and up to two provider calls per deletion.
- A provider item may live up to an hour longer while back-off runs, holding a provider slot.
- **A deferred submission waits through its blocker's whole back-off.** The submitter sees `submitting` (202) for as long as the blocker's deletion keeps failing: up to about 8 h of retries (1 s doubling to 1 h, 20 attempts) before parking fails the submission with `blocked-by-deletion`.
- Real-Debrid history entries live up to about a day (plus the timestamp offset) before the sweep removes them, and the sweep would also remove entries created outside debroxy.

### Risks and mitigations
- **Wrong provider item ID deleted**: verification proves absence, not identity. Mitigation: adapter contract tests assert the ID parameter per kind (`torrent_id`, `webdl_id`; TB-N14) against fixtures.
- **Stale listing after the deletion.** A listing taken before X's provider DELETE but applied after X's transaction 2 still shows P, so Y can be revived onto removed content. Closed by revision 7 (C7-01): `Revive` requires the observation to be newer than the `deletedAt` of every item that held P, so such a listing cannot revive Y.
- **Provider re-uses an item ID for different content** (not observed). This is the only remaining path to the anomaly in rule 6 (rule 7). Mitigation: anomaly log and absence handling (`gone`); the affected users resubmit.
- **Wall-clock steps backwards** between `AttemptStartedAt` and `deletedAt` and defeat the comparison. Mitigation: both come from the same `clock.Clock` port; operators run NTP with slewing.
- **Parked messages leave provider items behind.** Mitigation: admin listing, operator alert on parking; after the item is `deleted`, the leftover provider item appears in the Unmanaged Items view.
- **The sweep deletes foreign history entries** if the account is shared outside debroxy. Mitigation: ASM-G01 is an operator requirement and is documented.

## Alternatives considered
**Delete inline after commit.** The use case commits, then calls the provider. A crash or timeout after commit leaves an orphan with nothing to retry it. A-8 rejects this pattern.

**Delete before commit.** The provider call runs first, then the release commits. If the commit fails, the item is gone at the provider but still claimed in debroxy. A-9 puts a remote call before the transaction only when it creates state that can be stored; a deletion cannot be undone when the commit fails.

**Reconcile orphans through the refresh pipeline.** No outbox; the next refresh deletes provider items that no claimable item holds. Released items would be deleted only when someone refreshes, a crash loses the intent, and the refresh would have to tell "released, delete it" from "unmanaged, leave it alone" without a stored record. Leftovers are Unmanaged Items that debroxy never touches (DEC-10). A-8 and R-6 require the recorded side effect instead.

**Trust "not found = success" without lookup.** Revision 1 did this. TorBox answers a repeated delete with 500 `DATABASE_ERROR`, not not-found, so the rule cannot even be applied there. A lookup gives one answer for both providers.

**Hold a lock across the call.** Keep the `BEGIN IMMEDIATE` transaction open from the check through the provider call (an earlier proposal). It closes the race but blocks every writer for up to the call timeout. A lease row per provider item ID was also rejected: it adds TTL machinery and still fails when the call outlives the lease.

**Re-submit the taken-over item**. Rejected in revision 5 (DEC-39): it needs the source kept after completion and a transition back to `submitting`, and deferral, the late deletion check and the revive block leave nothing for it to do (rule 7).

**Track Real-Debrid history entry IDs in memory** (earlier draft). Entries are lost on restart, so a sweep is needed anyway; the owner chose the sweep alone.

## Compliance
- App tests (T-2) for `ExecuteRemoteItemDeletion` with a fake gateway: not `deleting` → done; claimable holder → `SupersedeDeletion` with `deletedAt` and no gateway call; `deleted` → `ConfirmDeleted` with `deletedAt` taken from the fake clock after the call; gateway error → message retried; claimable holder appears between the calls → `ConfirmDeleted`, one anomaly log entry, the other item unchanged.
- App test for `ExpireSubmissions`: a deferred item older than the submission timeout is not failed while its blocker's message is pending; it fails with `blocked-by-deletion` once the message is parked.
- Adapter tests for `DeleteRemoteItem`: success → `deleted` without lookup; error then lookup not-found → `deleted`; lookup found or failing → error; `not-found` never escapes.
- A test asserts no transaction is open while the gateway is called (the fake gateway checks the transaction tracker of the memory repository).
- Repository contract tests (T-3): the outbox row and the item change commit or roll back together.
- Provider adapter tests (T-5) on `docs/discovery/`: TorBox `09-delete-torrent-*.http`, `09-delete-torrent-again.http`, `09-mylist-id-after-delete.http`; Real-Debrid `09-delete-*.http`, `09-delete-again.http`, `09-info-after-delete.http`, `09-downloads-history.http`, `09-delete-download-history.http`.
- Sweep test with a fake clock: entries older than 24 h are deleted, younger ones kept; a failing delete is logged and does not stop the sweep; a canary in `download`/`link` never appears in logs.
- Back-off test with a fake clock: delays grow to 1 h; attempt 20 parks.
- Review checklist: no network call inside an `updateFn`; every new provider side effect uses the outbox (A-8); no second outbox message type without an ADR.
- Pipeline test (shared with ADR 0009): a `failed` item and a `deleting` item hold the same provider item ID, and the listing still shows it active → no `Revive`, the observation is ignored, and the deletion's transaction 2 logs no anomaly.
