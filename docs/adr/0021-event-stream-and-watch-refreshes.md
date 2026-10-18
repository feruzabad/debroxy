# 0021. Event stream with a change feed and `watch` refreshes

| | |
|---|---|
| Status | Accepted (2026-10-13, project owner) |
| Date | 2026-10-13 |
| Deciders | project owner |
| Spec | revision 13, §5.11, §10.1, changelog C13-01; decision DEC-54 |
| Related | amends ADR 0009 ("When a refresh runs", rules 1, 9, 10); ADR 0002 (single writer), ADR 0004 (claims in the aggregate), ADR 0013 rule 11 (shared budgets); ddd-rules R-6, A-9 |

## Context

- ADR 0009 refreshes provider state on demand. Its causes are `read` (the job queries, MaxAge 10 s), `forced`, `webhook`, `follow-up` and `safety`. A client that wants live progress must poll the job queries; every poll is a `read`.
- A browser client needs live job progress and state changes for its users, and live changes of all jobs for admins. Polling many tabs multiplies requests while adding nothing over one refresh every `refreshMaxAge`, because single flight and MaxAge already collapse them.
- A push channel only helps if something keeps the provider state fresh while nobody polls. Otherwise changes surface only at the next `safety` refresh (5 or 30 min).
- Job changes are committed by the RemoteItem repository, one aggregate per transaction (ADR 0002, ADR 0004). The aggregate records domain events that the repository already persists in the same transaction (R-6).

## Decision

**Event Stream**
1. `job` serves `GET /api/v1/events` as `text/event-stream` (server-sent events). It is authenticated like every API route, by cookie or bearer token, and runs under the caller's principal.
2. **Scope.** By default the stream carries the caller's own jobs. `?scope=all` carries every user's jobs, for admins only; others get 403 `forbidden`.
3. **Events are snapshots** in the §10.2 representation, never deltas:
   - `job.upserted`: the full Job; `ownerUsername` only in `scope=all`.
   - `job.released`: `{jobId, historyEntry}`.
   - `quota.updated`: the caller's `QuotaUsage`; own scope only.
   - `provider.stale`: `{providerId, stale}`, whenever a provider's back-off starts or ends.

   A comment line (`: ping`) is sent every 25 s. Each event has an `id:`, but there is **no replay**: `Last-Event-ID` is ignored, and a reconnecting client refetches its lists over REST. The stream opens with `retry: 3000`.
4. **Limits.** At most `job.events.maxStreamsPerUser` (default 4) per user and `job.events.maxStreamsTotal` (default 256) streams at once. A stream over a limit answers 503 `too-many-streams` with a retry hint (ADR 0020). Shutdown ends every stream before background work stops.

**Change Feed**
5. The RemoteItem repository reports each **committed** aggregate change to a new driven port, `out.ChangeNotifier`, after its transaction commits: the item ID, the owners of its claims before and after, and the IDs of released jobs, taken from the aggregate's recorded events. Nothing is reported for a rolled-back transaction. The notifier is called outside the transaction and never blocks the writer (A-9). Refresh back-off changes are reported the same way, by the Refresher.
6. **Layering (ddd-rules §3):**
   - **The hub.** An in-memory hub, `job/adapters/out/changefeed`, implements `out.ChangeNotifier` and a second driven port, `out.ChangeSubscriptions`, through which a subscriber receives change notices. It holds only IDs, never job data, and drops a notice for a subscriber whose bounded queue is full, marking that subscriber as overrun.
   - **The operation.** A new driving port, `in.WatchJobs{Actor, Scope}`, implemented in `job/app/query`, subscribes through `out.ChangeSubscriptions`. It re-reads each affected job through the existing read models, without a refresh, and filters it by the actor and scope. It yields events on a bounded channel and ends when its context ends or its subscription is overrun.
   - **The transport.** The HTTP adapter (`job/adapters/in/http`) calls only `in.WatchJobs` and writes the events as SSE. An overrun ends the stream, and the client reconnects and refetches.
7. The feed is in memory: a restart loses nothing durable, because clients refetch on reconnect.

**`watch` refreshes** (amends ADR 0009)
8. ADR 0009 "When a refresh runs" gains a sixth cause, **`watch`**. While a `WatchJobs` operation runs, it issues `RefreshProviderState{Cause: watch}` with MaxAge `refreshMaxAge` every `refreshMaxAge` (10 s) for each provider its stream covers; single flight and MaxAge collapse the refreshes of concurrent watchers into one per provider. In own scope, the covered providers are those of the caller's items, plus all enabled providers while the caller has a `submitting` item. In `scope=all`, every enabled provider is covered.
9. `watch` is paced like `read`. It shares single flight and the rate budgets (rule 7, ADR 0013 rule 11), it is **paused during a back-off** (rule 9), and its outcome resets or extends the back-off like any refresh (rule 8). Rule 10's list of causes that are not paused does not include `watch`.
10. With no open streams there are no `watch` refreshes. All other rules of ADR 0009 are unchanged.

## Consequences

### Positive
- Many open tabs cost one refresh per provider every 10 s, the same bound as polling, plus one push per change.
- Clients get state changes within one refresh interval without polling, and admins see all jobs live.
- Snapshots keep the client simple: an event replaces a cache entry, and a lost event is healed by the next snapshot or a refetch.

### Negative
- A stream open in a forgotten tab keeps its providers refreshing every 10 s.
- Each change costs one read per affected job to build the snapshot.
- No replay: a client that misses events while reconnecting must refetch.

### Risks and mitigations
- *Risk:* a slow client holds memory. *Mitigation:* bounded per-stream buffers; a full buffer closes the stream (rule 6).
- *Risk:* many streams exhaust the rate budget. *Mitigation:* `watch` uses single flight and MaxAge, so its cost does not grow with the number of streams. The per-user and total stream limits bound memory.
- *Risk:* a change is reported before its commit is visible to readers. *Mitigation:* reports happen strictly after commit (rule 5), and the feed reads through the reader pool afterwards.

## Alternatives considered

**WebSockets.** Bidirectional, but the client sends nothing. SSE runs over plain HTTP, reconnects by itself in browsers, and passes the existing middleware (authentication, access log, `Origin` rules for GET are unaffected).

**Invalidation hints instead of snapshots.** Events would only say "job X changed", and clients would refetch. Simpler to produce, but every change would cost one REST request per open tab. Rejected by the owner.

**Push only what existing refreshes find.** No new cause. Changes would surface only at `safety` (5 or 30 min) or webhook refreshes, which is too slow for live progress.

## Compliance

- Repository contract test: a committed change reaches a fake notifier once, and a rolled-back transaction reaches it never.
- The architecture test keeps holding: the hub is an `adapters/out` package, `WatchJobs` lives in `app`, and the HTTP adapter imports only `ports/in`.
- Refresher tests: `watch` is skipped during a back-off and shares single flight with `read`; no `watch` runs without streams.
- Stream tests: scope filtering (no other user's job in own scope; `ownerUsername` only in `scope=all`), limits, ping, closing on a full buffer, and closing on shutdown.
