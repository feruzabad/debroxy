# 0012. Egress pinned per provider account

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 7 (unchanged sections from revisions 5–6 apply), §1.2, §6.1 (`httpProfile`, `egress`), §6.2 `OpenDeliveryStream`, §7.3 step 5, §9.3, §9.4, §11, NFR-02, NFR-03, NFR-09; changelog C-13, C5-03, C5-06; decisions DEC-14, DEC-26, DEC-40 |
| Related | ADR 0001, ADR 0003, ADR 0010, ADR 0013; ddd-rules A-10, DEP-4, OUT-3, OUT-4, OUT-6, S-1, S-3; open question OQ-06; evidence realdebrid.md §3, R14, torbox.md T08, T18; fixtures `docs/discovery/torbox/04-cdn-head.http`, `…/torbox/04-cdn-zip-head.http`, `…/realdebrid/04-cdn-head.http` |

Revised 2026-10-11 for revision 5.

## Context

The goal is that all traffic to a provider, API calls and file bytes alike, leaves from **one IP address** per provider (§1.2, NFR-02). Providers watch for account sharing, for example many IPs on one account (§1.1, RISK-01).

Discovery found that the operating system's default route does not guarantee this:

- One test host reached both providers over **IPv6**; a second test host used IPv4. A dual-stack host can therefore show a provider two addresses, one per family (realdebrid.md §3).
- API hosts and CDN hosts are separate. CDN host names vary per request. The fixtures under `docs/discovery/` show `nexus-182.ceur.tb-cdn.st`, `nexus-321.ceur.tb-cdn.st`, `nexus-292.weur.tb-cdn.st`, `wormhole-001.weur.tb-cdn.st` and `store-079.wnam.tb-cdn.io` for TorBox, and `20-4.download.real-debrid.com` and `130-4.download.real-debrid.com` for Real-Debrid. A check of the API host alone says nothing about CDN reachability in the same family.
- CDN links are not bound to an IP. TorBox links worked from a second IPv4 host (torbox.md T08); Real-Debrid links did too (realdebrid.md R14). Pinning is therefore not needed for links to work. It exists to keep the account's visible footprint to one address.
- Both providers accepted custom User-Agents. TorBox accepted `debroxy-discovery/0.1` and `Mozilla/5.0` on API and CDN (torbox.md T18). Real-Debrid discovery ran with `debroxy-discovery/0.1` throughout (realdebrid.md header). The operator wants to set the User-Agent per account (DEC-14).

Go's default dialer uses both families ("Happy Eyeballs") and falls back from one to the other. Go's default transport also honours `HTTP_PROXY`/`HTTPS_PROXY` from the environment. Either can change the source address without notice.

Revision 5 decides that `provider` performs the CDN upstream request through the operation `OpenDeliveryStream` (DEC-40, C5-03). API calls and CDN downloads can therefore share one dialer that never leaves `provider`.

## Decision

1. **Each Provider Account pins its egress** (DEC-26). The configuration has `egress.ipFamily` (`ipv4` | `ipv6`, default `ipv4`) and optional `egress.localAddress` (§11). Different accounts may pin differently.
2. **One dialer per account for API and CDN.** The account's HTTP client (ADR 0003 rule 11, OUT-4) uses a `net.Dialer` that:
   - always dials the network `tcp4` or `tcp6` that matches `ipFamily`, never `tcp`. Go then uses only resolved addresses of that family and never falls back to the other;
   - binds `LocalAddr` to `localAddress` when it is set.
   The transport sets `Proxy` to `nil`, so environment proxy variables never apply (§9.3, owner decision). No other code builds an HTTP client or dialer for a provider host.
3. **DNS uses the system resolver and is not provider traffic.** Host names are resolved through Go's standard resolver with the host's configured DNS servers. DNS queries go to those servers, not to the provider, so this ADR does not pin them. An operator who also wants DNS on a specific path configures that at host level.
4. **CDN requests are made inside `provider`** (DEC-40). `delivery` calls `DeliveryStreamOpener`, which reaches the `provider` operation `OpenDeliveryStream` (DEP-4, A-10). The operation sends the CDN request through the account's client from rule 2 and returns a stream (ADR 0003 rule 6, ADR 0010). The dialer, the User-Agent and the CDN URL never leave `provider` (OUT-3). `delivery` opens no outbound connection to a provider or CDN host.
5. **Configuration validation in `cmd`** (S-3). `cmd` parses the configuration once. If `localAddress` is set, it must parse as an IP address of the pinned family. Otherwise debroxy refuses to start.
6. **Startup egress check in `provider/service`** (§9.3, S-1). `provider/service.NewApplication` runs the checks below for each enabled account. It returns an error when one fails, and `cmd` then refuses to start. The error names the account, host and family.
   - If `localAddress` is set, it must be assigned to a local interface.
   - debroxy resolves the API host and one representative host per known CDN domain in the pinned family. It opens a TCP connection to each through the pinned dialer.
   - The default probe hosts are concrete names from the fixtures, one per CDN domain: TorBox `nexus-182.ceur.tb-cdn.st` (`tb-cdn.st`) and `store-079.wnam.tb-cdn.io` (`tb-cdn.io`); Real-Debrid `20-4.download.real-debrid.com` (`download.real-debrid.com`). The list lives in the adapter, and the operator can override it in configuration.
   The check proves that the pinned family and route work for each domain. It does not prove that every dynamic CDN host is reachable; rule 7 enforces that at runtime.
7. **Runtime failure mode.** If an API or CDN host has no address in the pinned family, or the pinned connection fails:
   - the call fails with `provider-unavailable`; a stream answers 502 `provider-unavailable` (§7.3 step 5);
   - a missing address in the pinned family raises an operator alert naming the host and family;
   - debroxy **never** retries over the other family or another local address.
   A missing address in the pinned family is not retried at all: a retry through the same dialer would fail again. Only transient connection errors on idempotent calls are retried, through the same pinned dialer (NFR-09). Creates are never retried blindly (NFR-09).
8. **User-Agent per account** (DEC-14). The configuration key is `providers[].userAgent` (§11), default `debroxy/<version>`. It fills `httpProfile.UserAgent` in the account model (§6.1); there is no nested `httpProfile.userAgent` key in the configuration. The value is sent on every API request and every CDN request of that account. debroxy never forwards a client's User-Agent upstream.
9. **Logging.** Egress logs show account ID, family, local address and remote host only, never full URLs (OUT-6, NFR-03).

## Consequences

### Positive

- Each provider sees one source address per account, from one family, for both API calls and downloads (NFR-02).
- A missing route in the pinned family, or a wrong local address, shows up at startup for the API host and each known CDN domain, not during a user's download.
- Environment proxies or a dual-stack host cannot silently change the visible address.
- Egress, User-Agent and rate budget for CDN traffic live in one place, next to the API client.

### Negative

- A provider or CDN node that loses addresses in the pinned family becomes unavailable instead of degrading to the other family.
- The startup check depends on DNS and the network. A provider outage at boot blocks startup of the whole process.
- The CDN probe list needs maintenance when providers change CDN domains or retire the fixture hosts.
- The startup check cannot cover the concrete CDN host that a later link names; such a failure appears only at runtime (rule 7).

### Risks and mitigations

- *Risk:* on a host with several addresses in one family, the OS picks a different source address per route, or rotates IPv6 temporary addresses. *Mitigation:* the operator sets `localAddress` on such hosts; the operator documentation says so. The `ipv4` default avoids IPv6 privacy addresses.
- *Risk:* the host's public address changes (dynamic IP, NAT). *Mitigation:* out of debroxy's control; documented as an operator duty. ASM-G01 (exclusive use of the account) still applies.
- *Risk:* a new CDN domain appears that is not in the probe list. *Mitigation:* the runtime rule still pins it (rule 7). The operator alert names the host, so the list can be extended.
- *Risk:* a probe host from the fixtures is retired by the provider, and startup fails although the route works. *Mitigation:* the error names the host; the operator overrides the list in configuration.
- *Risk:* a provider later rejects the configured User-Agent. *Mitigation:* the value is per account and can be changed without a release.

## Alternatives considered

**Rely on OS default routing.** No code is needed. On a dual-stack host the provider can see an IPv6 and an IPv4 address for the same account (realdebrid.md §3). Go's fallback between families makes the visible address depend on timing. This fails NFR-02.

**Pin API calls only, leave CDN downloads on default routing.** CDN links are not IP-bound (torbox.md T08, realdebrid.md R14), so downloads would still work, and dynamic CDN hosts would need no probe. A dual-stack host would then download over a second address, so the account would show two IPs. §1.2 and §9.3 require one address for API calls **and** file bytes. Rejected.

**Outbound HTTP proxy per provider** (OQ-06). A SOCKS or HTTP proxy per account could route traffic through a VPN or another IP. It adds a dependency and a second component whose own egress must be pinned. All file bytes also take an extra hop. It stays an open question; rule 2 does not prevent adding it later as a separate dialer option.

**Pin globally for the whole process.** One family and one local address for everything is simpler to configure. Accounts could no longer use different paths, for example IPv6 for one provider and IPv4 for another. It would also pin unrelated traffic such as OIDC discovery. Per-account pinning matches OUT-4, which already gives each provider adapter its own HTTP client.

## Compliance

- **Dialer unit test:** for each family, the account's dialer dials only `tcp4`/`tcp6`, binds `localAddress` when set, and returns an error, with no second attempt, when the host has addresses only in the other family. Use a fake resolver.
- **Transport test:** the account's transport has `Proxy == nil`; setting `HTTPS_PROXY` in the test does not change behaviour.
- **Retry test:** a missing-address-in-family error is returned without retry; a transient connection error on an idempotent call is retried through the same dialer; a create is not retried.
- **User-Agent test:** an `httptest.Server` posing as API and as CDN receives the configured User-Agent on both, through `OpenDeliveryStream` for the CDN, and never the client's.
- **Startup tests:** a `localAddress` of the wrong family fails validation in `cmd`; one not assigned locally, or an unreachable probe host, makes `provider/service.NewApplication` return an error naming account, host and family.
- **Lint/import check:** `net.Dialer`, `http.Transport` and `http.Client` are constructed for provider hosts only in `provider/adapters/out/...`. `delivery` constructs no outbound `http.Client` or `net.Dialer`.
- **Review checklist:** no use of `http.DefaultClient` or `http.DefaultTransport`; no network `"tcp"` in provider dialers; egress logs contain no URLs.
