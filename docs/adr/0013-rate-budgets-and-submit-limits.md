# 0013. Provider rate budgets and per-user submit limits

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 7 (unchanged sections from revisions 5–6 apply), §1.5 RISK-02/RISK-07, §5.1 (`Submission.NotBefore`), §5.4 (`ExpireSubmissions`, `ResumeDeferredSubmissions`), §5.5 steps 3–5, §5.6, §5.8 (`submit_rate_windows`), §6.2 `OpenDeliveryStream`, §6.4 (budget and 429 rows), §9.4, §10.3, §11, §12 NFR-09 ; decisions DEC-24, DEC-21, DEC-18, DEC-40, DEC-43 ; changes C4-12, C5-03, C5-06, C5-08, C6-02, C6-03, C6-04, C6-05 |
| Related | ADR 0003, ADR 0005, ADR 0009, ADR 0010, ADR 0012; ddd-rules OUT-4, A-9, D-10, R-10, S-3; evidence TorBox T09, T13, TB-N05; Real-Debrid R11; assumption ASM-R11 |

Revised 2026-10-11 for revision 6.

## Context
All users share one provider account. Its rate limits are a shared resource, and one user or one bug can exhaust them for everyone.

Observed provider behaviour:
- TorBox sends `x-ratelimit-limit`, `x-ratelimit-remaining`, `x-ratelimit-reset` (epoch float) and `retry-after`. Buckets are **per endpoint**. The general limit is **300 per ~60 s** (torbox.md T13; `02-create-magnet-cached.http`).
- **Uncached creates cost 60 per hour**: `x-ratelimit-limit: 60`, `retry-after: 3597` on an uncached magnet (T13, TB-N05; `02-create-magnet-uncached.http`). Cached **torrent** creates use the 300 bucket (`03-readd-same-magnet.http`, `25-create-sintel-torrentfile.http`). **Web creates always use the 60-per-hour bucket**, even for a cached re-add (torbox.md T13 as corrected; `08-readd-same-webdl.http`).
- The TorBox CDN zip endpoint has its own **60 per 60 s** limit (T13; `04-cdn-zip-head.http`). **CDN file downloads carry no rate-limit headers** (T13; `04-cdn-head.http`, `04-cdn-range-1mib.http`).
- TorBox `checkcached` tells whether a hash is cached before creating (T09).
- **Real-Debrid sends no rate-limit headers** on any response, API or CDN (realdebrid.md R11). Its API limit is assumed to be about 250 per minute (ASM-R11, unverified).
- A Real-Debrid refresh costs one `info` call per tracked item (RD-N04, ADR 0009).

Forces: budgets live in memory (DEC-21), so a restart forgets how much of the window is used. Webhooks and forced refreshes can be abused to spend the budget. Joining an existing item makes no provider call and must stay free. A create that was never sent is safe to send later; a create that may have been sent is never re-sent blindly (NFR-09).

## Decision
debroxy keeps provider rate budgets per Provider Account and operation class inside `provider`, bounds CDN file downloads by stream caps, and limits each user's create submissions and forced refreshes.

**Provider budgets** (provider adapters, OUT-4)
1. Each Provider Account has its own limiter per operation class, in memory (DEC-21). Every **API** call, and every TorBox **zip** CDN request, takes a token from its class before it is sent. There is no shared global client or limiter.
2. **CDN file downloads have no rate class** at either provider (C5-08). The per-user and global stream caps bound them (DEC-18, ADR 0010). This includes Real-Debrid CDN downloads, which are outside the `general` API class.
3. TorBox classes and static defaults:
   - `general`: 300 per 60 s, **one bucket per endpoint path**;
   - `uncachedCreate`: 60 per hour, one bucket per create endpoint. The name is kept from configuration; it is the provider's hourly create bucket, which also covers every web create;
   - `cdnZip`: 60 per 60 s, for upstream requests to zip CDN URLs. The token is taken inside `provider`'s `OpenDeliveryStream` (DEC-40, ADR 0010), the only place where CDN requests are made.
4. **Create class (TorBox):** a torrent create takes from `uncachedCreate` unless `CheckCached` reported the hash cached in the same submission; then it takes from `general`. A web create **always** takes from `uncachedCreate` (T13 as corrected), and no `CheckCached` runs for it (rule 14, C6-04).
5. Real-Debrid has one class, `general`: 250 per 60 s for the whole API (ASM-R11).
6. **Bucket keying and header updates.** A bucket is keyed by (Provider Account, class, endpoint path); Real-Debrid `general` has one key for the whole API. When a response carries `x-ratelimit-*` headers, they update a bucket of that endpoint:
   - the reported limit selects among the endpoint's known buckets (on a TorBox create endpoint, 60 → `uncachedCreate`, 300 → `general`), so a call debroxy charged to the wrong class corrects the right bucket;
   - a limit value that matches no known bucket updates the bucket the call was charged to, adopting the reported limit, remaining count and reset, and is logged once per value (RISK-06);
   - a missing, unparseable or past `x-ratelimit-reset`, or a negative remaining count, makes the adapter ignore that response's headers and keep its own count; the anomaly is logged once per value. A parseable `retry-after` is still honoured.
7. **Waiting.** On 429, or when a bucket is empty, an idempotent call waits for `retry-after` or the bucket reset if that fits the call's deadline. Otherwise it fails with `provider-unavailable` and `retryAfter` (§6.4). For a **create**, only the empty-bucket wait applies (before sending; rule 8). A **429 answer to a create** is not retried inside the call and is not `not-sent`: `CreateRemoteItem` reports it as an **ambiguous failure** (§5.5 step 4, §6.4, C6-03, DEC-43). The item stays `submitting` and follow-up refreshes resolve it (ADR 0005, ADR 0009). A 429 normally means nothing was created, but treating it as ambiguous never re-sends a create that may have reached the provider (NFR-09).
8. **Budget-blocked creates** (§5.5 step 4, §6.4, C5-08, C6-02, DEC-43). A create whose bucket is empty, and that cannot wait within its deadline, is **not sent**. `CreateRemoteItem` reports this as a distinct neutral result, `not-sent{retryAfter}`, never as an ambiguous failure. `ExecuteSubmission` then, in a second short transaction, undoes the attempt it had started (`AttemptStartedAt` cleared, `Attempts` back to its previous value) and sets `Submission.NotBefore = now + retryAfter`. The item stays `submitting` with no attempt in progress:
   - `ExpireSubmissions` counts only from a started attempt, so the wait for the hourly bucket (up to 1 h, longer than the 20 min submission timeout) never expires it as `submission-lost` (§5.4);
   - `ResumeDeferredSubmissions` (every minute) runs `ExecuteSubmission` again once `NotBefore` has passed. `NotBefore` is stored in the item, so the re-run survives a restart (§5.4);
   - the attempt-start transaction proceeds **only if no attempt is in progress** (§5.5 step 4), so two runners never send the same create.
   A create that may have reached the provider is never re-sent; ambiguous failures follow ADR 0005 (follow-up refreshes).
9. **Startup** (C4-12): a header-reporting bucket allows **one call at a time until its first headers arrive** after a restart. A header-less bucket (Real-Debrid) gets **50 % of its static budget** for the first window after a restart, since the provider may still count calls from before it.
10. **Configuration.** Static budgets come from `providers[].rateBudgets`, validated once at startup to be > 0 (S-3). For TorBox the entries are `general`, `uncachedCreate` and **`cdnZip`**:
    ```yaml
    rateBudgets: { general: { requests: 300, per: "60s" }, uncachedCreate: { requests: 60, per: "1h" }, cdnZip: { requests: 60, per: "60s" } }
    ```
    This is the TorBox entry of §11 (C6-05).
11. Refreshes of every cause share the budgets (ADR 0009). Single-flight, MaxAge, the refresh back-off and webhook coalescing keep their cost bounded.

**Per-user limits** (`job` context)
12. **Submit rate** (DEC-24) applies only on the create path of `SubmitJob` (§5.5 step 3: no claimable item exists). Two limits per user, each over a **rolling hour** (owner decision):
    - **uncached**: default 10 per hour (`submitRate.perHour`);
    - **cached** ("cheap"): default 60 per hour (`submitRate.cachedPerHour`), only for providers with `CachedCheck`.
13. **Counter shape.** `submit_rate_windows` holds one row per accepted submit: (`user_id`, `class` ∈ `uncached` | `cached`, `accepted_at`). A limit is checked by counting the user's rows of that class with `accepted_at` within the last hour, inside the write transaction (R-10). A cached submit counts only against the cached limit, an uncached submit only against the uncached limit. Rows older than one hour are pruned.
14. **Classification.** For providers with `CachedCheck`, `SubmitJob` calls `CheckCached` for the provider hash of a **torrent** submit after a read finds no claimable item and **before** the step-3 transaction. A hash reported cached makes the submit `cached`; anything else, **including a failed or empty check, makes it `uncached`** (owner decision). **Web submits skip `CheckCached` and always count as `uncached`** (§9.4, C6-04): TorBox charges every web create to the hourly bucket, so a web submit must not use the cheap limit.
15. **Reject before `CheckCached`.** If the user is already at **both** limits, `SubmitJob` rejects with `submit-rate-exceeded` before calling `CheckCached`, so an over-limit user spends no shared `general` token. If only one limit is reached, `CheckCached` runs and the step-3 transaction checks the limit of the resulting class. Web submits and providers without `CachedCheck` check only the uncached limit, in step 3, with no `CheckCached` call.
16. The row is written in the same transaction as `StartSubmission`. A submit counts when it is accepted and is **not refunded** if the create later fails or is budget-blocked (owner decision).
17. **Joins are free**: claiming an existing claimable item (including a `submitting` one) uses no submit rate, no `CheckCached` and no provider budget. An idempotent retry with an existing `JobID` (§5.5 step 1) counts nothing.
18. **Forced refresh**: at most 2 per user per rolling minute (`forcedRefresh.perUserPerMinute`), counted in memory (a restart resets it; harmless). Over the limit, the request is rejected with the slug **`refresh-rate-exceeded`** (type quota-exceeded, HTTP 429, with `retryAfter`; §10.3, C6-05) and no refresh runs.

## Consequences
### Positive
- debroxy stays inside the provider's limits under load and after restarts, instead of finding them by 429.
- The expensive TorBox hourly create budget (60 per hour for all users) cannot be taken by one user.
- A budget-blocked create costs the user nothing beyond the accepted submit and completes on its own once the bucket refills.
- Joins stay free, so sharing content never costs quota.
- Header-driven buckets track the provider's own counters, including calls made just before a restart.
- All budgets, including the zip CDN budget, live in one place (`provider`).

### Negative
- A cached-check call before each new TorBox torrent submission costs one `general` request (except for users at both limits).
- The 50 % start can slow Real-Debrid refreshes for one minute after a restart.
- Ten users each within their limit can still exhaust the 60-per-hour create bucket together. Later creates then wait as `submitting` for up to an hour.
- CDN file downloads are limited only by concurrency, not by request rate; a provider limit on CDN requests would be found only by errors.
- A failed or budget-blocked create still counts against the user's submit rate.
- A cached web submit counts against the user's uncached limit (10 per hour by default), although the content is already at the provider; this mirrors what it costs the shared provider budget.
- A provider 429 on a create costs a follow-up schedule (up to 15 min) before the submission resolves, where an immediate re-send would often succeed.

### Risks and mitigations
- **Real-Debrid's real limit differs** from 250 per minute (ASM-R11). Mitigation: configurable budget; 429 handling with back-off; verify in a discovery top-up.
- **TorBox changes bucket layout.** Mitigation: rule 6 follows the headers, logs unknown limits once, and falls back to the static model on malformed headers; fixtures as regression tests.
- **A budget-blocked create waits long.** With the hourly bucket empty, the submitter sees `submitting` (202) for up to an hour. Mitigation: the per-user submit rate (rules 12–16) keeps one user from emptying the bucket; the wait is safe because nothing was sent (rule 8).
- **A 429 on a create that did create the item** (not observed). Mitigation: it is ambiguous (rule 7), so provider-hash matching during follow-up refreshes completes the submission instead of creating a duplicate (ADR 0005).

## Alternatives considered
**No debroxy-side limits; rely on provider 429.** Simple, but a 429 hits every user at once, and TorBox's hourly create bucket then blocks creates for up to an hour. Real-Debrid sends no headers, so debroxy could not even see how close it is. Provider-side abuse detection may also react to repeated 429s.

**One global token bucket per provider.** Ignores that TorBox buckets are per endpoint and per create class (T13). A burst of list calls would block creates and downloads that have their own budget, and the 60-per-hour create bucket would still be overrun.

**A rate class for CDN file downloads.** Neither provider reports a limit for them, so any value would be invented, and a rate on requests does not bound long-running streams. Stream caps bound what matters (concurrency and bandwidth) (C5-08).

**Fail a budget-blocked create.** Simple, but the user would lose a submit that never reached the provider and have to resubmit by hand. Nothing was sent, so waiting is safe.

**Treat a provider 429 on a create like `not-sent`**. It would re-send sooner, but it relies on the provider never creating the item before answering 429, which is not verified. Rejected in revision 6 (C6-03): ambiguous handling is safe either way.

**Per-user quotas only.** Limits each user but not the sum: many users within their quotas can still exceed the provider limit. Refreshes and safety runs belong to no user. Provider budgets are still needed; per-user limits only add fairness.

## Compliance
- Adapter tests (T-5) on `docs/discovery/torbox/`: `02-create-magnet-uncached.http` updates the `uncachedCreate` bucket to 59 remaining with reset in ~1 h; `02-create-magnet-cached.http` updates the 300 bucket; `08-readd-same-webdl.http` updates the web create's hourly bucket; `04-cdn-zip-head.http` updates `cdnZip`; `04-cdn-head.http` updates nothing.
- Header tests: an unknown limit value updates the charged bucket and logs once; a malformed reset leaves the bucket unchanged.
- Startup tests with a fake clock: concurrent calls on a fresh header bucket run one at a time until headers arrive; a fresh Real-Debrid bucket admits 125 calls in the first window.
- 429 and empty-bucket tests with an `httptest` server: wait within the deadline, else `provider-unavailable` with `retryAfter`; an empty create bucket returns `not-sent` without any HTTP request; a create that reached the server is never re-sent.
- App tests for `ExecuteSubmission`: `not-sent` → item stays `submitting`, `Attempts` unchanged, `AttemptStartedAt` cleared, `NotBefore = now + retryAfter`; a 429 on the create → ambiguous failure with follow-ups scheduled, no re-send; a second runner finding an attempt in progress sends nothing.
- App tests for the periodic jobs with a fake clock: `ExpireSubmissions` does not expire a budget-blocked item older than the submission timeout; `ResumeDeferredSubmissions` re-runs it once `NotBefore` has passed, also after a simulated restart (state reloaded from the repository).
- App tests for `SubmitJob`: the 11th uncached create in an hour fails; a cached submit uses only the cached limit; a failing `CheckCached` counts as uncached; a web submit makes no `CheckCached` call and counts as uncached; a user at both limits is rejected without a `CheckCached` call; a join does not count; a repeated `JobID` does not count; a failed create is not refunded.
- App test: the third forced refresh in a minute is rejected with `refresh-rate-exceeded`.
- Review checklist: every provider HTTP call goes through the account's limiter (no shared `http.Client`, OUT-4); no limiter or HTTP client exists outside `provider`.
