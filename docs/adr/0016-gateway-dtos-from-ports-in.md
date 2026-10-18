# 0016. Gateways may return the DTOs declared in ports/in

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 8, §6.2 (operations), §5.6 (Observation) |
| Related | ADR 0003 (rule 3), ADR 0014 (rule 1); ddd-rules §3, R-8, R-9, A-10, OUT-3 |

## Context

- `provider` is an anti-corruption layer that exposes operations (A-10, ADR 0003). Most operations pass neutral data straight through: the provider adapter translates the provider's JSON into an `Observation`, a listing with its `Complete` flag, a cached-availability answer or a hoster catalogue (ADR 0003 rule 3), and the operation returns that value to `job`.
- These neutral types are declared in `provider/ports/in`, because consumers receive them from the operations.
- The adapter implements a driven port in `provider/ports/out` (the gateway to one provider's HTTP API, R-9). That port must return the same neutral values.
- ADR 0014 rule 1 lets `ports/out` import `ports/in` of the same context only for "DTOs returned by read models (R-8)". A gateway is not a read model, so the gateway port would need its own identical set of types, and `app/operation` would copy every field from one set into the other. ADR 0014 rejected exactly that duplication for read models.

## Decision

1. ADR 0014 rule 1 is widened. `ports/out` may import `ports/in` of the same context for the DTOs that its **read models and gateways** return. The DTOs stay declared once, in `ports/in`.
2. ADR 0014 rule 2 is widened the same way. `adapters/out` may use those `ports/in` DTOs when it implements a read model or a gateway. A driven adapter still never calls a handler or an operation of its own context.
3. Gateway ports keep provider-neutral names and shapes (R-9, OUT-3). Provider JSON never appears in a DTO.
4. ddd-rules §3 changes: the `ports/out` and `adapters/out` rows read "ports/in (DTOs returned by read models and gateways)".

## Consequences

### Positive
- One set of neutral provider types. The operations return what the adapter produced, and the mapping code holds only decisions, not field copies.

### Negative
- A change to a `ports/in` DTO now reaches the provider adapters too. The coupling stays inside one context.

### Risks and mitigations
- *Risk:* adapters start using `ports/in` for things other than DTOs, such as calling operations. *Mitigation:* a driven adapter has no reference to the context's `Application`, and code review rejects any use of `ports/in` in `adapters/out` other than DTO types (as in ADR 0014).

## Alternatives considered

**Duplicate the types in `ports/out`.** The gateway declares its own identical structs and `app/operation` copies them field by field. That means about 150 lines of copy code for no difference in meaning, and the same structural duplication ADR 0014 rejected for read models.

**Declare the neutral types in `provider/domain`.** `ports/in`, `ports/out` and the adapters may all import the domain. But these types are not an aggregate, and ddd-rules L-3 keeps one aggregate per domain package. A value-object-only package for pass-through data would bend L-3 instead.

## Compliance

- `internal/archtest` already allows the imports (ADR 0014). This ADR widens their permitted purpose.
- Code review: `ports/in` in `adapters/out` is used for DTO types only.
