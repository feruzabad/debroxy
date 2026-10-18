# 0022. Embed the web client through a `web` package

| | |
|---|---|
| Status | Accepted (2026-10-13, project owner) |
| Date | 2026-10-13 |
| Deciders | project owner |
| Spec | revision 13, §11, changelog C13-05, NFR-05; decision DEC-57 |
| Related | amends ddd-rules §2 and §3 the way ADR 0015 did for `migrations`; ADR 0014 (dependency table), ADR 0010 (downloads stream through debroxy) |

## Context

- debroxy ships as one binary in one container (NFR-05, revision 12 C12-04). The browser client (`docs/frontend/`) must ship with it and be served from the **same origin** as the API. The session cookie is `SameSite=Strict`, and cookie-authenticated changes need an `Origin` equal to the public base URL (spec §4.3). A second origin would break both.
- The client is a single-page application built to static files (`web/dist`) by its own toolchain.
- Go embeds files only from the directory of the embedding Go file or below it (`//go:embed`), so `cmd/debroxy` cannot embed `../../web/dist`.
- The dependency table (ddd-rules §3, as amended by ADR 0014 and ADR 0015) lets `cmd` import only `<context>/service`, `internal/common/*` and `migrations`.

## Decision

1. `web/` holds the client's sources and one Go file, `web/web.go`, in `package web`, with only the embed directive and one exported variable:
   ```go
   //go:embed all:dist
   var Dist embed.FS
   ```
   No other declarations, and no imports besides `embed`. The build writes `web/dist`. A committed placeholder `web/dist/index.html` keeps the embed compiling before the client is built; it says that the client was not built.
2. `cmd` passes `web.Dist` to a static handler in `internal/common/server`, mounted after every API route. The handler serves any GET or HEAD outside `/api/`, `/dl/`, `/webhooks/`, `/healthz` and `/readyz`:
   - an existing file is served as is;
   - any other path gets `index.html`, for client-side routes;
   - other methods get 405.
3. **Headers.**
   - Files under `assets/` (content-hashed names) get `Cache-Control: public, max-age=31536000, immutable`; `index.html` and other files get `Cache-Control: no-cache`.
   - Every static response carries `Content-Security-Policy: default-src 'self'; img-src 'self' data:; media-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`, plus `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`.
4. Nothing besides `cmd` imports `web`.
5. **ddd-rules changes:**
   - §2 lists `web/web.go` as the embed-only package;
   - the §3 table gains `web → embed (only)` and extends `cmd → <context>/service, internal/common/*, migrations, web`.

## Consequences

### Positive
- One binary and one origin. The cookie, the `Origin` check and the CSP need no exceptions.
- The image needs no web server besides debroxy.

### Negative
- The Docker build gains a Node stage, and a Go build without a client build serves only the placeholder page.
- Routes not under the reserved prefixes belong to the client, so new server routes must use a reserved prefix.

### Risks and mitigations
- *Risk:* the package grows helpers. *Mitigation:* the architecture test allows `web` to import only `embed`, and only `cmd` to import `web` (as for `migrations`).
- *Risk:* the `index.html` fallback hides a mistyped API path. *Mitigation:* the fallback never applies under `/api/`, `/dl/` or `/webhooks/`; unknown paths there stay 404 with a slug body.

## Alternatives considered

**A separate web server container (nginx).** It would serve the client and proxy the API, which means two containers and a proxy config, and the same origin only through the proxy. Rejected by the owner.

**Serve `web/dist` from disk.** Breaks the single-binary rule (NFR-05), and binary and client could drift apart.

## Compliance

- `internal/archtest` classifies `web` as its own layer: it may import only `embed`, and only `cmd` may import it.
- Handler tests: the reserved prefixes are never served by the fallback; client routes get `index.html`; hashed assets get the immutable header; the CSP is present.
