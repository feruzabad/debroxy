# 0001. Modular monolith with hexagonal architecture, DDD and CQRS

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 7 (unchanged sections from revisions 5–6 apply), §1.2, §3 (3.1, 3.2), §5.8 (storage setup), §9.3, §12 (NFR-05, NFR-10); changelog C5-03, C5-06; decisions DEC-01, DEC-02, DEC-03, DEC-13, DEC-21, DEC-22, DEC-23, DEC-38, DEC-40, DEC-42 |
| Related | ADR 0002, ADR 0003, ADR 0010, ADR 0012; ddd-rules §2, §3, L-1, L-2, DEP-1…DEP-7, A-3, A-5, A-7, A-10, R-2, R-8, D-11, OUT-5, S-1, S-2 |

Revised 2026-10-11 for revision 5.

## Context

debroxy is a self-hosted proxy (§1.2). It ships as a single pure-Go binary (NFR-05) and runs as one process per database file (ADR 0002 rule 11). It has four areas with different language and different rates of change:

- `identity`: users, roles, credentials, sessions.
- `provider`: the translation of two very different provider APIs (TorBox, Real-Debrid), including the CDN requests.
- `job`: the core model of Remote Items, claims, dedup, quota, sync and outbox.
- `delivery`: Signed Links and byte streaming to the client.

Provider behaviour is irregular and changes silently (RISK-06). TorBox returns business errors as HTTP 500 and not-found as 404 with `success:true` (torbox.md T02). Real-Debrid packs multi-file selections into one RAR link (realdebrid.md §4). If these quirks leak into the core, every provider change ripples through the whole code base.

Revision 1 had a cycle: `job → provider` for API calls and `provider → job` for webhooks. It also mounted `identity`'s HTTP middleware from `cmd`, which broke the import rules. Revision 3 changed two aggregates in one transaction. All three came from boundaries that were implicit. The project needs boundaries that tooling can check.

## Decision

1. **One module, one binary.** debroxy is one Go module (`github.com/feruzabad/debroxy`) built into one binary (L-1). There is no `go.work` and no second `go.mod`.
2. **Four bounded contexts**, each a package tree under `internal/`: `identity`, `provider`, `job`, `delivery` (spec §3.1).
3. **Hexagonal layout per context** with exactly the sub-packages of ddd-rules §2, omitting only empty ones (L-2): `domain/<aggregate>`, `ports/in`, `ports/out`, `app/command`, `app/query`, `app/operation`, `adapters/in/<transport>`, `adapters/out/<technology-or-context>`, `service`. `app/operation` exists only in `provider` (A-10). Imports follow the table in ddd-rules §3 (DEP-1…DEP-3).
4. **CQRS in `identity`, `job` and `delivery`.** Driving ports are commands and queries. Command handlers return only `error`; identifiers come in the command (A-3). Queries read through `<Query>ReadModel` ports that return DTOs, never domain entities (R-8). Each context exposes one `ports/in.Application` (A-7).
5. **`provider` exposes operations** instead of commands and queries (A-10, DEC-02). Operations are declared in `provider/ports/in` and implemented in `provider/app/operation`. A-10 applies because `provider` holds no domain state; its only persisted data, `provider_capability_state`, is a rebuildable cache of learned facts, which A-10 allows (DEC-21). ADR 0003 records this decision.
6. **Acyclic context graph** (DEC-23, §3.2). The only allowed edges are:
   - `delivery → job` (`JobTargets` → query `DeliveryTarget`);
   - `delivery → provider` (`DeliveryStreamOpener` → operation `OpenDeliveryStream`, DEC-40). The operation returns a byte stream; CDN URLs never cross this edge (ADR 0010, ADR 0012);
   - `job → provider` (`ProviderGateway` → provider operations, including `TranslateWebhook`);
   - `delivery → identity` (`UserStatusChecker` → query `UserStatus`).
   DEC-23 also allows `job → identity` and `provider → identity`. Revision 5 defines no consumer port for them, so they are added only together with a consumer port in a later spec revision. No edge ever leaves `identity`, and `provider` never depends on `job` or `delivery`. Webhook requests enter through `job/adapters/in/webhook` and the command `HandleProviderWebhook`. `job` then asks `provider` to translate the request.
7. **Cross-context calls only through ports** (DEC-01, DEP-4). The consumer declares the interface it needs in its own `ports/out`. The only package allowed to import another context is the consumer's `adapters/out/<supplier>`, and it imports only the supplier's `ports/in`. It translates types both ways (OUT-5). Domain types never cross a context (DEP-5).
8. **Generic authentication in `common/server`** (DEC-22, as stated in revision 5 §3.2). `internal/common/server` defines `Authenticator interface { Authenticate(ctx, credential) (auth.Principal, error) }` and one generic middleware. The adapter that implements `Authenticator` on top of `identity`'s `AuthenticatedActor` query lives inside `identity` and is built by `identity/service`. `cmd` only passes it to the server; this is what "wired in `cmd`" in DEC-22 means. `cmd` therefore imports only `<context>/service` and `common/server` (ddd-rules §3). Each context's HTTP adapter converts `auth.Principal` into its own `ports/in.Actor` (DEP-6).
9. **Each context owns its tables** (DEC-13). All contexts share one SQLite file (ADR 0002), but ownership is strict:
   - each table belongs to exactly one context, and only that context's `adapters/out/sqlite` reads or writes it (DEC-13);
   - a goose migration touches the tables of one context only and is named `NNNN_<context>_<change>.sql` (DEC-42, owner decision, ddd-rules §2);
   - no foreign keys, joins or views span two contexts. This ban is an elaboration of DEC-13 made by this ADR; the spec does not state it.
   `delivery` has no tables. `provider` owns only `provider_capability_state` (DEC-21).
10. **No transaction spans two contexts.** Every write is one transaction in one context's repository (D-11, R-2).
11. **Manual wiring.** Each context's `service.NewApplication` builds its own adapters (S-1). `cmd` builds the contexts in dependency order: `identity` and `provider` first, then `job`, then `delivery`, and passes each consumer the supplier's `ports/in.Application`.

## Consequences

### Positive

- Provider quirks stay inside `provider/adapters/out/<provider>`. The core sees only neutral types.
- An import-graph test can check boundaries mechanically, so they cannot drift without notice.
- One process, one file, one deploy. The operator runs a single binary with no service mesh.
- Egress, User-Agent, rate budget and CDN links stay in one context, because `delivery` receives a stream, not a URL (DEC-40).
- Every context can be extracted later. Its edges are already ports with translated types.
- Component tests wire each context with memory adapters and fakes (S-2, NFR-10).

### Negative

- More packages and more boilerplate than a flat layout: ports, DTOs and two-way translation at every edge.
- Data that two contexts need is copied through ports (for example the username snapshot on jobs, DEC-03) instead of joined.
- `provider` follows a different driving-port style (operations) than the other three contexts, and one of its operations returns a stream that the caller must close (A-10).

### Risks and mitigations

- *Risk:* a shortcut import (for example `delivery` importing `job/domain`) slips in under time pressure. *Mitigation:* the import-graph test below fails the build.
- *Risk:* `internal/common` grows into a shared business layer. *Mitigation:* DEP-7. Review rejects business words in `common`.
- *Risk:* the shared SQLite file tempts cross-context joins for admin reports. *Mitigation:* rule 9; admin views compose results through ports.
- *Risk:* `delivery` leaks the upstream stream when a client disconnects. *Mitigation:* the caller closes the body (A-10); ADR 0010 tests disconnects.

## Alternatives considered

**Microservices, as in wild-workouts.** The reference project splits `trainer`, `trainings` and `users` into separate services. For debroxy this adds network calls, distributed failure modes and several deployables to a self-hosted tool. It conflicts with NFR-05 (single binary). It also complicates the single egress IP per provider (§9.3), because every service that talks to a provider would need the same pinned egress.

**A different number of bounded contexts.** Fewer contexts were possible. Folding `provider` into `job` puts provider JSON, webhook translation and capability state into the core model; ADR 0003 rejects this. Folding `delivery` into `job` mixes byte streaming, stream caps and Signed Link keys with the Remote Item model, and gives `job` a second, unrelated rate of change. More contexts were also possible, for example splitting sync and outbox out of `job`. Both change the `RemoteItem` aggregate in the same transactions as submissions and claims (D-11), so a split would cut through one consistency boundary. Four contexts match the four languages listed in the Context.

**Multi-module repository with `go.work`.** One module per context would make boundaries visible to the Go toolchain. It brings version skew between modules, harder refactoring and extra release tooling, while `internal/` already hides packages. An import-graph test enforces the same boundaries inside one module (L-1).

**Layered or clean architecture without hexagonal naming.** Layers such as `handlers/services/repositories` hide the direction of a dependency. Explicit `ports/in` and `ports/out` show who owns an interface. They also make the rule "the consumer declares the port" (DEP-4) easy to review. ddd-rules L-2 requires one layout so that a reader finds a concept in the same place in every context.

**Plain application services without CQRS.** One service struct per context with mixed read and write methods is simpler at first. It mixes refresh-on-read queries with state changes and makes idempotent retries by caller-chosen IDs (A-3, DEC-38) a convention instead of a rule. It also prevents decorating every use case uniformly (A-5). Read models (R-8) let queries enforce visibility without loading aggregates.

## Compliance

- **Import-graph test** (`go test`, using `go list -deps -json` or `golang.org/x/tools/go/packages`) fails when:
  - a package under `internal/<A>` imports `internal/<B>` and the importer is not `internal/<A>/adapters/out/<B>`, or the imported package is not `internal/<B>/ports/in`;
  - an edge between contexts is not in the allowed set of rule 6 (kept as a list in the test);
  - any layer import breaks the table in ddd-rules §3 (DEP-1…DEP-3, DEP-7);
  - an `app/operation` package exists outside `provider`;
  - `cmd/...` imports anything except `<context>/service`, `internal/common/...` and the standard library.
- **Module check:** CI fails if the repository has more than one `go.mod` or any `go.work`.
- **Table ownership check:** a test parses `migrations/*.sql` and the sqlc query files. It fails when a migration touches tables of two contexts, when a migration file name does not match `NNNN_<context>_<change>.sql` with the owning context, when a context's queries name a table owned by another context, or when a foreign key references another context's table.
- **Review checklist:**
  - New cross-context needs start as an interface in the consumer's `ports/out`.
  - No domain type appears in another context's code or in `ports/in` DTOs of another context.
  - No `ports/in` DTO of `provider` carries a CDN URL (DEC-40).
  - New use cases are commands or queries (operations only in `provider`), one file each (A-1, A-2).
  - No transaction covers two contexts.
