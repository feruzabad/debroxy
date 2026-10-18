# 0020. Error bodies may carry a retry hint

| | |
|---|---|
| Status | Accepted (2026-10-13, project owner) |
| Date | 2026-10-13 |
| Deciders | project owner |
| Spec | revision 13, §10.3, changelog C13-02; decision DEC-55 |
| Related | amends ddd-rules IN-3 (error response body); ADR 0013 rules 12–18 (submit rate, forced refresh); DEC-05 (login throttling), DEC-18 (stream caps) |

## Context

- ddd-rules IN-3 maps every error once, in `internal/common/server/httperr.RespondWithSlugError`, and fixes the response body to exactly `{"slug": "...", "message": "..."}`.
- Four errors are limits that end at a known time:
  - `submit-rate-exceeded`: a rolling hour, ADR 0013 rule 12
  - `refresh-rate-exceeded`: a rolling minute, ADR 0013 rule 18
  - `login-throttled`: a doubling wait, DEC-05
  - `too-many-streams`: the stream caps, DEC-18

  debroxy knows when each one ends, or at least when to try again, but the body cannot say so. A client can only guess, and guessing early costs the user another failure, or a longer login wait.
- `provider-unavailable` sometimes carries the provider's own `Retry-After` (ADR 0013 rule 7). Today that value is dropped at the HTTP boundary.
- A browser client must show a countdown instead of "try again later".

## Decision

1. The error body is `{"slug": "...", "message": "...", "retryAfter": N}`. `retryAfter` is optional: a whole number of seconds, at least 1, rounded up. It is present only when debroxy knows a time after which the same request may succeed.
2. When `retryAfter` is present, the response also carries the header `Retry-After: N` (RFC 9110, delay-seconds).
3. The hint travels on the error itself. `internal/common/errors` gains an optional interface, `RetryAfterError { RetryAfter() time.Duration }`, which slug errors may implement. `RespondWithSlugError` stays the only place that writes error bodies (IN-3). It reads the hint through this interface, and it never derives a hint from the error type or the status.
4. **Required hints:**

   | Slug | Hint |
   |---|---|
   | `submit-rate-exceeded` | when the oldest counted submit of that class leaves the rolling hour |
   | `refresh-rate-exceeded` | when the oldest counted forced refresh leaves the rolling minute |
   | `login-throttled` | the remaining throttle wait for that username/IP |
   | `too-many-streams` | a fixed 5 s (streams end at unknown times) |
   | `provider-unavailable` | the provider's `Retry-After` or budget reset, when known; otherwise absent |

   Other errors carry no hint.
5. The IN-3 status table is unchanged; only the body description changes, from "exactly `{slug, message}`" to "`{slug, message}` plus the optional `retryAfter` of ADR 0020".

## Consequences

### Positive
- Clients can wait exactly as long as needed, and the UI can show a countdown.
- Mapping still happens in one place; contexts express the hint as data on their own errors.

### Negative
- Errors that implement the interface must compute the hint from the clock their handler already uses, so a few error types gain a field.
- The body is no longer a fixed two-field shape, so clients must ignore unknown fields. The OpenAPI `Error` schema lists `retryAfter`.

### Risks and mitigations
- *Risk:* a hint reveals something about other users, such as a global stream count. *Mitigation:* hints come only from limits that belong to the caller (own submit window, own forced refreshes, own username/IP throttle) or are fixed (`too-many-streams` is a constant 5 s).

## Alternatives considered

**Header only.** `Retry-After` without a body field. Browsers expose response headers to `fetch`, but every other error detail lives in the body, so clients would need two parsing paths. Rejected in favour of both.

**A new error type per limit with its own body.** This contradicts IN-3's single mapping and adds per-slug shapes.

## Compliance

- `httperr` tests cover a hinted error, an unhinted error and a non-slug error: the body has `retryAfter` and the header only in the first case.
- The handler tests of the four slugs check that the hint is present and correct under a fixed clock.
