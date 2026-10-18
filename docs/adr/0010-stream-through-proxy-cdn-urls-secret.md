# 0010. Stream every byte through debroxy; CDN URLs are secrets; Signed Links

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 7 (unchanged sections from revisions 5–6 apply), §1.2, §1.5 RISK-02/RISK-04/RISK-05, §3.2, §6.2 `OpenDeliveryStream`, §6.5, §7, §9.3, §9.4, §12 NFR-01/02/03 ; decisions DEC-17, DEC-18, DEC-30, DEC-40 (with DEC-14, DEC-26) ; changes C5-03, C5-08, C5-11 |
| Related | ADR 0003, ADR 0011, ADR 0012, ADR 0013; ddd-rules A-10, OUT-4, OUT-6, IN-2, IN-3, DEP-4, DEP-5, R-9, D-10; spec changes C-02, C-16; evidence TorBox T07, T08, T13, TB-N01, TB-N02, torbox.md §5 (zip Range); Real-Debrid R05, R14, RD-N02, RD-N07 |

Revised 2026-10-11 for revision 5.

## Context
Users download files that live on the provider's CDN. The provider account is shared, and providers watch for account sharing such as many IPs on one account (§1.1).

Observed provider behaviour:
- **TorBox CDN URLs embed the account API key** as `?token=<API KEY>` (`…/dld/<uuid>?token=…`, `…/zip/<uuid>?token=…`). `requestdl` itself needs the key as a `token` query parameter (torbox.md T07, TB-N01; fixtures `04-requestdl-token-param.http`, `04-requestdl-zip.http`). Whoever holds such a URL holds full account access.
- CDN links are **not IP-bound** at either provider and **keep working after the item is deleted** (TorBox T08, TB-N02; Real-Debrid R14, RD-N02). They cannot be revoked by debroxy.
- TorBox `user_ip` only changed the CDN region (T08; `04-requestdl-userip.http`).
- TorBox file URLs honour Range (206, `ETag`) (`04-cdn-range-1mib.http`). The TorBox **zip URL advertises `Accept-Ranges: bytes` but returns 200 with the full body** for a ranged GET (torbox.md §5; `25-cdn-sintel-zip-range.http`).
- Real-Debrid CDN honours Range but sends `Content-Disposition` **without a filename** and **no `ETag`/`Last-Modified`** (RD-N07; `04-cdn-head.http`, `04-cdn-range-1mib.http`).
- Only the TorBox zip CDN reports a rate limit (60 per 60 s, `04-cdn-zip-head.http`). TorBox file downloads and Real-Debrid CDN downloads carry no rate-limit headers (torbox.md T13, realdebrid.md R11).
- A TorBox CDN link stayed valid for at least 27 min (T08, ASM-T24).

Forces: the single-IP goal (NFR-02) covers file bytes too. The pinned egress, the User-Agent and the rate budgets belong to the provider account inside `provider` (OUT-4, ADR 0012, ADR 0013); a second HTTP client in `delivery` would split them. A Signed Link is a bearer capability (RISK-04). Users must be able to paste a link into a download manager or player that cannot send headers. Streams consume proxy bandwidth and provider connection limits (RISK-02, RISK-03).

## Decision
debroxy streams every byte through itself. `provider` performs the upstream CDN request; CDN URLs never leave `provider`. Users get Signed Links.

**Who does what** (DEC-40, C5-03)
1. `provider` exposes the operation `OpenDeliveryStream` (§6.2; an operation that returns a stream, A-10). Input: provider ID, kind, provider item ID, target, method (`GET` or `HEAD`), and the client's `Range`/`If-Range`. Output: `DeliveryStream{Status, Headers (whitelisted), RangeSupported, Body io.ReadCloser}` or a neutral error. The caller closes `Body`.
2. `delivery` reaches it through its consumer port `DeliveryStreamOpener` (DEP-4). `delivery` receives only the stream and neutral values. It never sees a CDN URL, a provider host name or an HTTP client.
3. Inside `OpenDeliveryStream`, `provider`:
   - resolves the target to a CDN link (TorBox `requestdl` with `file_id` or `zip_link=true`; Real-Debrid unrestrict of the item's link; ADR 0011), or takes it from the CDN-link cache;
   - sends the upstream request through the account's HTTP client: pinned egress (DEC-26, ADR 0012), the account's User-Agent (DEC-14), and the account's rate budgets (rule 4);
   - forwards `Range`/`If-Range` only when the target supports ranges;
   - on upstream 401/403/404/410 **before the first byte**, drops the cache entry, resolves once more and retries once; otherwise returns `provider-unavailable`.
4. **Budgets** (ADR 0013, C5-08). Resolve calls are API calls and take tokens from the `general` class (TorBox per endpoint; Real-Debrid whole API). A TorBox **zip** upstream request takes a token from the `cdnZip` class (60 per 60 s). CDN **file** downloads at either provider have no rate class; the stream caps bound them (rule 15, DEC-18).
5. TorBox `user_ip` is never sent (resolves OQ-05).

**CDN URL secrecy** (DEC-30, OUT-6, §7.4)
6. A CDN URL exists only inside `provider`'s driven adapters: in the CDN-link cache and during an upstream request. No port type (`provider/ports/in`, `provider/ports/out`, any consumer port) carries it.
7. `CDNLink` is an opaque type in the provider adapter layer, not in any port. Its raw URL is an unexported field. The only code that reads it is the adapter function that builds the upstream HTTP request, in the same package; there is no exported accessor. `String()`, `GoString()`, `MarshalJSON()` and `LogValue()` return a redacted form (scheme and host only), as defence in depth. The previous `ExpiresHint` field is dropped: no rule used it, and the cache lifetime is the fixed `cdnLinkCacheMax`.
8. The CDN-link cache is per Provider Account and target, in memory, with entries kept at most 30 min (`cdnLinkCacheMax`, ASM-T24). It is never persisted, dumped, exported as metrics, or logged.
9. Provider adapters redact URLs in logs to scheme and host, including the `requestdl` request URL that carries the key. Errors from the HTTP client (Go's `*url.Error` prints the full URL) are rewrapped without the URL before they leave the adapter.
10. If a CDN URL may have leaked, the **only** revocation is rotating the provider API key. The operator documentation describes this. Deleting the item does not help (TB-N02, RD-N02).

**Signed Links** (DEC-17)
11. Format: `<publicBaseURL>/dl/<token>/<url-escaped name>`, `token = base64url(payload) "." base64url(HMAC-SHA256(key[kid], payload))`, payload `{v, kid, uid, jid, tgt, exp}`, `tgt` = `f:<FileID>` or `b`. The link carries no provider data.
12. Keys are a configured list. The first key signs; all keys verify. Removing a key revokes every link signed with it. TTL is configurable, default 6 h (OQ-03).
13. **The `<name>` segment is outside the signature** (C5-11). It is cosmetic and untrusted: anyone holding the link can change it. debroxy uses it only for `Content-Disposition`, after sanitizing it (control characters, path separators and quotes removed). If it is empty after sanitizing, debroxy uses the target's stored name (the file's name, or the item name for a bundle). It never selects the target.

**Request flow** (`GET`/`HEAD /dl/{token}/{name}`, §7.3)
14. Every request, in this order: verify the token with a constant-time HMAC comparison, a known `kid` and `exp` (403 `invalid-signed-link` / `signed-link-expired`); check the user is active (`UserStatusChecker`); take a stream slot (rule 15); check the job is unreleased, owned by `uid`, `ready`, and the target available (`JobTargets.DeliveryTarget`, 404/409); call `OpenDeliveryStream` (errors → 502 `provider-unavailable`). Nothing is cached between requests in `delivery`.
15. **Stream caps** (DEC-18): at most 4 concurrent streams per user and 64 in total (configurable). Over a cap → 503 `too-many-streams`. A slot is held from rule 14 until the response ends. These caps are the only bound on CDN file downloads.
16. **HEAD** runs the same checks and takes a stream slot for the duration of the request. `provider` sends an upstream `HEAD` (a zip HEAD takes a `cdnZip` token; resolving may take a `general` token). debroxy returns the status and whitelisted headers without a body, then releases the slot.

**Headers and Range**
17. Upstream, `provider` sends only the account's own headers (User-Agent) plus `Range` and `If-Range` **only when the target supports ranges**. No client header (cookies, authorization, forwarding headers) is passed upstream.
18. To the client, debroxy copies the status (200/206/416) and only `Content-Length`, `Content-Range`, `Content-Type`, `Last-Modified`, `ETag`. It sets `Accept-Ranges` from `DeliveryStream.RangeSupported`, never from upstream. It always sets `Content-Disposition: attachment; filename*=UTF-8''<name>` (rule 13).
19. For a target without Range support (TorBox zip), a client `Range` is ignored: 200, full body, `Accept-Ranges: none` (C-16).
20. **One mechanism for an assumed-range target that answers without range** (Real-Debrid RAR, ASM-R15; ADR 0011): if the upstream answers a ranged request with 200, `OpenDeliveryStream` returns status 200 with `RangeSupported = false`, and debroxy sends the full body with `Accept-Ranges: none`. A correct HTTP client treats a 200 as the whole resource.
21. Bytes are copied through a bounded buffer. Nothing is written to disk (NFR-01). A client disconnect closes `Body`, which cancels the upstream request. After the first byte, an upstream error ends the response.
22. Access logs never contain the token segment of `/dl/*` (NFR-03).

## Consequences
### Positive
- The API key never reaches a user, a browser history or a log, even though TorBox puts it in the URL.
- Provider traffic, including file bytes, leaves from one IP per provider, through one client per account; egress, User-Agent, budgets and the link cache live in one context.
- debroxy can revoke access at any time: disable the user, release the job, or rotate the signing key.
- Range behaviour is honest per target; clients do not get broken resumes on TorBox zips.

### Negative
- All download bandwidth passes through the proxy host (RISK-03).
- No CDN-side resume on targets without Range support.
- Real-Debrid sends no `ETag` or `Last-Modified` (RD-N07), so a client cannot send a validating `If-Range` for Real-Debrid targets. A resumed download relies on a plain `Range` and cannot detect that the upstream file changed.
- Each stream holds a connection on both sides for its whole duration.
- `provider` now carries byte streams, not only API calls; its operation decorators must not buffer or log bodies.
- The name in a link can be changed by its holder, so a downloaded file can carry a misleading name.

### Risks and mitigations
- **CDN URL leak through a code path** (panic dump, wrapped error, debug log). Mitigation: no port carries the URL, unexported field with no accessor, self-redacting methods, adapter redaction, canary log-redaction tests (Compliance).
- **Leaked Signed Link** works until expiry. Mitigation: short TTL, per-request ownership and user status checks, stream caps, key rotation.
- **Link outlives the cache** and the CDN URL expires mid-cache. Mitigation: rule 3 re-resolves once before the first byte.
- **Many concurrent file downloads** trip an unknown provider connection limit (no headers). Mitigation: stream caps (RISK-02); the caps are configurable.
- **Real-Debrid RAR Range** is assumed (ASM-R15). Mitigation: rule 20 at runtime; discovery top-up (§15) to fix the static flag.

## Alternatives considered
**Redirect users to CDN URLs.** Cheapest for the proxy. At TorBox it hands every user the account API key (TB-N01). Links cannot be revoked (TB-N02, RD-N02), and downloads would come from many user IPs, against the single-IP goal. Rejected.

**Proxy-issued short CDN redirects.** debroxy checks a short-lived token, then redirects to the CDN. The client still receives the CDN URL, and debroxy's short TTL does not bound it: CDN links stay valid for at least 27 min and survive deletion. Same leak and IP problems as above.

**`delivery` performs the upstream request with a resolved link** (revision 4: `ResolveCDNLink` returned the link to `delivery`). The CDN URL would cross a context boundary, and `delivery` would need its own client with the account's egress, User-Agent and zip budget, duplicating `provider`'s (OUT-4). Rejected (DEC-40).

**Sign the name into the link.** Prevents renaming, but the name is cosmetic and the target is already signed; a tampered name harms only the link holder. Kept outside the signature (§7.1).

**Bearer-token-only download endpoint.** Downloads require the session token in `Authorization`. Download managers and players often cannot send headers, so users would put the session token into URLs, exposing a credential with full account scope. A Signed Link is scoped to one target, one user and a short TTL.

## Compliance
- Log-redaction test: run stream flows against fixtures whose CDN URLs and keys carry a canary value; assert the canary is absent from captured slog output, error strings, metric labels and HTTP responses.
- Type tests for `CDNLink`: `%v`, `%+v`, `%#v`, `json.Marshal`, `slog` all redact.
- Import/visibility check: no type in `provider/ports/*`, `delivery` or `job` holds a CDN URL; `CDNLink` has no exported method returning the raw URL.
- Adapter test: a transport error on `requestdl` produces an error without the URL or key.
- Header whitelist test: an `httptest` upstream records request headers; only User-Agent and (when supported) `Range`/`If-Range` arrive. Response headers outside the whitelist are dropped.
- Range tests: `25-cdn-sintel-zip-range.http` → client `Range` on a zip gives 200, full body, `Accept-Ranges: none`; `04-cdn-range-1mib.http` → 206 passes through; an assumed-range target answering 200 → 200 with `Accept-Ranges: none`.
- Budget tests: a zip stream takes a `cdnZip` token (`04-cdn-zip-head.http` updates the bucket); a file stream takes none; re-resolve takes a `general` token.
- Re-resolve test: upstream 403 before the first byte → one re-resolve and one retry; a second 403 → 502.
- HEAD test: no body, whitelisted headers, slot released afterwards.
- Name test: a tampered or hostile name segment is sanitized; an empty one falls back to the stored name; the target is unchanged.
- TorBox adapter test: `requestdl` requests never contain `user_ip`.
- Signed Link tests: tampered payload, wrong `kid`, removed key, expired, disabled user, released job, foreign job.
- Stream cap tests: the fifth stream of one user and the 65th overall get 503.
- Review checklist: no `http.Redirect` in `delivery`; no HTTP client in `delivery`; operation decorators for `OpenDeliveryStream` do not read the body.
