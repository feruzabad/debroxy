# Architecture Decision Records

ADRs record decisions that are expensive to reverse. Source of truth for the decisions: [`docs/specs/revision14.md`](../specs/revision14.md) §13 (the ADRs cite revision 7, where they were accepted).

## Rules

- One decision per file: `NNNN-kebab-case-title.md`, numbered in order and never reused.
- Status values: `Proposed` → `Accepted` → (`Superseded by NNNN` | `Deprecated`). An accepted ADR is never edited except for its status line; changes need a new ADR that supersedes it.
- Each ADR cites the spec decision IDs (`DEC-*`), rules (`ddd-rules.md` IDs) and discovery evidence it rests on.
- A spec revision may not contradict an accepted ADR.

## Index

| ADR | Title | Spec decisions | Status |
|---|---|---|---|
| [0001](0001-modular-monolith-hexagonal-ddd-cqrs.md) | Modular monolith with hexagonal architecture, DDD and CQRS | DEC-01, DEC-13, DEC-22, DEC-23, DEC-42 (migrations) | Accepted |
| [0002](0002-embedded-sqlite-single-writer.md) | Embedded SQLite as the only store, relying on single-writer serialization | ASM-S01, DEC-08 (storage part), DEC-13, DEC-42 | Accepted |
| [0003](0003-provider-context-as-anti-corruption-layer.md) | Provider context as an anti-corruption layer exposing operations, with learned capability state | DEC-02, DEC-21, DEC-40, C-03, C-14 | Accepted |
| [0004](0004-claims-inside-remote-item-aggregate.md) | Jobs are entities inside the RemoteItem aggregate; a claim is their relation to it | DEC-06, DEC-07 | Accepted |
| [0005](0005-submission-as-remote-item-state.md) | A submission is a RemoteItem state (`submitting`) | DEC-27, DEC-32, DEC-33, DEC-36, DEC-38, DEC-39, DEC-43 | Accepted |
| [0006](0006-slot-charge-model.md) | Slot charge model: charge holder, then all claimants | DEC-08, DEC-28 | Accepted |
| [0007](0007-uniqueness-among-claimable-items.md) | Uniqueness only among claimable items; deferral instead of cross-aggregate revive | DEC-31, DEC-39, DEC-44 | Accepted |
| [0008](0008-transactional-outbox-verified-deletion.md) | Transactional outbox with verified provider deletion | DEC-20, DEC-37, DEC-25 | Accepted |
| [0009](0009-refresh-on-read-webhooks-as-hints.md) | Refresh-on-read with webhooks as refresh hints and one refresh pipeline | DEC-09, DEC-09b, DEC-10, DEC-11, DEC-12, DEC-34, DEC-41 | Accepted; rule 2 path amended by 0018, rule 18 by 0019, refresh causes by 0021 |
| [0010](0010-stream-through-proxy-cdn-urls-secret.md) | Stream every byte through debroxy; CDN URLs are secrets; Signed Links | DEC-17, DEC-18, DEC-30, DEC-40 | Accepted |
| [0011](0011-delivery-targets-file-and-bundle.md) | Delivery targets: file and bundle | DEC-29, DEC-15 | Accepted; rule 13 path amended by 0018 |
| [0012](0012-egress-pinning-per-provider.md) | Egress pinned per provider account | DEC-26, DEC-14 | Accepted |
| [0013](0013-rate-budgets-and-submit-limits.md) | Provider rate budgets and per-user submit limits | DEC-24, DEC-43, C4-12 | Accepted |
| [0014](0014-dependency-table-amendment.md) | Amend the ddd-rules dependency table to match R-8 and S-1 | DEC-01, DEC-22, DEC-42 | Accepted |
| [0015](0015-embedded-migrations-package.md) | Embed the migrations through a `migrations` package | DEC-13, DEC-42 | Accepted |
| [0016](0016-gateway-dtos-from-ports-in.md) | Gateways may return the DTOs declared in ports/in | DEC-02 | Accepted |
| [0017](0017-common-outbound-http-client.md) | Shared outbound HTTP client in `internal/common/httpclient` | DEC-14, DEC-26, DEC-30 | Accepted |
| [0018](0018-api-paths-without-colon-methods.md) | API paths use a sub-resource segment instead of colon methods | C9-01 | Accepted |
| [0019](0019-tracked-failed-observations-are-not-leftovers.md) | Observations of tracked failed or gone items are not leftovers | C10-04, DEC-10 | Accepted |
| [0020](0020-retry-hints-in-error-bodies.md) | Error bodies may carry a retry hint | DEC-55 | Accepted; amends ddd-rules IN-3 |
| [0021](0021-event-stream-and-watch-refreshes.md) | Event stream with a change feed and `watch` refreshes | DEC-54 | Accepted; amends ADR 0009 |
| [0022](0022-embedded-web-assets-package.md) | Embed the web client through a `web` package | DEC-57 | Accepted; amends ddd-rules §2, §3 |

## Template

```markdown
# NNNN. <Title in sentence case>

| | |
|---|---|
| Status | Proposed |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 4, §… ; decisions DEC-… |
| Related | ADR …; ddd-rules …; evidence … |

## Context
The forces at play: the problem, constraints, observed provider behaviour (cite discovery docs), and what goes wrong without a decision. Facts only; no solution yet.

## Decision
The decision in active voice ("debroxy does X"). Precise enough to implement and to review code against. Use numbered rules when there are several.

## Consequences
### Positive
### Negative
### Risks and mitigations

## Alternatives considered
For each: what it is, why it was rejected (one short paragraph each).

## Compliance
How to check that code follows this ADR (tests, lint rules, review checklist items).
```
