# 0014. Amend the ddd-rules dependency table to match R-8 and S-1

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 8, §3.2 (context map), §5.4 (queries), §6.2, §7.2; decisions DEC-01, DEC-22, DEC-23, DEC-42 |
| Related | ADR 0001, ADR 0002; ddd-rules §2, §3 (DEP-1…DEP-7), R-8, S-1, S-3, §12.7, §12.8, §12.10 |

## Context

ddd-rules §3 lists the imports each layer may use, and states them as exhaustive ("→ means may import"). Other binding rules in the same file need imports that the table doesn't list:

- **R-8 and template §12.7.** Queries use read-model ports in `ports/out` that "return app-layer DTOs defined in `ports/in`", for example `JobsOfUser(ctx, userID) ([]in.JobSummary, error)`. A `ports/out` file then imports its own context's `ports/in`, but the table allows `ports/out → domain` only.
- **R-8 implemented by an adapter.** The SQLite read model that implements such a port builds `in.JobSummary` values, so `adapters/out` imports its own context's `ports/in`. The table allows `adapters/out → ports/out, domain`, and explicitly forbids "ports/in of own context".
- **S-1 and DEP-4 across contexts.** A consumer context reaches a supplier only through `adapters/out/<supplier>`, which calls the supplier's `ports/in.Application` (OUT-5). Under S-1, `cmd` builds each context through `service.NewApplication(ctx, cfg, deps)` and passes shared resources in `deps`. So `delivery/service` receives the `Application` of `job` and `provider` in its `deps` and hands them to its adapters. To declare that field, `delivery/service` must import the supplier's `ports/in`. The table allows `service → everything inside its own context` only.
- **S-1 and S-3 in `cmd`.** `cmd` parses configuration and creates the shared resources that `deps` carry: the database pools, and also the logger, the metrics client and the clock. Those come from `internal/common/decorator` and `internal/common/clock`, but the table allows `cmd → <context>/service, internal/common/server, internal/common/sqlite` only.
- **Layout.** The repository has an architecture test, `internal/archtest`, that enforces §2 and §3. It is not part of the §2 layout.

As written, R-8 and S-1 cannot both be followed without breaking §3. Without a decision, every read model and every composition root needs an ad-hoc exception.

## Decision

ddd-rules §3 gets four additional imports, and §2 lists the architecture test. The rest of §3 is unchanged: adapters never import `app` (DEP-2) or other adapters (DEP-3), the domain stays pure (DEP-1), and `internal/common` never imports a context (DEP-7).

1. **`ports/out → ports/in` of the same context.** A read-model port returns the DTOs that the query's `ports/in` file declares (R-8). Driven ports never import another context.
2. **`adapters/out → ports/in` of the same context**, only for the DTO types a read-model port returns. Driven adapters never call a handler of their own context. Only `adapters/in` drives the context.
3. **`service → ports/in` of another context**, only to declare the supplier `Application` types that a `deps` struct receives and passes to `adapters/out/<supplier>`. `service` calls no supplier handler itself. DEP-4 still holds for every other layer: the only code that *uses* a supplier is the consumer's `adapters/out/<supplier>`.
4. **`cmd → internal/common/*`.** `cmd` may import any shared-kernel package it needs to parse configuration and create shared resources. It still imports no `domain`, `ports`, `app` or `adapters` package of any context.
5. **Layout:** `internal/archtest` is the architecture test of ddd-rules §2 and §3. It contains no production code, and nothing imports it.

The updated table:

```
domain        → stdlib, internal/common/errors
ports/in      → domain, internal/common/decorator
ports/out     → domain, ports/in (DTOs returned by read models, R-8)
app           → ports/in, ports/out, domain, internal/common/*
adapters/in   → ports/in, internal/common/*            (NOT app, NOT ports/out)
adapters/out  → ports/out, domain, ports/in (read-model DTOs only), internal/common/*   (NOT app)
service       → everything inside its own context, internal/common/*, other contexts' ports/in (deps types only)
cmd           → <context>/service, internal/common/*
```

## Consequences

### Positive
- R-8, template §12.7 and S-1 can be followed literally, and the architecture test can enforce the whole table without exceptions.
- DTOs are declared once, in `ports/in`, and are not mirrored in `ports/out`.

### Negative
- `ports/in` changes now reach driven adapters too (read models). The coupling stays inside one context.
- `service` packages know the supplier `Application` types. That code is wiring only.

### Risks and mitigations
- *Risk:* a driven adapter starts calling its own context's handlers through `ports/in`. *Mitigation:* the code review rejects any use of `ports/in` in `adapters/out` other than DTO types. A handler call there also needs a reference to the `Application`, which only `service` holds.
- *Risk:* `service` grows logic around supplier calls. *Mitigation:* S-1 limits `service` to wiring, and code review rejects handler calls in `service`.

## Alternatives considered

**Keep the table, and declare read-model result types in `ports/out`.** Each query then has two identical DTO sets, one in `ports/out` for the adapter and one in `ports/in` for the driving side, plus a mapping in `app`. That doubles the types for every query, which R-8 and §12.7 explicitly avoid.

**Let `cmd` build the cross-context adapters.** `service` would receive ready-made `ports/out` implementations. `cmd` would then import every context's `adapters/out/<supplier>`, which breaks the rule that only `service` constructs concrete adapters (§1, composition root).

**Edit ddd-rules without an ADR.** ddd-rules §0 resolves conflicts "by an ADR that changes this file". The change widens what four layers may import, which is a binding rule change, not a typo.

## Compliance

- `internal/archtest` encodes the updated table. Its table tests cover every new import, and the restrictions that remain (adapters never import `app`, cross-context imports from layers other than `service` and `adapters/out/<supplier>`, `cmd` importing a context's inner packages).
- Code review: `ports/in` in `adapters/out` is used for DTO types only; `service` calls no supplier handler.
