# 0017. Shared outbound HTTP client in `internal/common/httpclient`

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 8, §6.1 (`httpProfile`, `egress`, `rateBudgets`), §6.5, §7.4, §9.3, §9.4, NFR-03, NFR-09; decisions DEC-14, DEC-26, DEC-30 |
| Related | ADR 0003 (rules 3, 11, 12), ADR 0012, ADR 0013; ddd-rules §2, DEP-3, DEP-7, OUT-4, OUT-6 |

## Context

- Two provider adapters exist: `provider/adapters/out/torbox` and `provider/adapters/out/realdebrid` (ADR 0003 rule 3). Each owns one HTTP client per Provider Account (OUT-4, ADR 0003 rule 11).
- Every one of those clients needs the same technical machinery:
  - the pinned-egress dialer (`tcp4`/`tcp6`, optional local address, no proxy) of ADR 0012;
  - the configured User-Agent on every request (DEC-14);
  - retries with back-off for idempotent calls, and a circuit breaker (NFR-09);
  - rate buckets that count calls, wait for a reset, run single-flight until the first `x-ratelimit-*` headers arrive and start header-less buckets at 50 % (ADR 0013 rules 1, 6, 7, 9);
  - a self-redacting type for URLs that carry secrets, such as CDN links (DEC-30) and TorBox `requestdl` URLs with the API key in the query (§6.5);
  - redaction of request URLs in transport errors (OUT-6, NFR-03).
- DEP-3 forbids one adapter from importing another, so the two adapters cannot share a package between them.
- None of this machinery is about debrid providers. It is how debroxy talks HTTP to any external system with a pinned egress and a rate limit.

## Decision

1. A new shared-kernel package, `internal/common/httpclient`, provides the outbound HTTP machinery listed above. It holds **no business rules** (DEP-7). It knows HTTP, `x-ratelimit-*` headers, time and network families. It does not know providers, operation classes or debrid concepts.
2. **The caller decides which bucket a call uses.** Buckets are keyed by names the caller chooses (a class and an endpoint). The rules that are about TorBox and Real-Debrid stay in their adapters:
   - which class a call is charged to (for example, a torrent create goes to `uncachedCreate` unless the hash was reported cached);
   - which reported limit selects which class;
   - the static budget values.
3. **Secret URLs.** `httpclient.SecretURL` is an opaque URL whose `String`, `GoString`, `LogValue`, `MarshalJSON` and `MarshalText` return only scheme and host. Only the client's own request building reads the raw URL. The provider adapters use it as DEC-30's `CDNLink` (ADR 0003 rule 12). Transport errors returned by the client carry the redacted form, never the raw URL.
4. **One client per Provider Account** remains the rule (OUT-4). The package builds a client from a configuration; it holds no package-level client, limiter or state. Each adapter creates one per account.
5. ddd-rules §2 lists `httpclient/` among the `internal/common` packages: "outbound HTTP client: pinned egress, User-Agent, retry, circuit breaker, rate buckets, secret URLs (ADR 0017); no business rules".

## Consequences

### Positive
- One tested implementation of the egress pinning, budget and redaction rules for both adapters. Bugs in this security-relevant code are fixed in one place.
- ADR 0003's adapter paths and DEP-3 stay unchanged.

### Negative
- The shared kernel grows by one package, and every adapter that uses it depends on its API.

### Risks and mitigations
- *Risk:* provider rules leak into the package (for example a TorBox class name). *Mitigation:* DEP-7 and code review. The package's tests use made-up bucket names, and the archtest forbids `internal/common` from importing any context.
- *Risk:* another context starts calling external systems through it, bypassing `provider`. *Mitigation:* ADR 0003 rule 11 still says only `provider` opens connections to provider and CDN hosts. Code review checks new imports of the package.

## Alternatives considered

**One adapter tree** (`provider/adapters/out/debrid/{httpclient,torbox,realdebrid}`). Its subpackages may import each other under DEP-3, so nothing new enters the shared kernel. It changes the adapter paths that ADR 0003 rule 3 names and needs a superseding ADR. It also hides a generic client inside one context, where a second consumer, such as an OIDC discovery client with pinned egress, could not reuse it.

**Duplicate per adapter**, with the pure bucket and breaker logic in `provider` domain packages. No rule changes, but about 150 lines of wiring would be repeated, and technical retry and breaker code would sit in the domain, which is meant to hold the business language (DEP-1, L-3).

## Compliance

- `internal/archtest` accepts `internal/common/httpclient` and keeps forbidding it from importing any context (DEP-7).
- Tests in the package cover egress family pinning, proxy bypass, User-Agent, retry only for idempotent calls, the circuit breaker, every bucket rule of ADR 0013 (waits, `RetryAfter`, header updates, ignored invalid headers, single-flight start, 50 % start) and the redaction of secret URLs in errors.
