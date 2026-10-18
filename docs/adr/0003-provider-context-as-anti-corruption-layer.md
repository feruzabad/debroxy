# 0003. Provider context as an anti-corruption layer exposing operations, with learned capability state

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 7 (unchanged sections from revisions 5–6 apply), §3, §5.5 step 2, §5.6, §5.7, §6 (6.1–6.5), §7.3 step 5, §7.4, §8, §9.1, §9.2, §9.3, §12 (NFR-03, NFR-10, NFR-11); changelog C-03, C-14, C5-03, C5-05, C5-11; decisions DEC-02, DEC-14, DEC-15, DEC-16, DEC-21, DEC-26, DEC-30, DEC-40 |
| Related | ADR 0001, ADR 0008, ADR 0009, ADR 0010, ADR 0012, ADR 0013; ddd-rules A-10, DEP-4, DEP-5, R-9, OUT-3, OUT-4, OUT-6, T-5; evidence TB-N03, TB-N04, TB-N14, RD-N06, RD-N08, torbox.md T02, T10, T11, T19, realdebrid.md R03, R08 |

Revised 2026-10-11 for revision 5.

## Context

debroxy talks to two providers whose APIs differ in shape, error style and features.

- **Errors do not follow HTTP status.** TorBox returns business errors such as `PLAN_RESTRICTED_FEATURE` and `UNSUPPORTED_SITE` as HTTP 500. It reports not-found as 404 with `success:true` and `error:"ITEM_NOT_FOUND"`. Auth and validation errors are FastAPI bodies without the envelope. A repeated delete returns 500 with `error` and `detail` swapped (torbox.md T02, TB-N04). Real-Debrid always returns `{error, error_code}`, with codes 7, 8, 16, 24 and 37 observed (realdebrid.md RD-N08).
- **Features depend on the plan, and no API lists them.** On TorBox plan 1, `createusenetdownload` fails with 500 `PLAN_RESTRICTED_FEATURE` (torbox.md T10, TB-N03). `user/me` has no capability list (torbox.md T19). The only way to learn a plan feature is to call it.
- **Features differ by provider.** Real-Debrid's cached check is a `disabled_endpoint` (realdebrid.md R08). TorBox accepts web downloads only for hosters in `webdl/hosters` (torbox.md T11). Real-Debrid rejects unknown hosters with `hoster_unsupported` (RD-N06).
- **Identifiers and steps differ.** TorBox uses `torrent_id`, `web_id` or `webdl_id` depending on kind and endpoint (TB-N14). Real-Debrid needs a file-selection call after every add (realdebrid.md R03).

`job` and `delivery` must stay provider-neutral (DEP-5). Both need the provider: `job` for creates, listings, deletes and webhook translation; `delivery` for the bytes of a file or bundle. All of these calls must use one rate budget, one User-Agent and one pinned egress per account (DEC-14, DEC-26, ADR 0012, ADR 0013). CDN URLs are secrets (DEC-30).

`provider` holds no domain state of its own. Forcing commands, queries and read models onto it would add IDs and read models with no domain meaning. Learned capability state, however, must survive a restart. Otherwise the first plan-restricted submit after every restart hits the provider again. ddd-rules A-10 allows operations for such a context and explicitly allows rebuildable caches of learned facts.

## Decision

1. **`provider` is the anti-corruption layer** between debroxy and the providers. It is the only context that knows provider URLs, JSON, error codes, ID parameter names, provider-specific steps and CDN URLs.
2. **Operations, not CQRS** (A-10, DEC-02). `provider/ports/in` declares operations `<Verb><Noun>` as `decorator.OperationHandler[Request, Result]`. They are implemented in `provider/app/operation` and decorated with `ApplyOperationDecorators`. The set is §6.2: `ListProviders`, `HosterCatalogue`, `CreateRemoteItem`, `ListRemoteItems`, `DescribeRemoteItem`, `DeleteRemoteItem`, `OpenDeliveryStream`, `CheckCached`, `TranslateWebhook`, `ProbeCapabilities`. There is no operation that returns a CDN URL. Consumers reach the operations only through their own ports: `job` through `ProviderGateway`, `delivery` through `DeliveryStreamOpener` (DEP-4, DEC-40).
3. **Provider JSON never leaves the adapter.** Provider structs are unexported in `provider/adapters/out/torbox` and `…/realdebrid`. Responses are translated at once into neutral `ports/in` types (OUT-3, R-9):
   - `Observation`, including its file list. The file entries are a `provider` DTO; `job` translates them into its own `RemoteFile` value (DEP-5);
   - the listing result with its `Complete` flag (C5-05);
   - `DeliveryStream`;
   - `WebhookSignal`.
   Native states map to Lifecycle states through one table per provider (§8). Provider-specific steps stay inside the adapter: Real-Debrid file selection (DEC-15), Real-Debrid unrestrict, TorBox per-kind endpoints and ID names.
4. **Errors are classified by provider code, never by HTTP status alone** (C-14). Each adapter first parses the body as envelope, FastAPI body or Real-Debrid error. It then maps the code to the neutral results of §6.4: `provider-unavailable`, `capability-unavailable-for-plan`, `unsupported-hoster`, `not-found`, `provider-rejected`, or internal error. HTTP status decides only when no body can be classified: 401 without envelope, 429, 5xx without a known code. Unknown codes become `provider-rejected` with sanitized detail and are logged once per value (NFR-11).
5. **`DeleteRemoteItem` returns `deleted` or an error** (§5.7, C5-11). Only a success answer counts as deleted without a look-up. On any error, including not-found and TorBox's swapped `DATABASE_ERROR` ("verify with lookup" in §6.4), the operation looks the item up itself. Not-found → `deleted`. Still present, or the look-up fails → an error, and the outbox retries. ADR 0008 rule 5 records the details.
6. **`OpenDeliveryStream` performs the CDN upstream request** (DEC-40, C5-03). Input: provider ID, kind, provider item ID, target, method (`GET`/`HEAD`) and the client's `Range`/`If-Range`. Output: `DeliveryStream{Status, Headers, RangeSupported, Body io.ReadCloser}` or a neutral error; the caller closes `Body` (A-10). Inside the operation `provider`:
   - resolves the CDN link (TorBox `requestdl`, Real-Debrid unrestrict) and caches it in memory for at most `cdnLinkCacheMax` (default 30 min, ASM-T24);
   - sends the request through the account's client (rule 11): pinned egress, User-Agent and rate budget;
   - forwards `Range`/`If-Range` only when the target supports ranges;
   - re-resolves the link once on 401/403/404/410 before the first byte;
   - returns only the whitelisted headers of §7.3 step 7.
   Stream caps, `Content-Disposition` and copying to the client stay in `delivery` (ADR 0010).
7. **Kind capabilities are static per provider kind** (C-03). The table of §6.3 is code in the adapter. A submit is checked in this order, and the first match decides (§6.2, C5-11):
   1. the provider kind lacks the capability → `capability-not-supported` (501);
   2. the provider has it, but debroxy v1 does not enable it (`SubmitNZBURL`, Real-Debrid `SubmitWebURL`, DEC-16) → `capability-not-enabled` (501). The account state is not consulted, whatever it is;
   3. the account state is `unavailable` → `capability-unavailable-for-plan` (409);
   4. otherwise (`available` or `unverified`) the call proceeds.
8. **Account capability state is learned at runtime** (C-03). It applies only to capabilities that pass steps 1 and 2 of rule 7.
   - Values: `available{since}`, `unavailable{reason, since}`, `unverified`.
   - Initial value: `unverified` for `SubmitNZBFile` and `SubmitNZBURL`; `available` for all others, including TorBox `SubmitWebURL`. Stored rows override the initial value at startup.
   - A provider answer meaning "the plan does not allow this" (TorBox `PLAN_RESTRICTED_FEATURE`) sets `unavailable{plan}`. A successful call sets `available`.
   - `unverified` capabilities can be used. The API shows them as `unverified`.
   - While `unavailable` is younger than 24 h, requests are rejected locally and never reach the provider. After 24 h, or on the admin operation `ProbeCapabilities`, one call may test it again.
9. **Capability state is the only persisted provider data** (DEC-21). It lives in the `provider`-owned table `provider_capability_state` (`provider_id`, `capability`, `state`, `reason`, `since`). The table is a rebuildable cache of learned facts, as A-10 allows. Deleting it loses no correctness, only one extra provider call per capability. Rate budgets, hoster catalogues, the CDN-link cache and HTTP state stay in memory.
10. **Hoster catalogue.** The adapter loads the provider's hoster list (TorBox `webdl/hosters`, Real-Debrid `hosts/domains`), keeps it in memory and refreshes it every hour. `job` validates web URLs against it through `ProviderGateway` before any create (`unsupported-hoster`, §5.5 step 2).
11. **One HTTP client per Provider Account** inside the adapter, with base URL, User-Agent, timeout, budgets, retry, circuit breaker and pinned egress (OUT-4, ADR 0012). Every operation for that account uses it, including `OpenDeliveryStream`. No other context opens connections to a provider or CDN host.
12. **Secrets stay inside.** API keys and CDN URLs never appear in errors, logs, metrics or neutral results (OUT-6, NFR-03). Inside `provider`, the CDN link is the opaque, self-redacting `CDNLink` type (DEC-30); only the adapter's HTTP call reads its raw URL (§7.4).

## Consequences

### Positive

- Provider changes touch one adapter and its fixtures, not the core (RISK-06).
- `job` reasons in neutral terms (Lifecycle, Observation, `not-found`) and works the same for both providers.
- `delivery` never sees a CDN URL, so no port can leak one (DEC-40).
- Users see why a feature is missing: not offered by the provider, not enabled by debroxy, or not in the account's plan.
- After a restart, a plan restriction is remembered and costs no extra provider call.

### Negative

- Two driving-port styles exist in the code base: operations here, CQRS elsewhere (ADR 0001).
- `OpenDeliveryStream` returns a live stream. Its lifetime spans the client's download, and the caller must close it.
- Learning by failure means one user's request is the first to hit a plan restriction after 24 h.

### Risks and mitigations

- *Risk:* a provider adds or renames an error code, and the code falls into `provider-rejected`. *Mitigation:* unknown codes are logged once per value (NFR-11); recorded fixtures act as regression tests (T-5).
- *Risk:* a temporary provider fault is mistaken for a plan restriction. *Mitigation:* only the explicit plan code sets `unavailable`; 5xx without a known code becomes `provider-unavailable` and leaves the state unchanged.
- *Risk:* neutral types grow provider-specific fields over time. *Mitigation:* review rejects field names that exist in only one provider's API (R-9).
- *Risk:* a cached CDN link expires earlier than 30 min. *Mitigation:* the operation re-resolves once on 401/403/404/410 before the first byte (rule 6); ASM-T24 tracks the assumption.

## Alternatives considered

**Provider clients as plain driven adapters of `job`.** `job/adapters/out/torbox` would implement `ProviderGateway` directly. `delivery` also needs provider traffic, so it would need a second client per account. That splits the rate budget and the pinned egress across two places. Webhook translation and capability state would also land in `job` and blur the core model.

**`provider` returns the CDN URL; `delivery` makes the request** (revision 4, `ResolveCDNLink`). `delivery` would then need its own client with the account's dialer, User-Agent and budget, or `provider` would have to export them. A CDN URL would also cross a port, so every consumer becomes a place where it can leak. DEC-40 keeps the request in `provider`.

**Static capability tables per provider kind.** One table, decided at build time, is simple and needs no storage. TorBox features depend on the plan (TB-N03), and no API lists them (torbox.md T19). A static table would either promise usenet to plan-1 accounts or hide it from plans that include it.

**Lowest common denominator.** Offer only what both providers support: magnet and `.torrent` submission and file delivery. This drops TorBox web downloads, usenet, cached checks, webhooks and zip bundles. It contradicts the goal that debroxy offers what each provider API can do and shows differences as capabilities (§1.2).

## Compliance

- **Import rules** (ADR 0001 test): provider JSON types are unexported; no package outside `provider/adapters/out/<provider>` imports them; `job` and `delivery` import only `provider/ports/in`, and only from their `adapters/out/provider`.
- **Port check:** no type in `provider/ports/in` has a field that holds a CDN URL or an API key; `CDNLink` is not declared in `ports/in`.
- **Adapter fixture tests** (T-5) replay `docs/discovery/<provider>/` for each row of §6.4. Examples: TorBox 500 `PLAN_RESTRICTED_FEATURE` → `capability-unavailable-for-plan`; TorBox 404 `success:true` `ITEM_NOT_FOUND` → `not-found`; Real-Debrid 403 `disabled_endpoint` (37) → capability absent.
- **Deletion tests:** TorBox 500 with swapped `detail:"DATABASE_ERROR"` followed by a not-found look-up → `deleted`; a look-up that still finds the item, or fails → error.
- **Streaming tests:** with an `httptest.Server` as CDN, `OpenDeliveryStream` forwards `Range` only for range-capable targets, returns only whitelisted headers, re-resolves once on 403 before the first byte and not after it, and reuses a cached link within `cdnLinkCacheMax`.
- **State-mapping tests:** every row of §8.1 and §8.2 maps as specified; an unknown native state keeps the Lifecycle and logs once.
- **Capability tests:** initial states as rule 8; persisted state wins at startup; `unavailable` younger than 24 h blocks without a provider call; one success flips to `available`; the three error slugs map to 501/501/409; a not-enabled capability with a stored `unavailable` state still yields `capability-not-enabled`.
- **Redaction test:** neutral results, errors and logs from adapter tests contain neither the API key nor a CDN URL.
- **Review checklist:** new provider behaviour is added as adapter code plus a fixture; neutral types contain no provider-only names; every operation is decorated.
