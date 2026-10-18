# 0002. Embedded SQLite as the only store, relying on single-writer serialization

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 7 (unchanged sections from revisions 5–6 apply), §1.3, §5.1 (`deletedAt`, `AttemptStartedAt`), §5.2 (uniqueness rules), §5.5 (steps 1 and 3–6, slot-quota consistency), §5.7, §5.8 (incl. storage setup), §14 (ASM-S01), NFR-05; changelog C5-01, C5-02, C5-06, C6-07; decisions DEC-08 (storage part), DEC-13, DEC-24, DEC-37, DEC-38, DEC-39, DEC-42 |
| Related | ADR 0001, ADR 0005, ADR 0006, ADR 0007, ADR 0008, ADR 0013; ddd-rules §2 (`internal/common/sqlite`), R-2, R-5, R-6, R-7, R-10, D-8, D-11, DEP-1, A-9, OUT-1, OUT-2, S-1, T-3, §13 |

Revised 2026-10-11 for revision 6.

## Context

debroxy stores metadata only: users, sessions, Remote Items, claims, quotas, outbox messages and learned capability state. File bytes are never stored (NFR-01). The data set is small. It runs as one self-hosted binary, pure Go, on Linux and macOS (NFR-05).

Several decisions about one `RemoteItem` need facts about other aggregates. They read first and write second:

- **Slot quota** (DEC-08): before a charged claim or a new submission, count the active items on which the user holds a slot charge. The count reads other `RemoteItem` aggregates; the write changes only one (D-11 holds). Two concurrent submissions must not both pass the check.
- **Join or start** (§5.5 step 3): look up a claimable item by (provider, fingerprint); claim it or create a new one.
- **Submit rate** (DEC-24): count the user's accepted submits in the rolling hour before a new submission starts.
- **Submission outcome** (§5.5 step 5): look up other items that hold the returned provider item ID. A claimable holder leads to `merged-into`, a `deleting` holder to deferral. The **late deletion check** (C5-01, DEC-39) looks for an item that held the ID and has `deletedAt ≥ AttemptStartedAt`.
- **Outbox deletion** (§5.7, DEC-37): transaction 1 checks whether a claimable item has taken over the provider item ID.

Without serialized writers, two transactions can both read "no item" or "quota free" and both write. Revision 2 called this an ADR-required D-11 exception. This was later corrected: reading other aggregates is allowed, so the reliance is a storage assumption (ASM-S01), not a rule exception. ddd-rules R-10 now defines how a repository hands such reads to the update function.

The **idempotency check by `JobID`** (DEC-38) is different. §5.5 step 1 is a plain read before any write transaction; when the `JobID` exists, no other step runs.

Provider calls are slow and can hang. TorBox uncached creates have a budget of 60 per hour (torbox.md T13), and a create can still succeed after the client call was interrupted (torbox.md §5). A transaction that stays open across such a call blocks every other writer. The outbox processor faces exactly this risk.

## Decision

1. **SQLite is the only store.** debroxy uses one SQLite database file through `database/sql`, the pure-Go driver `modernc.org/sqlite` (no CGO), sqlc for queries and goose for migrations (ddd-rules §13). sqlc code stays in each context's `adapters/out/sqlite` (OUT-2); the shared package `internal/common/sqlite` (rule 2) owns no tables and no queries. Table ownership and migration naming follow DEC-13, DEC-42 and ADR 0001 rule 9.
2. **Two pools on the same file** (DEC-42, owner decisions). The process opens exactly one of each, shared by all contexts. The shared-kernel package `internal/common/sqlite` (ddd-rules §2, C6-07) creates both pools and takes the start lock of rule 11. `cmd` calls it once at startup and passes the handles to each context's `service.NewApplication` (S-1); no context opens the database file itself, and `cmd` closes the pools and releases the lock on shutdown. Each context still owns its tables (DEC-13):
   - The **writer pool** has `SetMaxOpenConns(1)` and `SetMaxIdleConns(1)`, with no connection lifetime or idle limit, so its single connection stays open. Every transaction on it begins with `BEGIN IMMEDIATE` (DSN `_txlock=immediate`).
   - The **reader pool** serves queries and read models. Each of its connections sets `query_only` (DSN `_pragma=query_only(1)`), so SQLite rejects every write on it, whether in a transaction or as a bare `Exec`. Its size is a tuning value, not part of this decision.
3. **Per-connection settings come from the DSN.** SQLite applies `foreign_keys` and `busy_timeout` per connection, and both are off by default. Both DSNs therefore carry `_pragma=foreign_keys(1)` and `_pragma=busy_timeout(5000)`; the 5 s value is fixed by DEC-42 (owner decision). The writer DSN also carries `_pragma=journal_mode(WAL)`; WAL mode is stored in the file. When `database/sql` replaces a broken connection, the new connection gets the same settings from the DSN. A transaction that fails on a broken connection is rolled back and returns its error. The repository retries it only in the case of rule 7.
4. **Every write transaction starts with `BEGIN IMMEDIATE`** (R-5). It takes the write lock before its first read, so its reads and writes see no concurrent writer. The commit error is always returned (R-5).
5. **Reads of other aggregates run inside the write transaction, through a reads port** (R-10). The repository begins the transaction, loads the item and calls `updateFn` with the item and a read-only, use-case-specific port declared in `ports/out` (for example `SubmissionPolicyReads`). The port is bound to the same transaction and has no write methods. The domain receives plain values from it (DEP-1, D-8), and only one aggregate is saved (D-11). Use cases never handle transactions (R-2). This covers:
   - the slot-quota count and the submit-rate check (ADR 0006, ADR 0013 rule 10);
   - the join look-up by (provider, fingerprint). Join-or-start uses one repository method that runs the look-up and then either updates the found item or inserts a new one, both through the same idiom;
   - the outcome look-ups of §5.5 step 5, including the late deletion check;
   - the takeover checks in outbox transactions 1 and 2 (§5.7).
6. **The `JobID` idempotency check is a plain read** (§5.5 step 1, DEC-38). It runs on the reader pool before any write transaction and never holds the write lock. If two requests with the same new `JobID` race past it, the write transaction catches the second one: `Claim` returns `ErrDuplicateJobID`, and the primary key of `jobs` is the storage backstop. The use case turns either into the idempotent answer.
7. **Partial unique indexes are the uniqueness backstop.** They mirror ADR 0007 rule 2. `remote_items` has three partial unique indexes, each `WHERE state IN ('submitting', 'queued', 'resolving_metadata', 'awaiting_file_selection', 'downloading', 'processing', 'ready')`, the claimable states of §5.2:
   - (`provider_id`, `fingerprint`);
   - (`provider_id`, `provider_hash`), where the provider hash is known;
   - (`provider_id`, `provider_item_id`), where `provider_item_id IS NOT NULL`.
   A unique violation is never shown to a client; the repository retries the transaction from its look-up (§5.5 step 6).
8. **What rests on ASM-S01, and what does not.** ASM-S01 (§14) covers read-then-write checks under the single writer:
   - **single writer only:** the slot quota, the submit rate, and the look-ups before an insert or a completion (join or start, `merged-into`, deferral, outbox takeover);
   - **single writer plus index backstop:** uniqueness of fingerprint, provider hash and provider item ID among claimable items (rule 7);
   - **timestamps:** the late deletion check (C5-01) compares the persisted `deletedAt` of other items with this item's `AttemptStartedAt` (C5-02). It relies on those timestamps being written by `ConfirmDeleted`, `SupersedeDeletion` and the no-provider-call paths, not on serialization alone.
9. **No transaction spans a network call.** A transaction never stays open across a provider call or any other remote call (A-9). Flows that need both use separate transactions around the call:
   - submissions: transaction, then a short transaction that records `AttemptStartedAt`, then the provider create, then a transaction on the same item (§5.5 steps 3–5);
   - outbox deletion: transaction 1, then the provider delete, then transaction 2, which re-checks (§5.7, DEC-37).
10. **Outbox messages and events share the aggregate's transaction** (R-6).
11. **One process per database file.** Before it opens either pool, `internal/common/sqlite` opens a sidecar file `<database>.lock` and takes an exclusive, non-blocking `flock` on it (`syscall.Flock` with `LOCK_EX|LOCK_NB`; available on Linux and macOS). It keeps the file descriptor open for the life of the process. If the lock is held, debroxy logs the lock path and refuses to start. The operating system releases the lock when the process exits or crashes, so no stale lock remains.
12. **Memory adapters serialize the same way.** Every memory repository holds one mutex per store for the whole load → reads → check → save section, including the calls to the reads port. The shared contract suite (R-7, T-3) then shows the same race behaviour for memory and SQLite.

## Consequences

### Positive

- No external database server. Backup is one file, made with SQLite's online backup or `VACUUM INTO`.
- Quota, rate and look-up checks are simple and race-free without row locks, `SELECT … FOR UPDATE` or serializable retries.
- One writer connection makes `SQLITE_BUSY` between writers inside the process impossible.
- The reads port keeps the domain pure and the app layer free of transactions, while the checks stay atomic.
- The memory and SQLite implementations behave alike under the contract tests.

### Negative

- **One instance only.** debroxy cannot run as several replicas or share a database over a network file system. High availability needs a restart on the same volume.
- All writes, across all contexts, are serialized. Write throughput is bounded by single-transaction latency. That fits the expected load of a few users.
- A slow write transaction delays every writer. Rule 9 keeps write transactions short, but a programming mistake affects everyone.
- Each use case that reads other aggregates needs its own reads port and a matching memory implementation.
- ASM-S01 binds the design to the store. Replacing SQLite needs a new ADR and a redesign of every read-then-write check.

### Risks and mitigations

- *Risk:* someone adds a write path outside the writer pool, and ASM-S01 silently breaks. *Mitigation:* only the writer pool can write; the reader pool rejects writes (rule 2). The contract tests run parallel updates (T-3).
- *Risk:* a provider call is placed inside `updateFn`. *Mitigation:* review item and a test helper that fails when a gateway fake is called while a write transaction is open.
- *Risk:* the claimable-state list in the index `WHERE` clauses drifts from the domain or from ADR 0007. *Mitigation:* a test compares both lists.
- *Risk:* the lock file sits on a file system where `flock` is not reliable (some network file systems). *Mitigation:* such file systems are already unsupported (Negative); the operator documentation says so.
- *Risk:* the database file grows. *Mitigation:* retention rules of §5.8 (deleted items 30 days, history 180 days, unmanaged items 7 days, processed outbox 30 days).

## Alternatives considered

**PostgreSQL.** It allows several replicas and has mature tooling. It needs a server, credentials and backups outside the binary, which contradicts NFR-05 for a small self-hosted tool. The quota check would need explicit locking (`SELECT … FOR UPDATE` on a per-user row, or serializable isolation with retries). That is more code for a load that SQLite handles easily.

**Pluggable stores (SQLite or PostgreSQL behind one interface).** The repository ports already allow it (R-1), and memory adapters exist for tests (R-7). A second production store doubles the contract test matrix. It also makes every read-then-write check depend on isolation guarantees that differ between stores, which ASM-S01 cannot cover. No user asked for it.

**Stateless proxy (no local store).** Claims, quotas, users and sessions have no place at the provider. Real-Debrid has no provider dedup (realdebrid.md RD-N01), so shared items cannot be found again without a local fingerprint index. TorBox's `tags` field (torbox.md TB-N11) has no counterpart at Real-Debrid. Interrupted creates (torbox.md §5) also need a local record before the call (DEC-27).

**SQLite `locking_mode=EXCLUSIVE` instead of a lock file.** It would keep a second process out. It also keeps the reader pool of the same process out, because the writer connection then holds the file lock on its own. That breaks rule 2.

## Compliance

- **Repository contract tests** (T-3) run against memory and SQLite. For the slot quota, N goroutines try to start charged submissions for one user with one free slot; exactly one succeeds. The same pattern covers same-fingerprint starts (one item results), the submit rate (exactly the allowed number pass) and duplicate `JobID`s (one claim; the others get the idempotent answer).
- **Reads-port tests:** the reads port seen by `updateFn` returns the state inside the open transaction; a memory implementation answers the same for the same data.
- **Index test:** the claimable states in each partial-index `WHERE` clause equal the set the domain reports as claimable and the list of ADR 0007 rule 2.
- **Pool tests:** on the reader pool, a write transaction and a bare `Exec` of an `INSERT` both fail. Writer transactions issue `BEGIN IMMEDIATE`. The writer pool reports `MaxOpenConnections == 1`. Every connection of both pools reports `foreign_keys = 1` and the configured `busy_timeout`, also after the writer connection was closed and replaced.
- **Network-in-transaction test:** in SQLite adapter tests, gateway fakes fail when called while a write transaction is open.
- **Lint/review:** only `internal/common/sqlite` opens the database file (`sql.Open`) and takes the lock; contexts receive the pools from `cmd`; no `db.Begin`/`BeginTx` outside `adapters/out/sqlite`; no gateway call inside `updateFn`; reads ports have no write methods; every commit error returned (R-5).
- **Startup test:** a second process on the same database file finds the lock held and refuses to start; after the first process is killed, a new one starts.

## Open points

- ddd-rules is not yet fully aligned with C6-07: §2 lists `internal/common/sqlite`, but the import table of §3 still allows `cmd` only `<context>/service` and `internal/common/server`, and the composition-root template in §12.10 opens the database inside `service.NewApplication`. Both need a ddd-rules update so `cmd` may import `internal/common/sqlite` and the template receives the pools as a dependency (rule 2). This is an editorial fix to ddd-rules; it needs no new decision.
