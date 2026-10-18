# 0018. API paths use a sub-resource segment instead of colon methods

| | |
|---|---|
| Status | Accepted (2026-10-12, project owner) |
| Date | 2026-10-12 |
| Deciders | project owner |
| Spec | revision 9, §10.1; changelog C9-01 |
| Related | amends ADR 0009 rule 2 and ADR 0011 rule 13 (paths only); ddd-rules IN-1 |

## Context

Revisions 1–8 name three API actions with a colon suffix on the last path segment, in the style of "custom methods":

- `POST /api/v1/providers/{id}:refresh` (ADR 0009 rule 2),
- `POST /api/v1/admin/providers/{id}:probe-capabilities`,
- `POST /api/v1/jobs/{id}/links:all` (ADR 0011 rule 13).

ddd-rules IN-1 requires HTTP servers generated with oapi-codegen's `std-http-server` target, which registers routes on Go's `net/http.ServeMux`. A ServeMux wildcard must fill a whole path segment, so `{id}:refresh` is not a valid pattern: registering it panics at startup. `links:all` is a literal segment and would register, but it would be the only route of its kind.

Without a decision, either IN-1 needs an exception for hand-written routes, or the contract (the OpenAPI file) and the served URLs differ.

## Decision

1. Each colon action becomes its own path segment:
   - `POST /api/v1/providers/{id}/refresh` replaces `POST /api/v1/providers/{id}:refresh`;
   - `POST /api/v1/admin/providers/{id}/probe-capabilities` replaces `POST /api/v1/admin/providers/{id}:probe-capabilities`;
   - `POST /api/v1/jobs/{id}/links/all` replaces `POST /api/v1/jobs/{id}/links:all`.
2. This amends only the paths in ADR 0009 rule 2 and ADR 0011 rule 13. Their behaviour (MaxAge 0 and the per-user limit for forced refreshes; one link per file or one bundle link, in stored file order) is unchanged.
3. New API actions follow the same pattern: a verb-like sub-resource segment after the resource, never a colon suffix.

## Consequences

### Positive
- Every route is generated from the OpenAPI files (IN-1), with no special router and no exception.
- The contract and the served URLs are the same.

### Negative
- The URLs differ from revisions 1–8. No client exists yet, so nothing breaks.
- `/links/all` sits under the `links` collection, so a future `GET /jobs/{id}/links/{linkId}` could not use the ID `all`. Links have no IDs today.

### Risks and mitigations
- *Risk:* a later route reintroduces a colon suffix. *Mitigation:* rule 3, and ServeMux panics at startup on such a pattern, which every component test catches.

## Alternatives considered

**Keep the colon URLs with a rewriting middleware.** The OpenAPI file would document the slash form for code generation, and a middleware would rewrite `{segment}:action` to `{segment}/action` before routing. Rejected: the contract would no longer describe the URLs clients call.

**Hand-written handlers for the three routes.** Rejected: it needs an exception to IN-1 for routes that differ from the others only in punctuation.

## Compliance
- The OpenAPI files contain no path segment with a colon.
- Component tests start the real server, so an invalid pattern fails them at registration.
