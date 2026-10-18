# 0009. Refresh-on-read with webhooks as refresh hints and one refresh pipeline

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner); the path in rule 2 amended by [ADR 0018](0018-api-paths-without-colon-methods.md); rule 18 amended by [ADR 0019](0019-tracked-failed-observations-are-not-leftovers.md); a sixth refresh cause, `watch`, added by [ADR 0021](0021-event-stream-and-watch-refreshes.md) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 7 (unchanged sections from revisions 5–6 apply), §5.2 (incl. `Revive`), §5.3 (`Observe`, `Revive`), §5.6, §5.8 (`provider_sync_state`), §6.2 `ListRemoteItems`, §8, §9.4, §10.2 (`stale`), §11 (`refreshBackoff`, `webhookRefreshInterval`), §12 NFR-03, NFR-11 ; decisions DEC-09, DEC-09b, DEC-10, DEC-11, DEC-12, DEC-34, DEC-41, DEC-44 ; changes C5-05, C5-06, C5-07, C5-10, C6-01, C6-06 |
| Related | ADR 0005, ADR 0006, ADR 0007, ADR 0008, ADR 0013; ddd-rules IN-2, IN-5, OUT-3, OUT-6, D-10, D-11, R-5; evidence TorBox T05, T12, T13, TB-N10, Real-Debrid R07, R13, RD-N01, RD-N03, RD-N04 |

Revised 2026-10-11 for revision 6.

## Context
debroxy mirrors provider state into Remote Items. The provider is the source of truth; debroxy only learns state by asking.

Observed provider behaviour:
- The TorBox webhook payload is `{type, timestamp, data{title, message}}`. It has **no item ID, hash or kind** and **no signature header**. It fired for an uncached completion but not for a cached add or a deletion (torbox.md T12, TB-N10; fixture `12-webhook-received.json`). It can only mean "something changed".
- Real-Debrid exposes no webhook settings in `/user`; that it has no webhooks at all is **assumed**, not observed (realdebrid.md R13).
- Real-Debrid list `status` is stale (`magnet_conversion` while `info` said `waiting_files_selection`), so state needs one `info` call per item (R07, RD-N04; `13-torrents-all.http` vs `03-dup-magnet-info.http`). A page past the end returns 204 (`13-torrents-page-beyond.http`).
- Real-Debrid `added` is local time labelled `Z` (RD-N03). Provider timestamps cannot order observations.
- Real-Debrid does not deduplicate (RD-N01), so a reappearing item can carry the same fingerprint or hash as a newer claimable item.
- TorBox listed a fresh item immediately (T05). Its general budget is 300 requests per 60 s per endpoint (T13).
- Providers add undocumented fields and states over time (RISK-06).

Forces:
- Users expect current state when they look at their jobs, but every refresh spends the shared rate budget (ADR 0013).
- Anyone who learns the webhook path can trigger refreshes (RISK-07).
- Several flows race on the same provider item: interrupted submissions, revives, deletions. Without one fixed order, the same listing produces different results.
- A partial listing must not make present items look gone.
- A provider that keeps failing must not be hammered by every read (single-flight only merges concurrent calls, not consecutive ones).

## Decision
debroxy refreshes provider state on demand through one command, `RefreshProviderState{ProviderID, Cause}`, and one pipeline. Webhooks only ask for a refresh.

**When a refresh runs**
1. **Refresh-on-read**: the queries `JobsOfUser`, `JobDetails` and `AllJobs` first issue `Cause=read` with MaxAge 10 s (`refreshMaxAge`). It stays on when webhooks are enabled (DEC-11).
2. **Forced**: `POST /api/v1/providers/{id}:refresh`, MaxAge 0, limited per user (2 per minute, ADR 0013).
3. **Webhook** (owner decision, C6-06): a webhook that arrives while no coalescing window is open refreshes **immediately** and opens a window of `webhookRefreshInterval` (default 30 s). Webhooks inside the window only set a flag. At the end of the window, if the flag is set, **exactly one trailing refresh** runs. The trailing refresh opens a new window (§5.6, C6-06), so a steady flood costs one refresh per window.
4. **Follow-ups** after an ambiguous create: +30 s, +2 min, +5 min, +10 min, +15 min. A follow-up is skipped when an authoritative sync younger than its due time already ran. Remaining follow-ups are cancelled when the submission resolves.
5. **Safety**: every 5 min while any item of the provider is active (including `submitting`), otherwise every 30 min (DEC-12).
6. **MaxAge** is measured against `provider_sync_state.last_authoritative_sync_at`. Only a refresh whose listing returned `Complete = true` updates it (§5.6 step 1). `Complete` is the one name for this flag in the gateway result, the pipeline and this ADR (C5-05); the column name keeps "authoritative" and means the same.
7. **Single-flight per provider**: while a refresh runs, callers for the same provider wait for it and share its result. All causes share the provider's rate budgets.

**Back-off and stale reads** (DEC-41, C5-07)
8. A refresh that fails, or whose listing is not `Complete`, starts or extends a per-provider **back-off**: 5 s, doubling up to 2 min (`refreshBackoff`). A refresh with a `Complete` listing ends it.
9. During the back-off, `read` refreshes do not call the provider. `JobsOfUser`, `JobDetails` and `AllJobs` answer from stored state and carry `stale: true` (§10.2). The read whose own refresh just failed is already inside the back-off and answers the same way. Outside a back-off, responses carry `stale: false`.
10. The other causes keep their own pacing during a back-off: `forced` (per-user limit), `webhook` (coalescing window), `safety` and `follow-up` (fixed schedules). Their outcomes reset or extend the back-off like any refresh (rule 8; §5.6, C6-06). §5.6 names `webhook`, `safety` and `follow-up`; `forced` is not paused either, because the back-off suspends only `read`.
11. An incomplete listing still applies its observations through steps 14–18; only absence (step 19) and the MaxAge timestamp need `Complete`.

**Webhook handling** (ddd-rules IN-5, DEC-23)
12. Route `POST /webhooks/{providerId}/{secret}` enters through `job` (`HandleProviderWebhook`), which calls `ProviderGateway.TranslateWebhook`. The adapter compares the path secret with `crypto/subtle.ConstantTimeCompare` **before** reading the body. The secret is at least 32 random bytes (≥ 256 bit), validated at startup. A rejected request triggers nothing; its response does not reveal whether the provider ID or the secret was wrong. The body is read with a size limit and never trusted for state; the only output is `WebhookSignal{RefreshRequested}`. Access logs, error messages and traces never contain the `/webhooks/*` path (NFR-03). A secret in a header or query string is not possible with TorBox and would leak the same way. Secret rotation is an operator procedure.

**The refresh pipeline** (DEC-34). The order is binding. Each item change is its own transaction on one item (D-11).
13. `ProviderGateway.ListRemoteItems` → observations and `Complete`. `Complete = true` only if every page and every usable kind was listed without error.
14. **Known live items**: provider ID held by a claimable item → `Observe`.
15. **Interrupted submissions**: remaining observations whose provider hash matches a `submitting` item → the outcome logic of §5.5 step 5 (`CompleteSubmission` / `DeferSubmission`, ADR 0005, including the late deletion check, DEC-39). Remaining follow-ups for that item are cancelled. A `deleting` item without a provider ID records it and writes `DeleteRemoteItem` (ADR 0008). Running this before step 16 makes such an observation complete the submission, not revive an item.
16. **Reappearing items**: remaining observations whose provider ID belongs to a `failed`/`gone` item and whose state is active or ready → `Revive`, only if **no claimable item conflicts on any of the three unique keys** and **no `deleting` item holds the provider item ID** (fingerprint, provider hash, provider item ID); with several `failed`/`gone` candidates, the newest by `created_at` is revived (§5.2, C5-10, ADR 0007). `Revive` adds no job; an active result sets `chargeHolder = none` and `chargeHolderReleased = true` (every claimant is charged), a non-active result clears both charge fields (§5.3, ADR 0006). A revive is also blocked while **a `deleting` item holds the same provider item ID** (§5.2, C6-01, DEC-44, ADR 0008 rule 7), and when the observation is **not newer than the `deletedAt` of every item that held that ID** (C7-01). A blocked revive is logged and routed by its cause (§5.6 step 4): if a `deleting` item holds the provider ID, the observation is dropped (step 17); otherwise it becomes a leftover (step 18).
17. **`deleting` items**: observations are ignored; the deletion has not propagated yet.
18. **Leftovers** → Unmanaged Items read model. They are never adopted; the only provider items debroxy takes over from a listing are the matches with `submitting` items in step 15 (DEC-10).
19. **Absence**, only if `Complete`: claimable items with a provider ID missing from the observations → `MarkGone`, except items created, completed or revived within the absence grace (default 60 s, per provider).

**Observation rules**
20. `ObservedAt` is debroxy's clock at the start of the request that produced the observation (D-10). `Observe` drops observations older than `lastObservationAt` (DEC-09b). Provider timestamps are never used for ordering.
21. **Tolerant reader** (NFR-11): unknown fields are ignored. An unknown native state keeps the current Lifecycle and is logged once per value. A forbidden sync transition is not applied and is logged (DEC-09). State mapping is one table per provider (OUT-3, §8).

**Per-provider listing** (adapter)
22. TorBox: `mylist?bypass_cache=true` per usable kind plus `queued/getqueued`.
23. Real-Debrid: walk `/torrents` pages until 204, then call `/torrents/info/{id}` per **non-terminal item** (§5.6). Precisely: a listed item gets an `info` call when its ID is held by a debroxy item in any state except `deleted` (the only terminal state, §5.2), or when its hash matches a `submitting` item or a `deleting` item without a provider ID. A listed item that matches no debroxy item gets no `info` call; its list data is enough for the Unmanaged Items view. A failed page or `info` call sets `Complete = false`.

## Consequences
### Positive
- Users see state at most 10 s old when they look, with or without webhooks, as long as the provider answers.
- A failing provider costs at most one read-triggered call per back-off step, and users get stored state flagged `stale` instead of errors.
- A leaked webhook secret costs at most one extra refresh per 30 s per provider.
- One ordered pipeline removes the races between submission, revive, deletion and absence.
- Real-Debrid's clock bug and stale list cannot corrupt state.

### Negative
- A read can wait for a provider round trip. A Real-Debrid refresh costs one `info` call per tracked item, including `ready` items whose state no sync transition can change. Narrowing the predicate (for example, skipping `ready` items) would save budget but departs from §5.6; it is left for a later revision.
- During a back-off, reads may show state up to 2 min older than the MaxAge promise; `stale: true` says so.
- No push to clients; a client must ask again to see changes.
- Between reads and safety runs, stored state can be up to 30 min old for idle providers.

### Risks and mitigations
- **Many readers at once** drain the budget. Mitigation: single-flight plus MaxAge plus back-off; budgets in ADR 0013.
- **Wrong `Complete` flag** marks live items gone. Mitigation: absence only on complete listings, plus the grace period; adapter tests for partial walks.
- **New provider states** are silently ignored. Mitigation: log once per unknown value; fixtures as regression tests.

## Alternatives considered
**Polling only.** A periodic refresh is the only trigger; reads and webhooks never refresh. A user who looks right after a change sees state up to one interval old. A short interval fixes that only by spending the shared budget all the time. Refresh-on-read gives fresh state exactly when someone looks, so polling stays only as the slow safety loop.

**Webhook-driven state.** Revision 1 turned refresh-on-read off in webhook mode (old DEC-11). The TorBox payload names no item, fires only for uncached completions and carries no signature (T12). It cannot carry state, and missed or forged webhooks would leave state wrong. Rejected.

**Background continuous polling.** A loop lists every provider every few seconds. It keeps state fresh but spends the shared budget all the time, including idle hours. On Real-Debrid each pass costs one `info` call per item (RD-N04). Rejected in favour of on-demand refreshes and a slow safety loop.

**Fail reads while the provider fails.** Reads would return `provider-unavailable` instead of stored state. Users would lose sight of their jobs during every provider hiccup, and retries would add load. Stored state flagged `stale` is more useful and cheaper.

**Webhook interval without a trailing refresh.** Webhooks inside the window would be dropped. A completion reported late in a window would wait for the next read or safety run. The single trailing refresh costs at most one extra call per window (owner decision).

## Compliance
- App tests for the pipeline with a fake gateway, one per ordering case: an ambiguous TorBox create that dedups onto a `failed` item completes the submission, not a revive; `deleting` observations are ignored; a partial listing never calls `MarkGone` but still applies `Observe`; items inside the grace survive absence; a revive blocked by a claimable item on fingerprint or hash goes to Unmanaged Items; a revive blocked by a `deleting` holder of the provider item ID is dropped (neither revived nor listed as unmanaged) and the item stays `failed`/`gone`; with two `failed` candidates the newest is revived; revive charge fields as §5.3.
- Single-flight test: 50 concurrent reads → one `ListRemoteItems` call.
- Back-off test with a fake clock: after a failed listing, reads within 5 s make no gateway call and return `stale: true`; consecutive failures double the delay up to 2 min; a `Complete` listing ends the back-off and updates `last_authoritative_sync_at`; an incomplete listing does not.
- Coalescing test with a fake clock: 10 webhooks in 30 s → one immediate and one trailing refresh; the trailing refresh opens a new window, so a webhook 1 s after it only sets the flag; a continuous flood → one refresh per 30 s.
- Back-off pacing test: during a back-off, `webhook`, `safety` and `follow-up` refreshes still call the gateway on their own schedule; a `Complete` result ends the back-off, a failed one extends it.
- Stale observation test: an older `ObservedAt` changes nothing.
- Webhook adapter test: wrong secret → no refresh and identical response for unknown provider; the secret check runs before the body is read; `12-webhook-received.json` translates to `RefreshRequested` only.
- Log-redaction test: a request to `/webhooks/torbox/<canary>` leaves no canary in captured logs.
- Adapter tests (T-5) on `docs/discovery/`: Real-Debrid `13-torrents-*.http` (paging, 204 end), `03-dup-*.http` (stale list), `info` called exactly for the items rule 23 names; TorBox `02-baseline-*.http`, `25-sintel-mylist.http`; an unknown state value logs once.
