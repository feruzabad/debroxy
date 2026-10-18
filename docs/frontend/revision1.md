# debroxy — Browser Client Specification, Revision 1

| | |
|---|---|
| Revision | 1 |
| Status | Draft, approved in scope by the owner (2026-10-13); not yet implemented |
| Date | 2026-10-13 |
| Supersedes | — (first revision) |
| Server spec | [`../specs/revision13.md`](../specs/revision13.md) (C13-01…C13-06 add what this client needs) |
| API contracts | [`../../api/openapi/identity.yml`](../../api/openapi/identity.yml), [`job.yml`](../../api/openapi/job.yml), [`delivery.yml`](../../api/openapi/delivery.yml), plus the revision 13 additions (§3.2) |
| ADRs | [0020](../adr/0020-retry-hints-in-error-bodies.md) (retry hints), [0021](../adr/0021-event-stream-and-watch-refreshes.md) (event stream), [0022](../adr/0022-embedded-web-assets-package.md) (embedded client); all others apply where they touch the API |

## 0. About this document

### 0.1 Revision workflow

1. This series lives in `docs/frontend/revisionN.md`. The highest revision is current; past revisions are never edited. A change is a new revision with a changelog against the previous one and an updated open-question list (§17).
2. The server spec (`docs/specs/`) and the ADRs are binding. This spec MUST NOT require server behaviour they do not specify. A client need that the server cannot meet is written as a server revision first, as revision 13 did for this one.
3. Code lives in `web/`. No client code is written before the owner starts the implementation.

### 0.2 Conventions

- **MUST / SHOULD / MAY** as in RFC 2119.
- `FE-*` are client decisions (§16). `OQ-FE-*` are open questions (§17). Server IDs (`DEC-*`, `C13-*`, `NFR-*`, slugs) keep their server meaning.
- Field names in `monospace` are API JSON members exactly as the OpenAPI files spell them. "Job", "Remote Item", "Signed Link" and the other capitalized terms are the server glossary's terms (§2).
- UI copy in **"double quotes"** is the exact English text. Placeholders are `{name}`.
- Wireframes are ASCII sketches of layout and content, not pixel designs.

### 0.3 Changelog

Initial revision. Scope decisions by the owner (2026-10-13) are listed as FE-01…FE-30 in §16.

---

## 1. Vision and scope

### 1.1 Goal

A desktop web application for everyone who uses a debroxy instance. Users add torrents and web links, watch them progress, and stream or download the results through debroxy. Admins manage users, quotas, providers and the instance's leftovers. The application uses **every operation and every response field of the API** that has a meaning for a person (§15 proves this). It is served by debroxy itself on the same origin (ADR 0022).

### 1.2 Users

| Role | What they do |
|---|---|
| **User** (`role: user`) | Log in; add jobs; watch progress; rename, release and download jobs; see their history and quota; change their password |
| **Admin** (`role: admin`) | Everything a user does, plus: users (create, role, status, password reset, OIDC link, slot quota), every user's jobs (view live, release), providers (capabilities, probe, hosters, forced refresh), Unmanaged Items, parked deletions |

### 1.3 In scope

- Password login, and OIDC login when the instance enables it.
- Every job flow of the API: submit (magnet, `.torrent`, web URL), pre-submit cached check, live progress through the Event Stream, rename, release, history.
- Delivery: per-file and bundle Signed Links; copy, open, export (aria2, plain text); an in-browser media player.
- All admin pages listed in §1.2.
- Dark, light and system theme.

### 1.4 Out of scope (non-goals)

| Not in v1 | Why |
|---|---|
| Phones and tablets; widths below 1024 CSS px | owner decision (FE-08) |
| Languages other than English; an i18n framework | owner decision (FE-09) |
| PWA, offline use, a service worker | owner decision (FE-10) |
| Batch submit, bulk release | owner decision (FE-13, FE-17) |
| Custom keyboard shortcuts, a command palette | owner decision (FE-12) |
| Browser (OS) notifications, sounds | owner decision (FE-21) |
| NZB submission | the server does not enable usenet in v1 (revision 8 C8-01); the client hides NZB sources unless a provider reports `SubmitNZBFile`/`SubmitNZBURL` as `enabled` (§6.5) |
| Adopting Unmanaged Items, retrying parked deletions | no API exists (server OQ-01) |
| Reading a user's current slot quota as admin | no API exists; the UI says so (§5.11, OQ-FE-01) |
| Pagination | the API returns whole lists; the client virtualizes long tables (§12) |

---

## 2. Glossary mapping (UI words ↔ server terms)

The UI uses short, plain words. Each maps to exactly one server term; tooltips and help texts use the plain word first.

| UI word | Server term | Meaning shown to users |
|---|---|---|
| **Job** | Job | "Your request for one piece of content." |
| **Shared** | `shared: true` (another claim on the same Remote Item) | "Someone else also added this. It stays at the provider until everyone releases it." Never says who (NFR-04). |
| **Counts toward quota** | `slotCharged: true` (Slot Charge) | "This job uses one of your {max} active slots." |
| **Slots** | Slot Quota (`maxActiveJobs`), `slotsUsed` | "Active jobs that count toward your limit, including ones still being added." |
| **Release** | `ReleaseJob` | "Remove this job from your list. If nobody else uses the content, debroxy deletes it at the provider." |
| **Link** | Signed Link | "A private download link that works without logging in, until it expires. Treat it like a password." |
| **Archive** | Bundle (`delivery.bundle`) | "All files in one {ZIP\|RAR} archive." |
| **Cached** | cached availability (`CachedCheck`) | "The provider already has this content: it will be ready almost instantly." |
| **Not up to date** | `stale: true` | "debroxy could not reach {provider} just now. You're seeing the last known state." |
| **Unmanaged item** | Unmanaged Item (DEC-10) | "Something on the provider account that debroxy did not add." |
| **Stuck deletion** | parked outbox message | "debroxy gave up deleting this item after {attempts} attempts. It still exists at the provider." |
| **Capability** | Capability + Capability State | "What this provider account can do, as debroxy last learned it." |
| **Live / Reconnecting / Offline** | Event Stream connection state | §8.4 |

---

## 3. Architecture

### 3.1 Technology (FE-01…FE-06)

| Concern | Choice | Notes |
|---|---|---|
| Language | TypeScript 5.x, `strict: true`, `noUncheckedIndexedAccess: true`, `exactOptionalPropertyTypes: true` | no `any` outside generated code; Biome rule enforced |
| UI runtime | React 19 | function components only; no class components; `use` and Actions only where they simplify (forms use TanStack Form) |
| Build | Vite (current major) with `@vitejs/plugin-react` | output `web/dist`, hashed assets under `assets/` (ADR 0022 rule 3) |
| Routing | TanStack Router, file-based routes (`@tanstack/router-plugin/vite`) | typed params and Zod-validated search params |
| Server state | TanStack Query v5 | the only cache of server data (§3.5) |
| Forms | TanStack Form + Zod | Zod schemas mirror server rules (§7.3) |
| Components | shadcn/ui (Radix primitives), copied into `src/components/ui/` | no other component library |
| Styling | Tailwind CSS v4 (`@tailwindcss/vite`), CSS-first config (`@theme`) | no CSS-in-JS, no runtime `<style>` injection (CSP, §11) |
| Icons | `lucide-react` | |
| Tables | TanStack Table v8 + TanStack Virtual | sorting and filtering client-side |
| API types | `openapi-typescript` generated from `api/openapi/*.yml` | `src/api/generated/` is generated, committed, never edited |
| API client | `openapi-fetch` over the generated types, wrapped once (§3.3) | |
| Dates | `Intl.DateTimeFormat` / `Intl.RelativeTimeFormat` | no date library |
| IDs | UUIDv7 generated in the browser (`uuid` package `v7`) | for `SubmitJobRequest.id` |
| Hashing | Web Crypto `crypto.subtle.digest('SHA-1')` | info hash of `.torrent` files (§5.5.4) |
| Toasts | shadcn `sonner` wrapper, with styles imported as CSS (no runtime style injection) | |
| Lint/format | Biome (`biome.json`), recommended rules + `noExplicitAny`, `useExhaustiveDependencies`, `noDangerouslySetInnerHtml` | |
| Package manager | pnpm with a committed `pnpm-lock.yaml` | Node LTS pinned in `web/.nvmrc` and in the Docker stage |
| Tests | Vitest + Testing Library + MSW; Playwright for visual regression | §14 |

Every dependency is pinned to an exact version in `package.json`. Adding a dependency needs a one-line justification in its commit.

### 3.2 API surface used

The client calls only the API below. "Rev 13" marks additions from server revision 13; the generated types MUST include them. The OpenAPI files are updated together with the server change.

| Operation | Method & path | Used by (§) |
|---|---|---|
| `logIn` | `POST /api/v1/auth/login` | 5.1 |
| `logOut` | `POST /api/v1/auth/logout` | 5.2 |
| `startOIDCLogin` | `GET /api/v1/auth/oidc/login` (full-page navigation) | 5.1 |
| `finishOIDCLogin` | `GET /api/v1/auth/oidc/callback` (server-side redirect target, not called by the client) | 5.1 |
| `getClientConfig` (rev 13) | `GET /api/v1/config` | 3.6, 5.1, 5.5 |
| `getMe` (+ rev 13 `session`, `submitRate`) | `GET /api/v1/me` | 3.6, 5.2 |
| `changeOwnPassword` | `PUT /api/v1/me/password` | 5.9 |
| `watchJobs` (rev 13) | `GET /api/v1/events[?scope=all]` | 8 |
| `listProviders` | `GET /api/v1/providers` | 5.2, 5.5, 5.13 |
| `listHosters` | `GET /api/v1/providers/{providerId}/hosters` | 5.5, 5.13 |
| `checkCachedAvailability` | `POST /api/v1/providers/{providerId}/cached-availability` | 5.5 |
| `refreshProvider` | `POST /api/v1/providers/{providerId}/refresh` | 5.3, 5.13 |
| `listJobs` | `GET /api/v1/jobs` | 5.3 |
| `submitJob` | `POST /api/v1/jobs` | 5.5 |
| `listJobHistory` | `GET /api/v1/jobs/history` | 5.8 |
| `getJob` | `GET /api/v1/jobs/{jobId}` | 5.6 |
| `renameJob` | `PATCH /api/v1/jobs/{jobId}` | 5.6 |
| `releaseJob` | `DELETE /api/v1/jobs/{jobId}` | 5.3, 5.6 |
| `issueSignedLink` | `POST /api/v1/jobs/{jobId}/links` | 5.6, 5.7 |
| `issueSignedLinksForAllFiles` | `POST /api/v1/jobs/{jobId}/links/all` | 5.6 |
| `download` | `GET /dl/{token}/{name}` (browser navigation, `<video>`/`<audio>` source) | 5.6, 5.7 |
| `listUsers`, `createUser`, `getUser`, `updateUser`, `resetUserPassword`, `linkOIDCIdentity`, `unlinkOIDCIdentity` | `/api/v1/admin/users…` | 5.10, 5.11 |
| `setSlotQuota` | `PUT /api/v1/admin/users/{userId}/slot-quota` | 5.11 |
| `listAllJobs`, `releaseAnyJob` | `/api/v1/admin/jobs…` | 5.12 |
| `probeProviderCapabilities` | `POST /api/v1/admin/providers/{providerId}/probe-capabilities` | 5.13 |
| `listUnmanagedItems` | `GET /api/v1/admin/unmanaged-items` | 5.14 |
| `listParkedOutboxMessages` | `GET /api/v1/admin/outbox` | 5.15 |

`downloadHead` (`HEAD /dl/…`) is used only by the player to find out why a media element failed (§5.7). `handleProviderWebhook` (provider to server) is not used. §15 lists both.

### 3.3 The API layer (`src/api/`)

```
src/api/
  generated/identity.ts, job.ts, delivery.ts   openapi-typescript output (committed)
  client.ts       one openapi-fetch client per spec file, sharing one fetch wrapper
  errors.ts       ApiError, slug union type, parseError()
  keys.ts         the Query key factory (§3.5)
  queries/*.ts    queryOptions() per GET operation
  mutations/*.ts  mutation hooks per changing operation
  events.ts       the Event Stream client (§8)
  btih.ts         magnet and .torrent info-hash extraction (§5.5.4)
```

**Fetch wrapper (MUST):**
1. Base URL is `""` (same origin). `credentials: "same-origin"`. The browser adds the HttpOnly cookie; the client never reads, stores or sends a token (FE-04). It never sends an `Authorization` header.
2. Requests with a body send `Content-Type: application/json`. The browser sets `Origin` itself on non-GET requests, which satisfies the server's cookie `Origin` check (server §4.3). The client MUST NOT be configured with a different origin in production.
3. Every non-2xx response is turned into an `ApiError { status, slug, message, retryAfter? }`:
   - from the JSON body `{slug, message, retryAfter?}`;
   - else from the `Retry-After` header for `retryAfter`;
   - if the body is not JSON: `slug` = `"http-{status}"`, `message` = `""`.
   Network failures (fetch rejects) become `ApiError { status: 0, slug: "network-error" }`. A request aborted by the client is not an error and is never shown.
4. A **401** from any call except `logIn` triggers the session-ended flow (§7.4) exactly once, even if several requests fail together.
5. Timeouts: none by default. Long calls (submit can wait up to the server's 10 s request deadline) show progress. The wrapper does not retry; Query retry rules apply (§8.6).
6. Responses are never logged to the console in production builds. Signed Link URLs and passwords MUST NOT appear in any log, error report or analytics (there are none in v1).

**`ApiError` slug type:** a string-literal union of every slug in §7.2 plus `"network-error"` and `"http-{status}"`, so `switch` statements are exhaustive.

### 3.4 Directory layout (`web/`)

```
web/
  web.go                 embed-only Go package (ADR 0022), not touched by the JS toolchain
  dist/                  build output; dist/index.html placeholder is committed
  index.html
  package.json, pnpm-lock.yaml, .nvmrc, biome.json, tsconfig.json, vite.config.ts, vitest.config.ts, playwright.config.ts
  public/                favicon.svg, robots.txt (Disallow: /)
  src/
    main.tsx             providers: QueryClient, Router, Theme, Toaster, EventStream
    routes/              file-based routes (§4)
    features/
      auth/  jobs/  add-job/  job-detail/  player/  history/  account/
      admin-users/  admin-jobs/  admin-providers/  admin-unmanaged/  admin-outbox/
    components/
      ui/                shadcn components (generated by the shadcn CLI, then owned)
      app/               shell, nav, meters, badges, empty states, error panels
    lib/                 format.ts, states.ts, capabilities.ts, filetree.ts, media.ts, exports.ts, storage.ts, theme.ts
    api/                 §3.3
    styles/app.css       Tailwind entry + @theme tokens (§9)
  tests/
    unit/  components/  visual/   mocks/ (MSW handlers + fixtures typed from generated types)
```

Each `features/<name>/` folder holds the components, hooks and Zod schemas of one area. Features import only `components/`, `lib/`, `api/` and their own files, never another feature. Shared pieces move to `components/app`.

### 3.5 State management

| State | Where | Persistence |
|---|---|---|
| Server data | TanStack Query cache | memory; cleared on logout and session end |
| URL state (filters, sort, search, tabs) | TanStack Router search params, validated by Zod | the URL |
| Theme | `lib/theme.ts`; `data-theme` attribute on `<html>` | `localStorage["debroxy.theme"]` = `light` \| `dark` \| `system` |
| Last used provider | `lib/storage.ts` | `localStorage["debroxy.lastProvider"]` = provider ID |
| Signed Links issued in this page session | Query cache under `['links', jobId, targetKey]` | memory only, never storage (§11) |
| Toast de-duplication | in-memory set of `(jobId, state)` | memory |

Every `localStorage` access is wrapped in `try/catch`; a failure falls back to the default. Nothing else is persisted. No cookies are written by the client.

**Query key catalogue** (`api/keys.ts`):

| Key | Data | Fresh for (`staleTime`) | Invalidated by |
|---|---|---|---|
| `['config']` | ClientConfig | `Infinity` (per page load) | never |
| `['me']` | Me | 30 s | login, logout, password change, rename/submit/release (quota), `quota.updated` replaces `quota` |
| `['providers']` | ProviderList | 60 s | `probeProviderCapabilities`, `provider.stale` (merges the flag, §8.3) |
| `['hosters', providerId]` | HosterList | 10 min | never (manual reload button) |
| `['jobs']` | JobList | `Infinity` while the stream is live, otherwise 10 s | stream events (direct writes); refetch on stream (re)connect; after submit/release/rename |
| `['job', jobId]` | Job | as `['jobs']` | stream events; after rename/release/link errors 409 |
| `['history']` | HistoryList | 60 s | `job.released` prepends; after releases |
| `['cached', providerId, fingerprint]` | boolean | 5 min | never |
| `['links', jobId, 'file:'+fileId \| 'bundle' \| 'all']` | SignedLink(s) | until `expiresAt − 60 s` (§5.6.6) | job leaves `ready`; job removed |
| `['admin','users']` | UserList | 30 s | user mutations |
| `['admin','user', userId]` | User | 30 s | user mutations |
| `['admin','jobs']` | JobList (all) | as `['jobs']`, fed by the `scope=all` stream | `releaseAnyJob` |
| `['admin','unmanaged']` | UnmanagedItemList | 60 s | manual refresh |
| `['admin','outbox']` | OutboxMessageList | 60 s | manual refresh |

### 3.6 Bootstrapping

1. `index.html` loads one module script. Before React renders, an inline-free bootstrap (in `main.tsx`) applies the stored theme to `<html>`, to avoid a flash of the wrong theme. The CSP forbids inline scripts, so the theme is applied by the module itself, and `index.html` sets `<meta name="color-scheme" content="light dark">`.
2. The router's root `beforeLoad` fetches `['config']` (public) and, for every non-public route, ensures `['me']`.
   - `getMe` 200 → the user is logged in; the role decides the admin guard.
   - `getMe` 401 → redirect to `/login?redirect={current path + search}`.
   - Any other error → the full-page error state (§7.5).
3. After `['me']` succeeds, the Event Stream connects (§8). On `/admin/jobs`, a second stream with `scope=all` connects (§8.2).
4. The app shell renders when `['config']` and `['me']` are known; until then a centered spinner with **"Loading debroxy…"** shows (no layout shift: the shell's skeleton is drawn).

---

## 4. Routes

### 4.1 Route table

Paths are TanStack Router file routes. Every search param is validated by the Zod schema shown. Invalid values are replaced by the defaults, never rejected; the URL is rewritten with `replace`. "Guard" values: **public** (no session needed; a logged-in visitor of `/login` is sent to `/`), **user** (any active session), **admin** (`me.user.role === "admin"`; others see the 403 state of §7.5).

| Path | Guard | Search params (Zod) | Data prefetched in `loader` | Document title |
|---|---|---|---|---|
| `/login` | public | `{ redirect?: string (must start with "/" and not "//"), error?: "oidc" }` | `['config']` | "Log in · debroxy" |
| `/` | user | `JobsSearch` (§5.3.2) | `['jobs']`, `['providers']` | "Jobs · debroxy" |
| `/add` | user | `{ source?: "magnet"\|"torrent"\|"web", provider?: string }` | `['providers']`, `['config']` | "Add job · debroxy" (dialog over `/`) |
| `/jobs/$jobId` | user | `{ tab?: "files"\|"details" = "files" }` | `['job', jobId]` | "{displayName} · debroxy" |
| `/history` | user | `{ q?: string, sort?: HistorySort }` | `['history']` | "History · debroxy" |
| `/account` | user | — | `['me']` | "Account · debroxy" |
| `/admin/users` | admin | `{ q?: string }` | `['admin','users']` | "Users · Admin · debroxy" |
| `/admin/users/new` | admin | — | `['config']` | "New user · Admin · debroxy" (dialog over `/admin/users`) |
| `/admin/users/$userId` | admin | — | `['admin','user', userId]` | "{username} · Admin · debroxy" |
| `/admin/jobs` | admin | `JobsSearch` + `{ owner?: string }` | `['admin','jobs']`, `['providers']` | "All jobs · Admin · debroxy" |
| `/admin/providers` | admin | `{ provider?: string }` (selected tab) | `['providers']` | "Providers · Admin · debroxy" |
| `/admin/unmanaged` | admin | `{ provider?: string }` | `['admin','unmanaged']` | "Unmanaged items · Admin · debroxy" |
| `/admin/outbox` | admin | — | `['admin','outbox']` | "Stuck deletions · Admin · debroxy" |
| `*` (no match) | user | — | — | "Not found · debroxy" |

Route components are code-split (`lazy` route files); `/`, `/login` and the shell are in the initial chunk.

### 4.2 Navigation structure

```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ ◆ debroxy                      [ Search jobs…            ]   ● Live   ☾  alice ▾     │  header (48 px)
├───────────────┬──────────────────────────────────────────────────────────────────────┤
│ + Add job     │                                                                      │
│               │                                                                      │
│ ▸ Jobs     12 │                      route content                                   │
│ ▸ History     │                                                                      │
│ ▸ Account     │                                                                      │
│               │                                                                      │
│ ADMIN         │                                                                      │
│ ▸ Users       │                                                                      │
│ ▸ All jobs    │                                                                      │
│ ▸ Providers ⚠ │                                                                      │
│ ▸ Unmanaged   │                                                                      │
│ ▸ Stuck del. 2│                                                                      │
│               │                                                                      │
│ Slots  2 / 3  │                                                                      │
│ ▓▓▓▓▓▓░░░     │                                                                      │
│ Submits 3/10  │                                                                      │
│ ⟳ in 42 min   │                                                                      │
│ v0.2.0        │                                                                      │
└───────────────┴──────────────────────────────────────────────────────────────────────┘
  sidebar 224 px
```

- **Header:**
  - The global search box filters the job list. On other pages, typing and pressing Enter navigates to `/?q=…`.
  - The Event Stream indicator (§8.4).
  - The theme toggle: a cycle button light → dark → system with its icon (sun, moon, monitor) and tooltip **"Theme: {Light|Dark|System}"**.
  - The user menu: username and role badge; items **"Account"**, **"Log out"**.
- **Sidebar:**
  - **"Add job"** is the primary button and opens `/add`.
  - The **"Jobs"** item shows the count of the user's active jobs (state group *active*, §6.1).
  - The ADMIN section is rendered only for admins.
  - **"Providers"** shows a warning dot when any provider is `stale` or has an `unavailable` capability.
  - **"Stuck deletions"** shows the count of parked messages, only after `['admin','outbox']` has been loaded once in this session.
  - The quota meters are described in §5.2.3, and the server `version` from `['config']` shows at the bottom.
- **Active item:** the current route is highlighted (`aria-current="page"`).

---

## 5. Screens

Each screen lists its layout, its fields with the API member that feeds them, its states (loading, empty, error, stale) and its actions with their API calls and outcomes. Error outcomes not listed explicitly follow the slug table (§7.2).

### 5.1 Login (`/login`)

```
            ┌────────────────────────────────────┐
            │            ◆ debroxy               │
            │                                    │
            │  Username  [____________________]  │
            │  Password  [____________________]👁 │
            │                                    │
            │  [            Log in            ]  │
            │  ─────────────── or ─────────────  │
            │  [      Log in with single sign-on ]│   only if config.oidcEnabled
            │                                    │
            │  ⚠ Too many attempts. Try again in │   login-throttled, with countdown
            │    0:42.                           │
            └────────────────────────────────────┘
                         v{config.version}
```

- **Form:**
  - `username`: `autocomplete="username"`, required, trimmed. The client does not lowercase it; the server does (server §4.4).
  - `password`: `autocomplete="current-password"`, required, with a show/hide toggle (`aria-pressed`).
- **Submit** → `logIn {username, password}`:
  - **200:** the server sets the cookie. The client ignores `token` and `expiresAt` from the body (FE-04), invalidates `['me']`, then navigates to `redirect` or `/`.
  - **401 `invalid-credentials`:** an inline form error, **"Wrong username or password."** The password field is cleared and focused.
  - **429 `login-throttled`:** an inline alert, **"Too many attempts. Try again in {m:ss}."**, with the countdown from `retryAfter`. The submit button is disabled until it reaches 0.
  - **400 `invalid-request`:** **"Enter a username and a password."**
  - Network error: **"Can't reach debroxy. Check your connection and try again."**
- **OIDC button:** shown only when `config.oidcEnabled` is true. A click sets `window.location.href = "/api/v1/auth/oidc/login"` (a full navigation, because the server redirects to the identity provider and back to `/`).
- **`?error=oidc`:** shows **"Single sign-on didn't work. Your account may not be linked yet; ask an admin."** Today the server answers a failed callback with an error response instead of redirecting, so this parameter only takes effect once the server redirects back (OQ-FE-02).
- **Already logged in** (`['me']` resolves): redirect to `redirect` or `/`.
- **Focus:** username on load; Enter submits.

### 5.2 App shell (all `user` routes)

#### 5.2.1 Session lifecycle

- **Idle expiry warning:** `me.session.idleExpiresAt` (rev 13) is refreshed by every request. When it is less than **5 min** away and no request has happened, a non-dismissable banner reads **"You'll be logged out in {m:ss} because of inactivity."** with the button **"Stay logged in"**. The button calls `getMe`, which touches the session, and hides the banner.
  - A running Event Stream does not count as activity. Whether long-lived streams touch the session is not specified (OQ-FE-09), so the client relies only on explicit requests.
  - A timer re-checks every 15 s.
- **Absolute expiry:** when `me.session.absoluteExpiresAt` is less than 10 min away, the banner reads **"Your session ends in {m}. Log in again to continue."** with **"Log in again"**, which logs out and then goes to `/login?redirect=…`.
- **Logout** (user menu) → `logOut`:
  - **204 or 401:** clear the Query cache, close the Event Stream, navigate to `/login`.
  - Other errors: show a toast **"Couldn't log out on the server. You've been logged out here."** and then do the same.

#### 5.2.2 Provider status

`['providers']` feeds the sidebar warning dot (§4.2). The `provider.stale` events (§8.3) keep a `staleByProvider` map in the Query cache under `['providers']` (a client-only field `stale?: boolean` merged per provider). When any provider the user has jobs on is stale, the jobs page shows the stale banner (§5.3.4).

#### 5.2.3 Quota meters (sidebar)

| Meter | Source | Text | Visual |
|---|---|---|---|
| **Slots** | `me.quota.slotsUsed`, `me.quota.maxActiveJobs` | "Slots {used} / {max}" | Bar; amber at max−1, red at max. With `maxActiveJobs = 0` it reads **"Slots: none allowed"** in red. |
| **Submits** | `me.submitRate.uncached` (rev 13) | "Submits {used} / {perHour} this hour" | Bar; when `used ≥ perHour` it turns red and shows "⟳ in {relative resetsAt}". |
| **Cached submits** | `me.submitRate.cached` | shown in the meter's tooltip: "Cached: {used} / {perHour}" | — |

Tooltip on **Slots**: **"Active jobs that count toward your limit, including ones still being added. Jobs shared with others count for whoever added them first, until they release them."** (ADR 0006 in plain words.)

`quota.updated` events replace `me.quota` in the cache; `me.submitRate` refreshes with `['me']` (30 s stale time, and after every submit).

### 5.3 Jobs (`/`)

#### 5.3.1 Layout

```
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│ Jobs                                                        [⟳ Refresh ▾]   [+ Add job]  │
│ ⚠ Not up to date: debroxy couldn't reach TorBox just now. Showing the last known state.  │  stale banner
│ [All 14] [Active 3] [Ready 9] [Problems 2]    Provider: [All ▾]     [🔍 filter by name ] │
├────┬──────────────────────────────┬─────────┬──────────────┬────────────────┬────────┬───┤
│ ●  │ Name                       ▲ │ Provider│ State        │ Progress       │ Size   │ … │
├────┼──────────────────────────────┼─────────┼──────────────┼────────────────┼────────┼───┤
│ ◐  │ debian-13.7.0-amd64-netinst  │ TorBox  │ Downloading  │ ▓▓▓▓░░ 62.4%   │ 754 MiB│ ⋯ │
│    │ ⇄ shared                     │         │              │ 12.3 MiB/s · 41s · 18 peers     │
│ ✓  │ Sintel                       │ RD      │ Ready        │ —              │ 123 MiB│ ⋯ │
│ ✕  │ big-buck-bunny               │ TorBox  │ Failed       │ Provider rejec…│ —      │ ⋯ │
└────┴──────────────────────────────┴─────────┴──────────────┴────────────────┴────────┴───┘
  14 jobs · 3 active · 2 of 3 slots used
```

#### 5.3.2 Search params (`JobsSearch`)

```ts
const JobsSearch = z.object({
  q: z.string().trim().max(200).optional(),                       // name filter
  group: z.enum(["all", "active", "ready", "problems"]).default("all"),
  provider: z.string().optional(),                                  // provider id; absent = all
  sort: z.enum(["name", "provider", "state", "progress", "size", "expires", "created"]).default("created"),
  dir: z.enum(["asc", "desc"]).default("desc"),
});
```

#### 5.3.3 Columns

| Column | Source | Rendering | Sort key |
|---|---|---|---|
| Status icon | `state`, `telemetry.stalled` | the state icon (§6.1); while `stalled` the icon is the stalled icon and has a tooltip | — |
| **Name** | `displayName`; second line: `shared`, `slotCharged` | the name is text (never HTML), ellipsized with the full name in a tooltip, and links to `/jobs/{id}`. Under it, small badges: **"⇄ shared"** if `shared`; **"counts toward quota"** if `slotCharged` (muted). With `kind` = `web`, a globe icon goes before the name. | `displayName` (locale compare, case-insensitive) |
| **Provider** | `provider` → `providers[].displayName` | the display name; the raw ID if unknown | display name |
| **State** | `state` | the state badge (§6.1) | state order of §6.1 |
| **Progress** | `telemetry.*`, `failureReason` | active states show a bar `progress × 100` (one decimal), with a second line: `bytesPerSecond` (speed, §6.6) · `etaSeconds` (ETA; "—" if null) · `peers` ("{n} peers"; hidden if null). `ready` shows "—". `failed`/`gone` show the failure text (§6.3), truncated. | `progress` |
| **Size** | `sizeBytes` | IEC (§6.6); 0 → "—" | `sizeBytes` |
| **Expires** | `expiresAt` | relative ("in 3 d"); amber under 24 h, red under 1 h; null → "—" (§6.4) | `expiresAt` (nulls last) |
| **Added** | `createdAt` | relative, with the exact time in a tooltip | `createdAt` |
| Actions `⋯` | — | a dropdown: **"Open"**, **"Copy all links"** (ready only), **"Rename…"**, **"Release…"** | — |

Row height is 40 px, or 52 px with a second line. Rows are keyboard-focusable; Enter opens the job. The table virtualizes above 200 rows (§12).

#### 5.3.4 States

| State | Display |
|---|---|
| Loading (first load) | 8 skeleton rows |
| Empty (no jobs at all) | An illustration-free empty state: **"No jobs yet"**, then **"Add a magnet link, a .torrent file or a web link to get started."**, and the button **"Add job"** |
| Empty after filtering | **"No jobs match these filters."** with **"Clear filters"** |
| Stale | When `JobList.stale` is true, or any provider of a listed job has `stale` from `provider.stale`: an amber banner, **"Not up to date: debroxy couldn't reach {provider names} just now. Showing the last known state."**. It disappears when fresh data arrives. |
| Error | the inline error panel (§7.5) with **"Try again"** |

The footer line reads "{n} jobs · {active} active · {slotsUsed} of {maxActiveJobs} slots used".

#### 5.3.5 Actions

- **Refresh** (split button) → `refreshProvider` for the provider chosen in its menu (**"Refresh {provider}"**, one item per enabled provider). The main button refreshes every provider of the listed jobs in sequence.
  - **200 `{stale:false}`:** toast **"{provider} refreshed."**, then refetch `['jobs']`.
  - **200 `{stale:true}`:** toast **"{provider} couldn't be reached. Showing the last known state."**
  - **429 `refresh-rate-exceeded`:** the button is disabled with **"Try again in {s}s"** (from `retryAfter`).
  - **404 `provider-not-found`:** refetch `['providers']`, toast per §7.2.
- **Rename…** opens the rename dialog (§5.6.3).
- **Release…** opens the release dialog (§5.6.4).
- **Copy all links** follows §5.6.6, for this row.
- **Filters:** the group chips show live counts; provider is a select with "All providers" plus `providers[]`. Every change updates the URL with `replace`.

### 5.4 Shared job presentation components

Both `/` and `/admin/jobs` use `JobTable`, `StateBadge`, `ProgressCell`, `ReleaseDialog` and `RenameDialog` from `features/jobs`. They live in `components/app` once a second feature uses them (§3.4 rule).

### 5.5 Add job (`/add`, a dialog over the jobs page)

#### 5.5.1 Layout

```
┌ Add job ───────────────────────────────────────────────────── ✕ ┐
│ Provider  [ TorBox ▾ ]                                           │
│ [ Magnet link ] [ .torrent file ] [ Web link ]                   │  tabs (only enabled ones)
│ ┌──────────────────────────────────────────────────────────────┐ │
│ │ magnet:?xt=urn:btih:…                                        │ │  textarea, 3 rows
│ └──────────────────────────────────────────────────────────────┘ │
│ ⚡ Cached at TorBox: ready almost instantly                       │  cached-check result
│ Name (optional) [_______________________________]                │
│                                                                  │
│ Uses 1 of your 3 slots · 7 of 10 submits left this hour          │
│                                        [ Cancel ]  [ Add job ]   │
└──────────────────────────────────────────────────────────────────┘
```

#### 5.5.2 Provider choice (FE-15)

- **Options:** only providers with `enabled: true`.
- **Disabled options:** a provider that supports none of the visible source types is shown disabled, with the tooltip **"{provider} can't take {type} right now."**
- **Preselection, in order:**
  1. the `?provider` search param;
  2. `localStorage["debroxy.lastProvider"]`, if that provider is enabled and supports the selected source type;
  3. the first enabled provider (in `providers[]` order) that supports it.
- **Remembering:** the chosen provider is stored after a successful submit (201/202/200).
- **Switching:** a provider switch keeps the typed input and re-runs the cached check.

#### 5.5.3 Source tabs and capability rules

A source tab is **available** for a provider when its capability (table below) has `enabled: true` and `state !== "unavailable"`. A capability with `state: "unverified"` is available; the server learns its real state on first use. A tab available for no provider is hidden. A tab available for some providers but not the chosen one is shown disabled, with the tooltip **"Not available at {provider}"**.

| Tab | Capability | `Source` sent | Input rules (Zod) |
|---|---|---|---|
| **Magnet link** | `SubmitMagnet` | `{type:"magnet", magnet}` | trimmed; must start with `magnet:?`; must contain `xt=urn:btih:` (40 hex or 32 base32) or `xt=urn:btmh:1220` (64 hex); ≤ 16 KiB. Error: **"This doesn't look like a magnet link."** |
| **.torrent file** | `SubmitTorrentFile` | `{type:"torrent-file", fileName, file: base64}` | one file; the name ends in `.torrent` (case-insensitive) or the MIME is `application/x-bittorrent`; size ≤ `config.uploadLimitBytes`, else **"This file is {size}; the limit is {limit}."**; it must parse as bencode with an `info` dictionary (§5.5.4), else **"This file isn't a valid .torrent."** A drop zone plus a file picker; the chosen file shows as a chip with name, size and a remove button. |
| **Web link** | `SubmitWebURL` | `{type:"web-url", url, password?}` | an absolute `http(s)` URL, ≤ 8 KiB. The host must match a domain of an `up` hoster in `['hosters', provider]` (suffix match on `domains[]`), else the warning **"{host} isn't on {provider}'s list of supported sites."** The warning does not block, because the server decides (`unsupported-hoster`). Optional **"Password"** field (`type=password`, `autocomplete="off"`), with the help text **"Only if the link is password-protected. If someone else already added this link, their password is used."** (server §5.1). |
| *(NZB file, NZB URL)* | `SubmitNZBFile`, `SubmitNZBURL` | — | hidden while the capability is not `enabled` (v1). When a future server enables them, they follow the torrent and web rules with `type: "nzb-file"` / `"nzb-url"` (OQ-FE-03). |

- **Name (optional)** → `displayName`, trimmed, ≤ 255 characters (FE-25; the server sets no limit, OQ-FE-04). Empty means the field is omitted, and the server derives the name from the source.
- **Hoster list:** the web tab loads `['hosters', provider]` lazily. A collapsible **"Supported sites ({n})"** lists the hosters with their up/down status and daily usage ("{dailyUsed} / {dailyLimit} today", when present; OQ-FE-08). It shows **"Can't load the list of supported sites."** on `501 capability-not-supported` (the provider has no catalogue) or on other errors; submitting stays possible.

#### 5.5.4 Pre-submit cached check (FE-14)

Runs only when the chosen provider's `CachedCheck` capability is available (§5.5.3) and the tab is magnet or `.torrent`. It never runs for web links (server C6-04).

1. **Fingerprint extraction** (`api/btih.ts`, pure functions, unit-tested):
   - **Magnet:**
     - Parse `xt` values. For `urn:btih:` with 40 hex characters, lowercase them. For 32 base32 characters, decode RFC 4648 base32 to 20 bytes and hex-encode, lowercase.
     - Fingerprint = `"btih:" + hex`.
     - A magnet with only `urn:btmh:` (BitTorrent v2) yields no fingerprint, so no check runs (the server computes no provider hash for v2-only torrents, server §5.10).
   - **.torrent:**
     - Parse the bencode in a Web Worker so a large file doesn't block the UI. Find the raw byte span of the top-level `info` value; this needs a bencode scanner that tracks offsets and does not re-encode.
     - Fingerprint = `"btih:" + hex(SHA-1(info bytes))`.
     - A v2-only torrent (`info` has `meta version` 2 and no `pieces`) yields no fingerprint.
2. **Call:** debounced 400 ms after the input settles, with `checkCachedAvailability(provider, {fingerprints: [fp]})`, cached under `['cached', provider, fp]`.
3. **Display under the input:**
   - `cached[fp] === true` → **"⚡ Cached at {provider}: ready almost instantly"** (green).
   - `false` → **"Not cached at {provider}: it will download first."** (muted).
   - Pending → a spinner, **"Checking {provider}…"**.
   - Any error (including `capability-*`, `provider-unavailable`) → nothing shown. The check is a hint only.
4. **Rate hint:** when the remaining submits of the class that applies (cached → `submitRate.cached`, otherwise `submitRate.uncached`) are 0, the submit button is disabled with **"Submit limit reached. Try again {relative resetsAt}."** The server's cached check is authoritative and may classify differently; a `submit-rate-exceeded` answer is then shown per §7.2.

#### 5.5.5 Submit

1. When the dialog opens, the client generates a `jobId` (UUIDv7). The same ID is reused for retries of this submission until the dialog closes or a non-retryable error arrives. This makes a retry after a lost response safe (server §5.10).
2. `submitJob {id: jobId, providerId, displayName?, source}`. The button shows **"Adding…"**; the server may take up to its request deadline (10 s).
3. **Outcomes:**

| Response | Client behaviour |
|---|---|
| **201** `Job` | Close the dialog; write the job into `['jobs']` and `['job', id]`; invalidate `['me']`; toast **"Added {displayName}."** with an **"Open"** action; store the last provider. |
| **202** `SubmissionPending {slug:"submission-pending", job}` | Same as 201, but the toast reads **"{provider} is still accepting {displayName}. It will appear as soon as it's confirmed."**. The job shows state `submitting` and updates through the Event Stream. |
| **200** `HistoryEntry` | A retry of a submission whose job was already released. Close the dialog and show the toast **"You added and released this before ({relative releasedAt}). Add it again?"** with the action **"Add again"**, which reopens the dialog prefilled, with a **new** `jobId`. |
| **400** `invalid-source` | Inline on the input: **"debroxy couldn't read this {magnet link\|file\|link}."** |
| **400** `upload-too-large` | Inline on the file: **"This file is larger than {limit}."** |
| **400** `unsupported-hoster` | Inline on the URL: **"{provider} doesn't support this site."** |
| **400** `provider-rejected` | Inline alert: **"{provider} refused this: {message}"** |
| **400** `invalid-request` | Inline alert: **"Something in this request is invalid: {message}"** |
| **404** `provider-not-found` | Refetch `['providers']`; inline alert **"This provider no longer exists. Pick another."** |
| **409** `job-already-exists` | Inline alert **"You already have this."** with the link **"Open it"**. The client finds the job in `['jobs']` by refetching and matching the fingerprint; it cannot know the ID, so the link opens `/` filtered by the name instead (OQ-FE-05). |
| **409** `job-id-conflict` | Generate a new `jobId` and retry once automatically; if it repeats, show the generic error. |
| **409** `provider-disabled`, `capability-unavailable-for-plan`, `provider-account-limit`, `remote-item-unavailable`, `remote-item-being-deleted` | Inline alert per §7.2; `remote-item-unavailable` and `remote-item-being-deleted` also offer **"Try again"** (same `jobId`). |
| **429** `slot-quota-exceeded` | Inline alert **"All {max} of your slots are in use. Release a job to add another."** with the link **"Show my active jobs"** (`/?group=active`). |
| **429** `submit-rate-exceeded` | Inline alert **"You've reached {perHour} submissions this hour. Try again in {mm:ss}."** with the countdown from `retryAfter`; the button stays disabled until 0. |
| **501** `capability-not-supported`, `capability-not-enabled` | Inline alert per §7.2; refetch `['providers']`. |
| **502** `provider-unavailable` | Inline alert **"{provider} isn't responding. Try again{ in {s}s}."** with the **"Try again"** button (same `jobId`), enabled after `retryAfter` if present. |
| **503** | Generic retryable error (§7.2). |
| Network error | **"Couldn't reach debroxy. Your job may still have been added."** with **"Try again"** (same `jobId`, so it is safe). |

4. **Closing:** Escape or ✕ closes the dialog without a prompt; a typed but unsent input is discarded. While a request is in flight, closing is allowed. The request continues, and its outcome shows as a toast.

#### 5.5.6 Pre-submit info line

"Uses 1 of your {max} slots · {left} of {perHour} submits left this hour". After a positive cached check it reads "{left} of {cachedPerHour} cached submits left". The client cannot predict whether a submit joins content someone else already added (joins are free, ADR 0013 rule 17), so the line always shows the cost of a new item, and its tooltip says **"Joining content someone else already added doesn't use a slot or a submission."**

### 5.6 Job detail (`/jobs/$jobId`)

#### 5.6.1 Layout

```
┌──────────────────────────────────────────────────────────────────────────────────────────┐
│ ← Jobs                                                                                   │
│ Sintel ✎                                                    [Copy all links ▾] [Release] │
│ ✓ Ready · TorBox · 123 MiB · added 2 h ago · expires in 5 d · ⇄ shared · counts toward  │
│ ⚠ Not up to date …                                                     (stale, §5.3.4)   │
├─[ Files (3) ]─[ Details ]────────────────────────────────────────────────────────────────┤
│ Download as one archive: ZIP · 123 MiB · resuming not supported   [Download] [Copy link] │
│                                                                                          │
│ ▾ 📁 Sintel                                    123.3 MiB                                  │
│     ▶ Sintel.mp4                               123.2 MiB   [▶ Play] [⧉ Copy] [⤓ Download] │
│       Sintel.srt                                 2.1 KiB          [⧉ Copy] [⤓ Download] │
│   ▾ 📁 Extras                                    0.1 MiB                                  │
│       poster.jpg                                98.0 KiB          [⧉ Copy] [⤓ Download] │
│                                                                                          │
│ Links expire 6 h after they are made. They work without logging in: share them carefully│
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

#### 5.6.2 Header and Details tab

| Element | Source | Notes |
|---|---|---|
| Title | `displayName` | an inline rename button ✎ (§5.6.3) |
| State badge | `state`, `telemetry.stalled` | §6.1 |
| Provider | `provider` → display name | |
| Size | `sizeBytes` | §6.6 |
| Added | `createdAt` | relative + tooltip |
| Expires | `expiresAt` | §6.4 |
| Shared | `shared` | the badge with the §2 explanation as a tooltip |
| Counts toward quota | `slotCharged` | the badge; false shows **"doesn't count toward your quota"** in the tooltip of a muted badge |
| Progress panel (active states) | `telemetry.progress`, `bytesPerSecond`, `etaSeconds`, `peers`, `stalled` | a large bar; speed, ETA and peers; when `stalled`: **"No progress for a while. The provider may be struggling to find sources."** |
| Failure panel (`failed`, `gone`) | `failureReason` | §6.3 text, plus what the user can do. Release stays possible. |
| Kind | `kind` | Details tab: "Torrent", "Usenet", "Web link" |
| Job ID | `id` | Details tab, monospace, with a copy button |
| Delivery | `delivery.perFile`, `delivery.bundle.format`, `delivery.bundle.rangeSupported` | Details tab: "Single files: yes/no · Archive: ZIP/RAR · Resumable archive: yes/no" |
| Stale | `stale` (single job) | the banner as in §5.3.4 |

- **`getJob` 404 `job-not-found`:** the page shows **"This job doesn't exist or isn't yours."** with **"Back to jobs"**. The job is removed from `['jobs']`. The same applies when a `job.released` event arrives for this job while the page is open. Then the page shows **"This job was released."**, plus, if the event carried a `historyEntry`, its final state and the link **"See history"**.

#### 5.6.3 Rename

- **Dialog:** a single field **"Name"**, prefilled with `displayName`, trimmed, 1–255 characters (FE-25). Save → `renameJob {displayName}`.
- **200:** update the cache from the returned Job; toast **"Renamed."**
- **400:** inline message per §7.2.
- **404:** as above.

#### 5.6.4 Release

The confirmation dialog:
- Title: **"Release {displayName}?"**
- Body when `shared`: **"Others also use this content. It stays at the provider for them; it just leaves your list."**
- Body otherwise: **"Nobody else uses this content, so debroxy will delete it at {provider}. Download links stop working."**
- When `state` is active: an extra line, **"It isn't finished yet."**
- When `slotCharged`: an extra line, **"This frees one of your slots."**
- Buttons: **"Cancel"** and **"Release"** (destructive style; focus starts on Cancel).

`releaseJob`:
- **204:** remove the job from `['jobs']`, drop `['job', id]` and `['links', id, …]`, invalidate `['me']` and `['history']`, toast **"Released {displayName}."**. On the detail page, navigate to `/`.
- **404:** treat as already released (same handling, toast **"Already released."**).

#### 5.6.5 Files tab (FE-19)

- **Tree:** built from `files[].path` split on `/`. Folders are sorted before files, then by name (locale compare, numeric: `Intl.Collator(undefined, {numeric:true})`). Folder rows show the sum of the sizes below them. Folders with one child folder collapse into one row ("Sintel/Extras"). All folders start expanded when there are ≤ 50 files; otherwise only the first level is expanded.
- **File row:** name, size, and the actions below.
- **No files yet** (`files` empty, state active): **"The file list appears once {provider} knows it."**
- **Per-file actions** (shown only when `delivery.perFile` and `state === "ready"`):
  - **"Play"**: only for media files (§6.7); opens the player (§5.7).
  - **"Copy link"**: §5.6.6.
  - **"Download"**: issues the link and then navigates `window.location.assign(url)`. The server's `Content-Disposition: attachment` makes this a download without leaving the page.
- **Archive bar** (shown when `delivery.bundle` is present and `state === "ready"`): **"Download as one archive: {ZIP|RAR} · {sizeBytes}"**.
  - When `rangeSupported` is false, it adds **"· resuming not supported"** with the tooltip **"If the download breaks, it starts over."** (ADR 0011).
  - A RAR bundle adds **"· unpack with an archive tool after downloading"** (server OQ-13).
  - Buttons **"Download"** and **"Copy link"** (target `{bundle: true}`).
- **`perFile` false and `bundle` present** (Real-Debrid multi-file): the file rows show no actions, and the archive bar has the note **"{provider} delivers this job only as one archive."**
- **Not `ready`:** actions are hidden. The tab shows **"Downloads are available once the job is ready."**

#### 5.6.6 Signed Links: issuing, caching, expiry, export (FE-18, FE-20)

- **Issue one** → `issueSignedLink(jobId, {target: {fileId} | {bundle: true}})` → `SignedLink {url, expiresAt, rangeSupported, fileId?, bundle?}`.
  - `url` is absolute (built from the server's public base URL). The client uses it as given and never rewrites it.
  - It is cached under `['links', jobId, key]` and treated as **fresh until `expiresAt − 60 s`**. Any later use (copy, download, play) re-issues it automatically when it is stale. That is the only "refresh" of a link.
- **Issue all** → `issueSignedLinksForAllFiles(jobId)` → `SignedLinkList.links` (one per file in stored order, or one bundle link). It is cached under `['links', jobId, 'all']`, and each link is also cached per file.
- **Copy link:** writes `url` to the clipboard (`navigator.clipboard.writeText`) and toasts **"Link copied. It works for anyone who has it until {exact expiresAt time}."**
- **Copy all links** (menu): the button's dropdown has
  - **"Copy all links"**: newline-separated URLs;
  - **"Download link list (.txt)"**: a file named `{displayName}.txt` with one URL per line;
  - **"Download for aria2 (.aria2)"**: a file named `{displayName}.aria2`, one entry per link:
    ```
    {url}
      out={file path relative to the job, or the archive name}
    ```
    `out` uses the file's `path`. The file is generated with a `Blob` and an `<a download>` click; nothing goes to the server.
  - **"Download for JDownloader (.txt)"**: the same as the plain list. JDownloader reads plain URL lists, and the link's own `{name}` path segment carries the file name.
- **Expiry display:** the bottom note of the files tab, **"Links expire {config.signedLinkTTLSeconds as "6 h"} after they are made. They work without logging in: share them carefully."**. In the copy menu, each cached link shows **"expires {relative}"**.
- **Errors:**
  - `409 remote-item-not-ready` / `target-not-available`: toast per §7.2, then refetch `['job', id]`.
  - `404 job-not-found`: as §5.6.2.
  - `400 invalid-request`: shown as a generic error; it means a client bug, because the target must have exactly one of `fileId` or `bundle`.

### 5.7 Media player (FE-20)

- **Opened** from a file row's **"Play"**. A large modal dialog (`max-width: 1200px`, 16:9 area) with the file name as its title.
- **Source:** a fresh Signed Link for the file (re-issued if it expires within 10 min), used as `<video src>` or `<audio src>` by media type (§6.7). `preload="metadata"`, native `controls`, `playsInline`.
- **Warning:** for types browsers may not decode (§6.7 "maybe" column), a banner above the player reads **"Your browser may not be able to play {extension} files. If it doesn't start, use Download."**
- **Seeking** needs Range support. A per-file link has `rangeSupported: true` for file targets. When `rangeSupported` is false, the client shows **"Seeking isn't supported for this file."**
- **Errors:**
  - The media element's `error` event shows, in place of the video, **"This file can't be played here."** with the buttons **"Download instead"** and **"Copy link"**.
  - The server's `503 too-many-streams` (the media element cannot read the body) also fires `error`. The player then calls `fetch(url, {method:"HEAD"})`. On 503 with `retryAfter`, it shows **"You have too many downloads running. Close one, or wait {s}s."** with an automatic retry after `retryAfter`. On 403 (`signed-link-expired`), it re-issues the link and retries once.
- **Closing** the dialog pauses and unloads the element (`src` removed, `load()` called), so the stream slot is freed (server DEC-18).
- **Subtitles:** sibling `.srt`/`.vtt` files are not loaded in v1 (OQ-FE-06).

### 5.8 History (`/history`)

```
┌────────────────────────────────────────────────────────────────────────────────────┐
│ History                                                 [🔍 filter by name      ]  │
├──────────────────────────────┬──────────┬──────────┬───────────────┬───────────────┤
│ Name                         │ Provider │ Ended as │ Added         │ Released    ▼ │
├──────────────────────────────┼──────────┼──────────┼───────────────┼───────────────┤
│ Sintel                       │ TorBox   │ ✓ Ready  │ 3 d ago       │ 2 d ago       │
│   Sintel (provider name)     │          │          │               │               │
│ big-buck-bunny               │ TorBox   │ ✕ Failed │ 5 d ago       │ 5 d ago       │
│   Provider rejected it       │          │          │               │               │
└──────────────────────────────┴──────────┴──────────┴───────────────┴───────────────┘
```

| Column | Source | Rendering |
|---|---|---|
| Name | `displayName`; second line `name` when it differs | the job's own name, then the content's provider-side name, both as text |
| Provider | `provider` | display name |
| Kind | `kind` | an icon before the name (torrent / web / usenet) |
| Ended as | `finalState`; `failureReason` | the state badge (§6.1) of the state the item had at release; for `failed`/`gone`, the §6.3 text as a second line |
| Added | `claimedAt` | relative + tooltip |
| Released | `releasedAt` | relative + tooltip; the default sort is descending |

`HistorySort = z.enum(["name","provider","finalState","claimed","released"])`, plus `dir`. There are no actions; entries are read-only (server §5.4). The empty state reads **"Nothing released yet."** `job.released` events prepend their `historyEntry` (§8.3).

### 5.9 Account (`/account`)

```
┌ Account ──────────────────────────────────────────────────────────┐
│ alice · User · active · member since 12 Oct 2026                  │
│ Sign-in methods: Password ✓ · Single sign-on ✓                     │
│ Session: idle logout {relative idleExpiresAt} · ends {absolute}    │
│ Slots: 2 of 3 · Submits: 3 of 10 this hour · cached 0 of 60        │
│                                                                   │
│ Change password                                                   │
│ Current password [________________]                               │
│ New password     [________________]   at least 12 characters      │
│ Repeat new       [________________]                               │
│ ⓘ Changing your password logs you out everywhere else.            │
│                                         [ Change password ]       │
└───────────────────────────────────────────────────────────────────┘
```

- **Fields:**
  - from `me.user`: `username`, `role`, `status`, `createdAt`, `passwordLogin`, `oidcLinked`
  - from `me.session`: `idleExpiresAt`, `absoluteExpiresAt`
  - `me.quota` and `me.submitRate`
- **The change-password form** is shown only when `me.user.passwordLogin` is true. Otherwise the page reads **"You sign in with single sign-on. You have no password here."**
  - **Zod rules:**
    - `currentPassword` is required;
    - `newPassword` has at least `config.passwordMinLength` characters (rev 13; default 12) and at most 1024 **bytes** (UTF-8, measured with `TextEncoder`);
    - the repeat must equal it.
  - **Messages:** **"Use at least {n} characters."**, **"That's too long."**, **"The passwords don't match."**
  - **Submit** → `changeOwnPassword {currentPassword, newPassword}`:
    - **204:** toast **"Password changed. Other sessions were logged out."** The current session stays valid (server §4.4); `['me']` is invalidated.
    - **400 `wrong-current-password`:** inline on the current field, **"That isn't your current password."**
    - **400 `invalid-password`:** inline on the new field, with the server `message`.

### 5.10 Admin: users (`/admin/users`, `/admin/users/new`)

```
┌ Users ─────────────────────────────────────────────── [ + New user ] ┐
│ [🔍 filter by username]                                               │
├──────────────┬────────┬──────────┬──────────┬──────────┬─────────────┤
│ Username   ▲ │ Role   │ Status   │ Password │ SSO      │ Created     │
├──────────────┼────────┼──────────┼──────────┼──────────┼─────────────┤
│ alice        │ Admin  │ ● Active │ ✓        │ —        │ 12 Oct 2026 │
│ bob          │ User   │ ○ Disabl.│ ✓        │ ✓        │ 13 Oct 2026 │
└──────────────┴────────┴──────────┴──────────┴──────────┴─────────────┘
```

- **List** → `listUsers` (ordered by username on the server; the client may re-sort). Columns are `username`, `role`, `status`, `passwordLogin`, `oidcLinked` and `createdAt`; a click opens the user's detail page. "(you)" marks the current user.
- **New user** (dialog route `/admin/users/new`):
  - **Fields:**
    - **"Username"**: Zod 3–32 characters of `a-z 0-9 . _ -` after trimming and lowercasing, mirroring server `NewUsername`. The client lowercases as the admin types and shows a preview.
    - **"Role"**: User/Admin, default User.
    - **"Sign-in"**: checkboxes **"Password"** (default on) and **"Single sign-on"** (shown only when `config.oidcEnabled`).
    - With Password on: **"Password"** and **"Repeat"**, with the §5.9 rules, plus a **"Generate"** button. It generates 20 random characters from `crypto.getRandomValues` over an unambiguous alphabet, fills both fields and reveals the password once, with **"Copy"**.
    - With Single sign-on on: **"Issuer"** (an absolute https URL; help: **"Exactly the iss value of your identity provider, e.g. https://accounts.example.org"**) and **"Subject"** (required; help: **"The user's sub claim at that provider."**).
  - **Validation:** at least one sign-in method; otherwise **"Choose at least one way to sign in."** (mirrors `no-login-method`).
  - **Submit** → `createUser {username, role, password?, oidcIdentity?}`:
    - **201:** close the dialog, write the user into `['admin','users']`, toast **"Created {username}."**, navigate to its detail page.
    - **409 `username-taken`:** inline on username. **409 `oidc-identity-taken`:** inline on subject.
    - **400 `invalid-username` / `invalid-password` / `invalid-role` / `invalid-oidc-identity` / `no-login-method`:** inline on the matching field (§7.2).

### 5.11 Admin: user detail (`/admin/users/$userId`)

```
┌ ← Users ───────────────────────────────────────────────────────────────┐
│ bob · created 13 Oct 2026                                              │
│                                                                        │
│ Role     ( ) User  (•) Admin                              [ Save ]     │
│ Status   (•) Active ( ) Disabled                                       │
│ ⓘ Disabling logs bob out everywhere and stops their download links.   │
│                                                                        │
│ Password            set ✓                       [ Reset password… ]    │
│ Single sign-on      https://accounts.example.org · 2f9a…   [ Unlink ]  │
│                     (or) not linked                        [ Link… ]   │
│ Slot quota          [ 3 ] active jobs              [ Set ]             │
│ ⓘ The current quota can't be read here; setting it replaces it.       │
│ Jobs                [ Show bob's jobs → ]  (/admin/jobs?owner=bob)     │
└────────────────────────────────────────────────────────────────────────┘
```

- **Data** → `getUser(userId)`. **404 `user-not-found`:** **"This user doesn't exist."**
- **Role and status** → `updateUser {role?, status?}` with only the changed members.
  - **200:** update the caches, toast **"Saved."**
  - **409 `last-admin`:** **"bob is the last active admin. Make someone else an admin first."** The server applies role and status as two commands, role first and not atomically (server §4.4). After any error the client therefore refetches the user and shows what was actually saved.
  - **Editing yourself:** the admin sees the warning **"You're changing your own account."** Demoting yourself, then confirming, invalidates `['me']`; the admin section disappears and the client navigates to `/`.
  - **Disabling** shows a confirmation: **"Disable {username}? They're logged out at once and their download links stop working. Their jobs stay."** (server: links of disabled users, §7.5).
- **Password** row: "set ✓" when `passwordLogin`, otherwise **"none"**.
  - **"Reset password…"** opens a dialog with the new password, repeat and Generate (the §5.10 rules) → `resetUserPassword {password}`.
  - **204:** toast **"Password reset. {username} was logged out everywhere."** `passwordLogin` becomes true, so the user is refetched.
- **Single sign-on** row: the user object does not expose the linked issuer and subject; only `oidcLinked` is known. The row therefore reads **"linked"** or **"not linked"** (OQ-FE-07).
  - **"Link…"** (or **"Change…"** when linked) → a dialog with Issuer and Subject → `linkOIDCIdentity {issuer, subject}`. **200** updates the user; **409 `oidc-identity-taken`** shows **"Another user already has this identity."**
  - **"Unlink"** confirms, then `unlinkOIDCIdentity`. **200** updates the user; **400 `no-login-method`** shows **"{username} would have no way to sign in. Set a password first."**
  - When `config.oidcEnabled` is false, the row shows the state but the buttons are disabled, with **"Single sign-on is off on this server."**
- **Slot quota** → `setSlotQuota {maxActiveJobs}`, an integer ≥ 0 (Zod) where 0 means "can't add jobs".
  - **204:** toast **"Quota set to {n}."**
  - The field starts **empty**, with the placeholder **"current value not readable"**: neither the user's quota nor the instance default is exposed (OQ-FE-01).
  - **400 `invalid-slot-quota`:** inline error.
- **Jobs** link → `/admin/jobs?owner={username}`.

### 5.12 Admin: all jobs (`/admin/jobs`)

- Same table as §5.3, plus the column **"Owner"** (`ownerUsername`, sortable). The `?owner` filter is an exact match on the username.
- **Data:** `listAllJobs`, then kept live by the `scope=all` Event Stream (§8.2), which is opened only while this page is mounted.
- **Actions:** **"Open"** is not offered for other users' jobs, because `getJob` is owner-only (server §5.4); a click on the row expands an inline detail drawer that renders the same header, progress and file tree **read-only** from the list item. **"Release…"** → `releaseAnyJob(jobId)`; its dialog adds **"This is {owner}'s job."** **204** removes the job from `['admin','jobs']`, and from `['jobs']` too if the job is the admin's own.
- **Stale banner:** as in §5.3.4, from `JobList.stale`.

### 5.13 Admin: providers (`/admin/providers`)

```
┌ Providers ────────────────────────────────────────────────────────────────────────┐
│ [ TorBox ] [ Real-Debrid ]                                                         │
│ TorBox · torbox · ● enabled · ⚠ not up to date                                     │
│ [ ⟳ Refresh now ]  [ Re-test unavailable capabilities ]                             │
│                                                                                    │
│ Capability              Provider has it   debroxy uses it   Learned state           │
│ Add magnet links        ✓                 ✓                 ● available             │
│ Add .torrent files      ✓                 ✓                 ● available             │
│ Add web links           ✓                 ✓                 ◌ not yet tested        │
│ Add NZB files           ✓                 ✕ (not in v1)     —                       │
│ Single-file downloads   ✓                 ✓                 ● available             │
│ Archive downloads       ✓                 ✓                 ✕ unavailable (plan)    │
│ Cached check            ✓                 ✓                 ● available             │
│ Webhooks                ✓                 ✓                 —                       │
│ Deduplicates content    ✓                 ✓                 —                       │
│                                                                                    │
│ Supported sites (124)                              [🔍 filter]   [ Reload ]         │
│ Site         Domains                         Status   Today                         │
│ 1fichier     1fichier.com, alterupload.com   ● up     12 / 50                       │
└────────────────────────────────────────────────────────────────────────────────────┘
```

- **Tabs:** one per provider in `providers[]`, with `displayName` and `kind`. `enabled` shows as a badge; a disabled provider reads **"disabled in the configuration"**.
- **Capability matrix:** `capabilities[]` → name (human label, §6.5), `supported`, `enabled`, `state`.
  - Labels per `state`: `available` → "available"; `unavailable` → "unavailable"; `unverified` → "not yet tested"; absent → "—".
  - **Not in v1:** `supported && !enabled` shows **"✕ (not in v1)"**.
- **Refresh now** → `refreshProvider`, with the outcomes of §5.3.5.
- **Re-test unavailable capabilities** → `probeProviderCapabilities`.
  - **204:** toast **"debroxy will test unavailable capabilities again the next time they're used."**, then refetch `['providers']`.
  - **404:** refetch.
- **Hosters** → `listHosters` (the web capability only; otherwise the section reads **"This provider doesn't take web links."**).
  - **Columns:** `name`, `domains[]` (comma list, wrapping), `up` (● up / ○ down), and "{dailyUsed} / {dailyLimit} today" when present, as plain numbers; the unit depends on the provider (OQ-FE-08).
  - **Errors:** `501 capability-not-supported` reads **"No site list for this provider."**; `409 provider-disabled` reads **"Provider disabled."**; `503`/`502` read **"Can't load the site list right now."** with Reload.

### 5.14 Admin: Unmanaged Items (`/admin/unmanaged`)

- **Explanation at the top:** **"These items are on the provider accounts but weren't added through debroxy. debroxy never deletes them. If someone uses the account directly, they show up here. Remove them in the provider's own website if you don't need them."**
- **Table** from `items[]`:
  - Provider (`provider`), Name (`name`, as text)
  - Kind (`kind`), Size (`sizeBytes`), Provider state (`state`, the raw provider-side value, shown monospace)
  - Provider item ID (`providerItemId`, monospace, copy button)
  - First seen (`firstSeenAt`) and Last seen (`lastSeenAt`), both relative
- **Filters and refresh:** a provider filter (`?provider`) and a **"Reload"** button. There are no row actions (server OQ-01).
- **Empty state:** **"Nothing unmanaged. The provider accounts contain only what debroxy added."**

### 5.15 Admin: stuck deletions (`/admin/outbox`)

- **Explanation:** **"debroxy tried to delete these items at the provider and gave up after {attempts} attempts. They still exist there and count against the account's limits. Delete them in the provider's website."**
- **Table** from `messages[]`:
  - ID (`id`), Kind (`kind`, raw), Remote item (`remoteItemId`, monospace, copy)
  - Attempts (`attempts`)
  - Last error (`lastError`, monospace, wrapped, as text)
  - Created (`createdAt`) and Gave up (`parkedAt`)
- **Actions:** **"Reload"** only; no row actions.
- **Empty state:** **"No stuck deletions."**
- The sidebar count (§4.2) comes from this list.

### 5.16 Not found and forbidden

- **Unknown route:** **"Page not found"**, then **"There's nothing at {path}."**, and the button **"Go to jobs"**.
- **Admin route opened by a non-admin:** **"Admins only"**, then **"You don't have access to this page."**, and **"Go to jobs"**. The client never requests admin data for a non-admin.

---

## 6. Domain presentation rules

### 6.1 States (all 11, plus `stalled`)

The server's job `state` values, in the order the "State" column sorts (§5.3.3):

| `state` | Label | Group | Icon (lucide) | Colour token | Meaning (tooltip) |
|---|---|---|---|---|---|
| `submitting` | Adding | active | `loader-circle` (spinning) | `--state-pending` | "debroxy is handing this to {provider}." |
| `queued` | Queued | active | `clock` | `--state-pending` | "Waiting at {provider} to start." |
| `resolving_metadata` | Finding files | active | `search` (pulse) | `--state-pending` | "{provider} is fetching the torrent's file list." |
| `awaiting_file_selection` | Selecting files | active | `list-checks` | `--state-pending` | "{provider} is selecting all files; this resolves by itself." |
| `downloading` | Downloading | active | `arrow-down-circle` | `--state-active` | "{provider} is downloading it." |
| `processing` | Processing | active | `cog` (spinning) | `--state-active` | "{provider} is preparing the files." |
| `ready` | Ready | ready | `check-circle-2` | `--state-ready` | "Ready to download." |
| `failed` | Failed | problems | `x-circle` | `--state-failed` | the failure text (§6.3) |
| `gone` | Gone | problems | `circle-off` | `--state-failed` | "It disappeared from {provider}." |
| `deleting` | Removing | problems | `trash-2` | `--state-muted` | "debroxy is deleting it at {provider}." |
| `deleted` | Removed | problems | `trash` | `--state-muted` | "Deleted at {provider}." |

- **Stalled** (`telemetry.stalled: true`, active states only): the badge keeps its label, gains a suffix **"· stalled"**, and uses the `--state-warning` colour; its tooltip reads **"No progress for a while."**
- **Unknown values** (a future server state): the label is the raw value in title case, the group is *active*, the icon is `help-circle` and the colour is `--state-muted`. The client MUST NOT crash.
- **Group chips** (§5.3) count by group; *all* counts every job.

### 6.2 `shared` and `slotCharged`

| Combination | Badges | Tooltip |
|---|---|---|
| `shared` + `slotCharged` | "⇄ shared", "counts toward quota" | "Others also use this; it counts toward your quota because you added it first." |
| `shared` + not charged | "⇄ shared" | "Others also use this; it counts toward someone else's quota while they keep it." |
| not shared + charged | "counts toward quota" | "This uses one of your slots." |
| neither | — | — (for example `ready`; ready items don't count) |

The client never infers *who* shares (NFR-04). The words above are derived from ADR 0006 and say nothing about identities.

### 6.3 Failure reasons (`failureReason` codes)

| Code | Text | What the user can do (second line) |
|---|---|---|
| `provider-rejected` | "{provider} refused it." | "Check the source, or try another provider." |
| `provider-failed` | "{provider} couldn't finish it." | "Release it and try again later, or try another provider." |
| `submission-lost` | "It never showed up at {provider}." | "Release it and add it again." |
| `merged-into` | "{provider} merged it into another item you already have." | "Your job moved there; release this one." |
| `merged-into-refused` | "{provider} merged it into an item debroxy couldn't attach you to." | "Release it and add it again." |
| `blocked-by-deletion` | "The same content was being deleted when this arrived." | "Add it again in a minute." |
| *(null)* with `failed` | "It failed." | "Release it and try again." |
| *(unknown code)* | "It failed ({code})." | "Release it and try again." |

### 6.4 Expiry (`expiresAt`)

When the provider deletes the content (for example, TorBox's retention):
- null → nothing shown;
- more than 24 h away → muted "expires {relative}";
- less than 24 h → amber;
- less than 1 h → red **"expires {relative}"**;
- past → red **"expired"** (the next refresh will mark it `gone`).

### 6.5 Capability labels

| Name | Label | Used by |
|---|---|---|
| `SubmitMagnet` | Add magnet links | the Magnet tab |
| `SubmitTorrentFile` | Add .torrent files | the .torrent tab |
| `SubmitWebURL` | Add web links | the Web tab, the hoster list |
| `SubmitNZBFile` | Add NZB files | (hidden tab, §5.5.3) |
| `SubmitNZBURL` | Add NZB links | (hidden tab) |
| `PerFileDelivery` | Single-file downloads | informational (the job's `delivery.perFile` decides per job) |
| `BundleDelivery` | Archive downloads | informational (`delivery.bundle` decides per job) |
| `CachedCheck` | Cached check | §5.5.4 |
| `Webhooks` | Webhooks | informational |
| `ProviderDedup` | Deduplicates content | informational, tooltip **"Adding the same content twice gives the same item."** |
| *(unknown)* | the raw name | informational |

### 6.6 Number and time formats (FE-23)

| Value | Rule | Examples |
|---|---|---|
| Size (bytes) | IEC, base 1024; B, KiB, MiB, GiB, TiB; 3 significant digits (≥ 100 → 0 decimals, ≥ 10 → 1, else 2); `Intl.NumberFormat("en")`; 0 → "0 B" | 754 MiB · 12.3 GiB · 2.15 KiB |
| Speed (bytes/s) | size + "/s"; 0 → "—" | 12.3 MiB/s |
| ETA (seconds) | < 60 s "41s"; < 1 h "12m 05s"; < 24 h "3h 12m"; else "2d 4h"; null → "—" | |
| Progress | `progress × 100`, one decimal, clamped to 0–100 | 62.4% |
| Relative time | `Intl.RelativeTimeFormat("en", {numeric:"auto"})` with the largest unit (s, min, h, d, mo, y); refreshed every 30 s | "2 h ago", "in 5 d", "now" |
| Exact time (tooltips, banners) | `Intl.DateTimeFormat(undefined, {dateStyle:"medium", timeStyle:"short"})`, browser locale and time zone | "13 Oct 2026, 14:35" |
| Countdown | `m:ss`, or `h:mm:ss` from 1 h | 0:42 |
| Durations from config | `signedLinkTTLSeconds` → the largest whole unit ("6 h") | |

Numbers in tables use `font-variant-numeric: tabular-nums`, and sizes and speeds are right-aligned.

### 6.7 Media types (FE-20, "all media, warn on others")

Decided by the file extension (case-insensitive) in `path`:

| Plays natively (no warning) | May not play (warning shown) | Element |
|---|---|---|
| mp4, m4v, webm, mov | mkv, avi, wmv, flv, mpg, mpeg, ts, m2ts, 3gp, ogv | `<video>` |
| mp3, m4a, aac, ogg, oga, opus, flac, wav | wma, ape, alac, aiff, dsf | `<audio>` |

Every other extension gets no **"Play"** action.

---

## 7. Error handling

### 7.1 Behaviour kinds

| Kind | Meaning |
|---|---|
| **inline** | shown next to the form field or inside the dialog that caused it; focus moves to the field; `aria-invalid` + `aria-describedby` |
| **alert** | an inline alert box at the top of the dialog or page section (role `alert`) |
| **toast** | a sonner toast, 6 s (errors: 10 s, dismissable), bottom right; at most 3 visible, newest on top |
| **banner** | a persistent strip under the page header until resolved |
| **countdown** | inline or alert, with a live `m:ss` from `retryAfter` (rev 13); the triggering control is disabled until it reaches 0 |
| **redirect** | navigation as stated |
| **page** | the route's content is replaced by an error state (§7.5) |

### 7.2 Slug table (every slug the server can send)

"Where" names the operations that can return it; "Title" is bold text, and "Text" follows it. `{message}` is the server's message, shown only where stated, because server messages are for operators.

| Slug | HTTP | Where | Title / text | Behaviour |
|---|---|---|---|---|
| `invalid-request` | 400 | any body or path parameter | **"Invalid request."** "Something in this request is invalid: {message}" | alert; also reported as a client bug in dev builds (console) |
| `invalid-source` | 400 | `submitJob` | **"Can't read this."** "debroxy couldn't read this {magnet link\|file\|link}." | inline on the source field |
| `upload-too-large` | 400 | `submitJob` | **"File too large."** "This file is larger than {uploadLimitBytes}." | inline |
| `unsupported-hoster` | 400 | `submitJob` (web) | **"Site not supported."** "{provider} doesn't support this site." | inline on the URL |
| `provider-rejected` | 400 | `submitJob` | **"Refused by {provider}."** "{provider} refused this: {message}" | alert |
| `invalid-username` | 400 | `createUser` | **"Invalid username."** "Use 3–32 characters: letters, digits, dot, dash, underscore." | inline |
| `invalid-password` | 400 | `createUser`, `resetUserPassword`, `changeOwnPassword` | **"Invalid password."** "Use at least {passwordMinLength} characters and at most 1024 bytes." | inline |
| `invalid-role` | 400 | `createUser`, `updateUser` | **"Invalid role."** "Pick User or Admin." | inline (client bug if seen) |
| `invalid-status` | 400 | `updateUser` | **"Invalid status."** "Pick Active or Disabled." | inline (client bug if seen) |
| `invalid-oidc-identity` | 400 | `createUser`, `linkOIDCIdentity` | **"Invalid identity."** "The issuer must be an https URL and the subject must not be empty." | inline |
| `wrong-current-password` | 400 | `changeOwnPassword` | **"Wrong password."** "That isn't your current password." | inline on current password |
| `no-login-method` | 400 | `createUser`, `unlinkOIDCIdentity` | **"No way to sign in."** "{username} would have no way to sign in. Set a password first." | alert |
| `invalid-slot-quota` | 400 | `setSlotQuota` | **"Invalid quota."** "Enter a whole number, 0 or more." | inline |
| `webhook-too-large` | 400 | (webhook only) | — | never reaches the client |
| `invalid-credentials` | 401 | `logIn` | **"Wrong username or password."** | inline (form level) |
| `authentication-required` | 401 | any protected call | — | session-ended flow (§7.4) |
| `session-expired` | 401 | any protected call | — | session-ended flow (§7.4) |
| `forbidden` | 403 | admin calls, `getUser`, cookie `Origin` mismatch, `events?scope=all` | **"Not allowed."** "You don't have permission to do this." | toast; on admin pages: refetch `['me']` (the role may have changed) and show the 403 page if no longer admin |
| `invalid-signed-link` | 403 | `/dl` (player HEAD) | **"Link invalid."** "This link is broken." | player: re-issue once |
| `signed-link-expired` | 403 | `/dl` (player HEAD) | **"Link expired."** | player: re-issue once, silently |
| `job-not-found` | 404 | job, link and release calls | **"Job not found."** "This job doesn't exist or isn't yours." | page on detail; elsewhere toast + remove from cache |
| `provider-not-found` | 404 | provider calls, `submitJob` | **"Provider not found."** "This provider no longer exists." | alert/toast; refetch `['providers']` |
| `user-not-found` | 404 | admin user calls | **"User not found."** | page on user detail; toast elsewhere + refetch the list |
| `not-found` | 404 | unknown API path | **"Not found."** | generic error (client bug) |
| `job-already-exists` | 409 | `submitJob` | **"Already added."** "You already have this." | alert with **"Show my jobs"** (§5.5.5) |
| `job-id-conflict` | 409 | `submitJob` | — | automatic retry with a new ID (§5.5.5); after a second time, the generic error |
| `remote-item-being-deleted` | 409 | `submitJob` | **"Being removed."** "This content is being deleted at {provider}. Try again in a minute." | alert + **"Try again"** |
| `remote-item-unavailable` | 409 | `submitJob` | **"Changed meanwhile."** "This content changed while adding. Try again." | alert + **"Try again"** |
| `remote-item-not-ready` | 409 | link calls | **"Not ready yet."** "Downloads are available once the job is ready." | toast; refetch the job |
| `target-not-available` | 409 | link calls | **"Not available."** "This file or archive isn't available for download." | toast; refetch the job |
| `last-admin` | 409 | `updateUser` | **"Last admin."** "{username} is the last active admin. Make someone else an admin first." | alert |
| `username-taken` | 409 | `createUser` | **"Username taken."** "Pick another username." | inline |
| `oidc-identity-taken` | 409 | `createUser`, `linkOIDCIdentity` | **"Identity in use."** "Another user already has this identity." | inline on subject |
| `provider-disabled` | 409 | `submitJob`, `listHosters`, `checkCachedAvailability` | **"Provider disabled."** "{provider} is turned off on this server." | alert; refetch `['providers']` |
| `provider-account-limit` | 409 | `submitJob` | **"Provider account full."** "The {provider} account has reached its limit. Ask an admin." | alert |
| `capability-unavailable-for-plan` | 409 | `submitJob`, link calls, `listHosters` | **"Not on this plan."** "The {provider} account's plan doesn't include this." | alert; refetch `['providers']` |
| `bootstrap-not-allowed` | 409 | (command line only) | — | never reaches the client |
| `slot-quota-exceeded` | 429 | `submitJob` | **"No free slots."** "All {max} of your slots are in use. Release a job to add another." | alert + link to active jobs |
| `submit-rate-exceeded` | 429 | `submitJob` | **"Submit limit reached."** "You've reached {perHour} submissions this hour. Try again in {m:ss}." | countdown |
| `refresh-rate-exceeded` | 429 | `refreshProvider` | **"Slow down."** "You can refresh again in {s}s." | countdown on the button |
| `login-throttled` | 429 | `logIn` | **"Too many attempts."** "Try again in {m:ss}." | countdown |
| `capability-not-supported` | 501 | `submitJob`, `listHosters`, `checkCachedAvailability` | **"Not supported."** "{provider} can't do this." | alert; hide the tab after refetching `['providers']` |
| `capability-not-enabled` | 501 | `submitJob` | **"Not enabled."** "debroxy doesn't offer this yet." | alert |
| `oidc-not-enabled` | 501 | `startOIDCLogin` | — | not shown: the button is hidden unless `oidcEnabled`; a stale page shows **"Single sign-on is off."** |
| `oidc-provider-unavailable` | 502 | `startOIDCLogin` (navigation) | — | the server's own response page; the client can't intercept the navigation |
| `provider-unavailable` | 502 | provider-backed calls | **"{provider} isn't responding."** "Try again{ in {s}s}." | alert/toast + **"Try again"** (enabled after `retryAfter`) |
| `too-many-streams` | 503 | `/dl`, `events` | **"Too many downloads."** "You have too many downloads running. Close one, or wait {s}s." | player message (§5.7); for `events`, §8.5 |
| `internal-error` | 500 | anything | **"Something went wrong."** "debroxy hit an unexpected error. Try again; if it keeps happening, tell your admin." | toast / page |
| `submission-pending` | 202 | `submitJob` | not an error | §5.5.5 |
| `provider-state-stale` | — | never sent | — | stale is a flag, not an error (server §10.3) |
| `submission-lost`, `merged-into` | — | failure codes, not error slugs | — | §6.3 |
| *(client)* `network-error` | 0 | anything | **"Can't reach debroxy."** "Check your connection and try again." | toast/alert + retry |
| *(client)* `http-{status}` | any | non-JSON error body | **"Unexpected response ({status})."** | toast |
| *(any unknown slug)* | any | — | **"Error ({slug})."** "{message}" | toast |

### 7.3 Client-side validation mirrors server rules

| Field | Rule | Server source |
|---|---|---|
| Username (create) | trim, lowercase, 3–32, `[a-z0-9._-]` | identity `NewUsername` (server §4.4) |
| Password (any new) | ≥ `config.passwordMinLength` (12) characters, ≤ 1024 UTF-8 bytes | server §4.4 |
| OIDC issuer | absolute `https:` URL | `invalid-oidc-identity` |
| OIDC subject | non-empty after trim | `invalid-oidc-identity` |
| Slot quota | integer ≥ 0 | `SlotQuotaRequest.minimum: 0` |
| Display name | trim, 1–255 characters (rename); 0–255 (submit, empty = omitted) | client rule (FE-25) |
| Magnet | §5.5.3 | `invalid-source` |
| `.torrent` | ≤ `uploadLimitBytes`, bencode with `info` | `upload-too-large`, `invalid-source` |
| Web URL | absolute http(s) | `invalid-source` |
| Cached-check fingerprints | 1 per request (the API allows ≤ 100) | `CachedAvailabilityRequest.maxItems` |

Client validation never replaces the server's; every server error is still handled per §7.2.

### 7.4 Session-ended flow

Triggered by the first 401 (`authentication-required` or `session-expired`) on any call except `logIn`:
1. Close the Event Streams.
2. Cancel queries, then clear the Query cache.
3. Navigate to `/login?redirect={current path + search}` with `replace`.
4. Show the toast **"Your session ended. Please log in again."**, unless the user just logged out.

The flow runs only once per page lifetime per session; a new login resets the guard.

### 7.5 Page-level error states

| Case | Content |
|---|---|
| Bootstrap failure (`config` or `me` with a non-401 error) | full page: **"Can't load debroxy"**, the error title per §7.2, and **"Try again"** (reload) |
| Route loader error | inside the shell: an error panel with title, text and **"Try again"** (`router.invalidate()`) |
| Render error (bug) | a TanStack Router `errorComponent` per route: **"This page crashed."** with **"Reload"**; the error stack only in dev builds |
| 403 page | §5.16 |
| 404 page | §5.16 |

---

## 8. Live updates and caching

### 8.1 Event Stream connection

- **One `EventSource`** for the user's own scope: `new EventSource("/api/v1/events")` (same origin; the cookie is sent automatically; `withCredentials` is not needed). It opens after `['me']` succeeds and closes on logout or session end.
- **Admin scope:** while `/admin/jobs` is mounted, a second `EventSource("/api/v1/events?scope=all")`. It closes on unmount. Events from it go only to `['admin','jobs']`, and own-scope events stay with the first stream. Two streams stay within the server's limit of 4 per user (server §5.11), even with two tabs open.
- The browser reconnects by itself (`retry: 3000`). The client tracks `open` and `error` events to drive the indicator (§8.4).

### 8.2 On (re)connect

On the `ready` event (sent first by the server, server §5.11):
1. `invalidateQueries(['jobs'])` and `['me']`, or `['admin','jobs']` for the admin stream. The server keeps no replay, so this refetch heals any gap.
2. Set the connection state to **Live**.

### 8.3 Event → cache

| Event | Own-scope cache effect | Admin-scope cache effect | Side effects |
|---|---|---|---|
| `job.upserted {Job}` | upsert into `['jobs'].jobs` by `id` (insert at the right sort position: newest `createdAt` first); `setQueryData(['job', id], job)` | upsert into `['admin','jobs'].jobs` | toast when the state *changes* to `ready` or `failed` for an own job (§8.7); drop `['links', id, …]` when `state` leaves `ready` |
| `job.released {jobId, historyEntry}` | remove from `['jobs']`; `removeQueries(['job', jobId])`, `['links', jobId]`; prepend `historyEntry` to `['history'].entries` if that query exists | remove from `['admin','jobs']` | if the detail page of that job is open: the "released" state (§5.6.2) |
| `quota.updated {QuotaUsage}` | `setQueryData(['me'], m => ({...m, quota}))` | — | — |
| `provider.stale {providerId, stale}` | merge `stale` into the provider in `['providers']` | same | the banner (§5.3.4) appears or disappears |
| `ready {scope}` | §8.2 | §8.2 | — |
| unknown event names | ignored (logged in dev builds only) | | |

Event data is parsed with `JSON.parse` and validated with hand-written Zod schemas in `api/schemas.ts`, each declared with `satisfies z.ZodType<GeneratedType>` so it cannot drift from the generated API types. Invalid data is dropped and triggers one `invalidateQueries` of the affected list.

### 8.4 Connection indicator (header)

| State | When | Display |
|---|---|---|
| **Live** | open and `ready` received | green dot, tooltip **"Live: updates appear as they happen."** |
| **Connecting** | before the first `ready` | grey pulsing dot, **"Connecting…"** |
| **Reconnecting** | `error` after having been open | amber dot, **"Reconnecting… updates may be delayed."** |
| **Polling** | 3 consecutive failed reconnects (§8.5) | amber dot with clock, **"Live updates unavailable. Checking every 10 seconds."** |

### 8.5 Fallback to polling

- **Switching to polling:** after 3 consecutive `error` events without a `ready` in between, the client closes the `EventSource` and polls `['jobs']` (and `['admin','jobs']` on its page) every **10 s** with `refetchInterval`. `refetchIntervalInBackground: false`, so a hidden tab stops polling.
- **Retrying the stream:** every 60 s while polling, the client tries a new `EventSource`; on `ready`, polling stops.
- **Stream limit:** a `503 too-many-streams` on connect cannot be read from `EventSource`; it shows as an `error`, so the same fallback applies.
- **Hidden tab:** when `document.visibilityState` turns `hidden` for more than **5 min**, the client closes the streams, which stops the server's `watch` refreshes (ADR 0021). It reopens them when the tab becomes visible, and the reconnect refetch covers the gap.

### 8.6 Query defaults

```ts
new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      gcTime: 5 * 60_000,
      refetchOnWindowFocus: true,      // off for queries kept live by the stream
      retry: (count, err) => isApiError(err) && (err.status === 0 || err.status >= 500) && err.status !== 501 && count < 2,
      retryDelay: (n, err) => (isApiError(err) && err.retryAfter ? err.retryAfter * 1000 : 1000 * 2 ** n),
    },
    mutations: { retry: false },
  },
});
```

- No retry on 4xx.
- Mutations never retry automatically. The only exception is the submit `job-id-conflict` rule (§5.5.5).
- Queries kept live by the stream (`['jobs']`, `['job', id]`, `['admin','jobs']`) set `staleTime: Infinity` while the stream is Live, and `10_000` otherwise.

### 8.7 Toasts for job transitions (FE-21)

- **Triggers:** an own-scope `job.upserted` whose previous cached state differs and whose new state is `ready` (**"{displayName} is ready."**, action **"Open"**) or `failed` (**"{displayName} failed: {failure text}."**, action **"Open"**).
- **No toast** for:
  - the first snapshot after a (re)connect, or any job not in the cache before;
  - jobs submitted in the last 3 s that are already `ready` (a cached add), since the submit toast covers them.
- **De-duplication:** by `(jobId, state)` for the page lifetime.

---

## 9. Design system (FE-11: dense, power-user)

### 9.1 Tokens (`src/styles/app.css`, Tailwind v4 `@theme`)

Colours are OKLCH. Both themes define every token; `.dark` (set on `<html>`) switches them. `system` follows `prefers-color-scheme` through a `matchMedia` listener that toggles the class.

| Token | Light | Dark | Use |
|---|---|---|---|
| `--background` | `oklch(0.99 0 0)` | `oklch(0.16 0.005 260)` | page |
| `--surface` | `oklch(0.97 0.003 260)` | `oklch(0.20 0.006 260)` | sidebar, table header, cards |
| `--border` | `oklch(0.90 0.004 260)` | `oklch(0.30 0.008 260)` | lines |
| `--foreground` | `oklch(0.22 0.01 260)` | `oklch(0.93 0.005 260)` | text |
| `--muted-foreground` | `oklch(0.50 0.01 260)` | `oklch(0.68 0.01 260)` | secondary text |
| `--primary` | `oklch(0.55 0.17 255)` | `oklch(0.68 0.15 255)` | buttons, links, focus ring |
| `--destructive` | `oklch(0.55 0.20 25)` | `oklch(0.65 0.19 25)` | release, delete |
| `--state-pending` | `oklch(0.60 0.02 260)` | `oklch(0.70 0.02 260)` | submitting…awaiting |
| `--state-active` | `oklch(0.58 0.15 240)` | `oklch(0.70 0.14 240)` | downloading, processing |
| `--state-ready` | `oklch(0.58 0.15 150)` | `oklch(0.72 0.15 150)` | ready |
| `--state-warning` | `oklch(0.72 0.15 75)` | `oklch(0.80 0.14 80)` | stalled, stale, expiring |
| `--state-failed` | `oklch(0.56 0.20 25)` | `oklch(0.66 0.19 25)` | failed, gone |
| `--state-muted` | `oklch(0.65 0.01 260)` | `oklch(0.55 0.01 260)` | deleting, deleted, unknown |

Every text/background pair MUST reach a WCAG contrast ratio of 4.5:1 (3:1 for icons and large text). The values above are starting points; the visual tests (§14.3) freeze the tuned result.

### 9.2 Density and typography

- Base font size is 13 px; table rows 40 px (52 px with a second line); controls 28 px high (`h-7`); spacing scale 4 px.
- **Font:** the system UI stack (`ui-sans-serif, system-ui, …`); numbers `tabular-nums`; IDs, hashes, provider states and errors in `ui-monospace`. No web fonts are downloaded (the CSP `font-src` defaults to `'self'`, and nothing is needed).
- Layout is min-width 1024 px: below it, the page scrolls horizontally rather than reflowing (FE-08). Max content width is unlimited; tables use the full width.

### 9.3 Components (shadcn/ui set used)

`button`, `input`, `textarea`, `label`, `select`, `checkbox`, `radio-group`, `tabs`, `dialog`, `alert-dialog`, `dropdown-menu`, `tooltip`, `badge`, `progress`, `table`, `skeleton`, `separator`, `scroll-area`, `alert`, `sonner`, `collapsible`, `toggle`. No others without a spec revision.

### 9.4 Motion

Spinners and pulses only (§6.1), all disabled under `prefers-reduced-motion: reduce`. No page transitions.

---

## 10. Accessibility

- **Landmarks:** `header`, `nav` (sidebar), `main`; one `h1` per page (the page title).
- **Keyboard:** everything works by keyboard with the standard Tab, Shift+Tab, Enter, Space and Esc, and arrow keys inside Radix menus, tabs, radio groups and the file tree (a `tree` role with `aria-expanded`). No custom shortcuts (FE-12).
- **Focus:** a visible ring (`--primary`, 2 px, offset 2 px); dialogs trap and return focus (Radix); after a route change, focus moves to the page `h1`.
- **Live regions:**
  - toasts are `role="status"` (errors: `alert`);
  - progress values in the table are **not** live, to avoid chatter;
  - the job detail progress bar has `aria-valuenow` and announces state changes through a polite live region (**"{name}: Ready"**);
  - countdowns announce only their start and end.
- **Labels:** every icon-only button has an `aria-label`; badges have text; colour is never the only signal (states have icon + label).
- **Contrast:** §9.1. The target is WCAG 2.2 AA by design; there are no automated audits in v1 (owner decision, FE-29), and the visual review covers it.

---

## 11. Security

1. **Session:** HttpOnly cookie only (FE-04). The client never sees a token and never uses `Authorization`. `logIn`'s `token` in the response body is ignored, not stored and not rendered.
2. **CSRF:** the server's `Origin` check (server §4.3) plus `SameSite=Strict`. The client sends only same-origin requests.
3. **CSP** (ADR 0022 rule 3) is strict: no inline scripts or styles, no `eval`, no remote origins. Consequences:
   - no runtime CSS injection: sonner and other libraries' CSS is imported at build time;
   - no `style` attributes in server-rendered HTML; React's `style` prop is applied through CSSOM and is allowed;
   - no web fonts or images from other origins;
   - the media player uses same-origin `/dl/…` URLs (`media-src 'self'`).
4. **Untrusted text:** all provider-supplied strings (`displayName`, `name`, `files[].path`, hoster names, `lastError`, server `message`) are rendered as React text, never as HTML. `dangerouslySetInnerHTML` is forbidden (Biome rule). File names are used in `download` attributes and `out=` lines only after removing control characters, `/` and `\` from the base name, and `..` path segments.
5. **Signed Links** are bearer secrets (server RISK-04):
   - they are kept in memory only;
   - they are never put in the URL bar of the app, in `localStorage`, in logs or in error reports;
   - the copy toast warns about sharing (§5.6.6), and exports are generated locally.
6. **Clipboard:** written only on explicit user action.
7. **Exports:** generated as `Blob`s with `type: "text/plain"`; object URLs are revoked right after the click.
8. **Redirect parameter:** `?redirect` is accepted only as a same-origin path starting with `/` and not `//` (open-redirect guard).
9. **Dependencies:**
   - lockfile committed and `pnpm install --frozen-lockfile` in builds;
   - `pnpm audit --prod` runs in `make web-check` and fails on high severity;
   - no post-install scripts are allowed except from an allow-list (`pnpm.onlyBuiltDependencies`).
10. **No telemetry,** analytics or third-party requests.

---

## 12. Performance budgets

| Budget | Limit |
|---|---|
| Initial JS (gzip), `/login` | ≤ 120 KiB |
| Initial JS (gzip), `/` after login, incl. shell | ≤ 250 KiB |
| CSS (gzip) | ≤ 30 KiB |
| Time to interactive on `/` (local server, mid-range laptop) | ≤ 1.5 s |
| Job table | 60 fps scrolling at 2 000 rows (virtualized from 200 rows) |
| Event handling | ≤ 2 ms per `job.upserted` for a 2 000-job cache (immutable update of one row) |
| `.torrent` hashing | in a Worker; 10 MiB in ≤ 300 ms |

`vite build` reports chunk sizes, and `make web-check` fails when a budget is exceeded (`size-limit` or an equivalent check script).

---

## 13. Build, embedding and development

### 13.1 Scripts (`web/package.json`)

| Script | Does |
|---|---|
| `gen:api` | `openapi-typescript ../api/openapi/identity.yml -o src/api/generated/identity.ts` (and job, delivery) |
| `dev` | `vite` on :5173 with the proxy of §13.3 |
| `build` | `tsc -b && vite build` → `dist/` |
| `check` | `biome check . && tsc -b --noEmit && vitest run && pnpm audit --prod --audit-level high && size check` |
| `test` / `test:watch` | Vitest |
| `test:visual` | `playwright test` (visual regression, §14.3) |
| `test:visual:update` | update the baselines (reviewed in the commit) |

### 13.2 Generated code

- `src/api/generated/*` is committed.
- `gen:api` runs when the OpenAPI files change. `make web-check` re-generates and fails when `git diff` shows a change, so the types never drift from the contract.

### 13.3 Development proxy

`vite.config.ts` proxies `/api`, `/dl`, `/healthz` and `/readyz` to `http://localhost:8080` (a local debroxy, README "Run with Docker"), with `changeOrigin: false`. Run the dev server with `DEBROXY_SERVER_PUBLIC_BASE_URL=http://localhost:5173`, so the cookie `Origin` check passes and Signed Links point through the proxy. SSE passes through the proxy unbuffered.

### 13.4 Embedding and Docker (server C13-05, C13-06)

- `vite build` writes `web/dist/index.html` and `web/dist/assets/*-[hash].{js,css}`; the Go package `web` embeds `dist` (ADR 0022).
- The committed placeholder `web/dist/index.html` is overwritten by real builds, and `.gitignore` ignores everything else in `web/dist/`.
- The `Dockerfile` Node stage: pinned `node:<LTS>-alpine` (exact version, as for the other images), `corepack enable`, `pnpm install --frozen-lockfile`, `pnpm build`; then the Go stage copies `web/dist` before `go build`.
- `make web` and `make web-check` run the same steps in a pinned Node container, so nothing is installed on the host.

---

## 14. Testing (FE-28, FE-29)

### 14.1 Unit (Vitest)

| Area | Cases |
|---|---|
| `btih.ts` | hex and base32 magnets; btmh-only magnet → none; multiple `xt`; malformed; `.torrent` v1, hybrid (v1+v2) and v2-only; truncated bencode; the `info` byte span is exact (golden hashes of synthetic torrents) |
| `format.ts` | every row of §6.6, including 0, null, boundaries 1023/1024, negative ETA guard |
| `states.ts` | every state and the unknown state; group mapping; failure codes incl. unknown |
| `filetree.ts` | nesting, single-child folder collapsing, numeric sort, per-folder sums, 10 000 files |
| `media.ts` | every extension of §6.7, case-insensitivity, no extension |
| `exports.ts` | aria2 and txt output; name sanitization (§11.4) |
| `errors.ts` | body with and without `retryAfter`; header only; non-JSON; network error; every slug maps to an entry |
| `events.ts` reducer | every event of §8.3 on an empty cache and on a full one; invalid data; toast triggers and de-duplication |

### 14.2 Component (Vitest + Testing Library + MSW)

MSW handlers are typed from the generated API types. Every screen of §5 is rendered in each of its states (loading, empty, data, stale, every error behaviour listed for it), and the key flows are driven by user events:
- login, including throttled and OIDC shown/hidden;
- add job (each tab, the cached hint, each submit outcome of §5.5.5);
- rename, release (shared or not);
- link copy and export;
- the player's error paths;
- change password;
- every admin form and its conflict errors.

An `EventSource` mock drives the §8 behaviours (connect, events, reconnect, fallback to polling).

### 14.3 Visual regression (Playwright)

Run against `vite preview` with MSW in the browser (no backend), on Chromium at 1440×900. Baselines are committed under `tests/visual/__screenshots__/`. Each page below is captured in **light and dark** themes:

| Page | States captured |
|---|---|
| Login | empty; throttled countdown; with SSO button |
| Jobs | many jobs (all 11 states + stalled + shared/charged badges); empty; stale banner; filters active |
| Add job | each tab; cached hint; slot-quota alert; submit-rate countdown |
| Job detail | ready multi-file tree with archive bar; downloading; failed; RD bundle-only; released |
| Player | video; warning banner; can't-play state |
| History | entries |
| Account | password user; SSO-only user |
| Admin users | list; new-user dialog; detail |
| Admin jobs | with owner column; expanded read-only drawer |
| Admin providers | capability matrix incl. unavailable/unverified/not-in-v1; hoster list |
| Unmanaged, Stuck deletions | data; empty |
| Errors | 404; 403; bootstrap failure |

A diff above 0.1 % of pixels fails. Fonts are system fonts, so baselines are produced in the pinned Playwright Docker image only (`mcr.microsoft.com/playwright:<exact version>`).

### 14.4 CI entry point

`make web-check` (in Docker) runs `pnpm check` and `pnpm test:visual`. It is separate from the Go `make check`.

---

## 15. API coverage matrix

Every operation and every request and response member of the API (the three OpenAPI files plus server revision 13) maps to a place in the client, or is marked **not used** with a reason. A server change that adds an operation or member MUST update this table in the next revision of this spec.

### 15.1 Operations

| Operation | Client use | § |
|---|---|---|
| `logIn` | login form | 5.1 |
| `logOut` | user menu, session-end banner | 5.2.1 |
| `startOIDCLogin` | SSO button (full navigation) | 5.1 |
| `finishOIDCLogin` | not called; the identity provider redirects the browser there and the server redirects to `/` | 5.1 |
| `changeOwnPassword` | account page | 5.9 |
| `listUsers` | admin users list | 5.10 |
| `createUser` | new-user dialog | 5.10 |
| `getUser` | admin user detail | 5.11 |
| `updateUser` | role and status | 5.11 |
| `resetUserPassword` | reset dialog | 5.11 |
| `linkOIDCIdentity` | link dialog | 5.11 |
| `unlinkOIDCIdentity` | unlink | 5.11 |
| `getMe` | bootstrap, shell meters, account, session banner | 3.6, 5.2, 5.9 |
| `listProviders` | add dialog, filters, admin providers, stale map | 5.3, 5.5, 5.13 |
| `listHosters` | web tab, admin providers | 5.5.3, 5.13 |
| `checkCachedAvailability` | pre-submit hint | 5.5.4 |
| `refreshProvider` | jobs Refresh, admin providers | 5.3.5, 5.13 |
| `listJobs` | jobs page (+ refetch on stream connect) | 5.3, 8.2 |
| `submitJob` | add dialog | 5.5.5 |
| `listJobHistory` | history page | 5.8 |
| `getJob` | job detail | 5.6 |
| `renameJob` | rename dialog | 5.6.3 |
| `releaseJob` | release dialog | 5.6.4 |
| `listAllJobs` | admin jobs | 5.12 |
| `releaseAnyJob` | admin release | 5.12 |
| `setSlotQuota` | admin user detail | 5.11 |
| `listUnmanagedItems` | admin unmanaged | 5.14 |
| `listParkedOutboxMessages` | admin stuck deletions, sidebar count | 5.15, 4.2 |
| `probeProviderCapabilities` | admin providers | 5.13 |
| `handleProviderWebhook` | **not used**: provider-to-server only | — |
| `issueSignedLink` | copy, download, play | 5.6.6, 5.7 |
| `issueSignedLinksForAllFiles` | copy all, exports | 5.6.6 |
| `download` (`GET /dl/…`) | browser download navigation, `<video>`/`<audio>` source | 5.6.5, 5.7 |
| `downloadHead` (`HEAD /dl/…`) | player diagnosis after a media error only | 5.7 |
| `getClientConfig` (rev 13) | bootstrap; SSO button, upload limit, link TTL, password length, version | 3.6 |
| `watchJobs` (rev 13, `GET /api/v1/events`) | live updates, own and admin scope | 8 |

### 15.2 Members

| Schema (file) | Member | Client use (§) |
|---|---|---|
| `Error` (all) | `slug`, `message`, `retryAfter` (rev 13) | 3.3, 7.2 |
| `LoginRequest` | `username`, `password` | 5.1 |
| `LoginResponse` | `token` | **not used**: cookie-only auth (FE-04, §11.1) |
| `LoginResponse` | `expiresAt` | **not used**: `me.session.idleExpiresAt` replaces it (5.2.1) |
| `ChangeOwnPasswordRequest` | `currentPassword`, `newPassword` | 5.9 |
| `CreateUserRequest` | `username`, `role`, `password`, `oidcIdentity` | 5.10 |
| `OIDCIdentity` | `issuer`, `subject` | 5.10, 5.11 |
| `UpdateUserRequest` | `role`, `status` | 5.11 |
| `ResetUserPasswordRequest` | `password` | 5.11 |
| `User` (identity and job) | `id` | routes, "(you)" marker |
| `User` | `username`, `role`, `status`, `passwordLogin`, `oidcLinked`, `createdAt` | 5.9, 5.10, 5.11, header |
| `UserList` | `users` | 5.10 |
| `Me` | `user`, `quota` | 5.2, 5.9 |
| `Me` (rev 13) | `session.idleExpiresAt`, `session.absoluteExpiresAt` | 5.2.1, 5.9 |
| `Me` (rev 13) | `submitRate.{uncached,cached}.{used, perHour, resetsAt}` | 5.2.3, 5.5.4, 5.5.6, 5.9 |
| `QuotaUsage` | `slotsUsed`, `maxActiveJobs` | 5.2.3, 5.3.4, 5.5 |
| `ProviderList` | `providers` | 5.5, 5.13 |
| `Provider` | `id`, `kind`, `displayName`, `enabled`, `capabilities` | 5.3, 5.5.2, 5.13 |
| `Capability` | `name`, `supported`, `enabled`, `state` | 5.5.3, 5.13, 6.5 |
| `HosterList` | `hosters` | 5.5.3, 5.13 |
| `Hoster` | `name`, `domains`, `up`, `dailyLimit`, `dailyUsed` | 5.5.3, 5.13 |
| `CachedAvailabilityRequest` | `fingerprints` | 5.5.4 |
| `CachedAvailability` | `cached` | 5.5.4 |
| `RefreshResult` | `stale` | 5.3.5 |
| `SubmitJobRequest` | `id`, `providerId`, `displayName`, `source` | 5.5.5 |
| `Source` | `type` (`magnet`, `torrent-file`, `web-url`; `nzb-file`, `nzb-url` hidden in v1), `magnet`, `url`, `password`, `fileName`, `file` | 5.5.3 |
| `SubmissionPending` | `slug`, `job` | 5.5.5 |
| `RenameJobRequest` | `displayName` | 5.6.3 |
| `JobList` | `jobs`, `stale` | 5.3, 5.12 |
| `Job` | `id`, `displayName`, `provider`, `kind`, `state`, `telemetry`, `sizeBytes`, `expiresAt`, `shared`, `slotCharged`, `delivery`, `files`, `createdAt`, `failureReason` | 5.3.3, 5.6.2, 6 |
| `Job` | `ownerUsername` | 5.12 (admin) |
| `Job` | `stale` | 5.6.2 (single job) |
| `Telemetry` | `progress`, `bytesPerSecond`, `etaSeconds`, `peers`, `stalled` | 5.3.3, 5.6.2, 6.1 |
| `Delivery` | `perFile`, `bundle` | 5.6.5 |
| `Bundle` | `format`, `rangeSupported` | 5.6.5 |
| `File` | `id`, `path`, `sizeBytes` | 5.6.5 |
| `HistoryList` | `entries` | 5.8 |
| `HistoryEntry` | `jobId`, `displayName`, `provider`, `kind`, `name`, `finalState`, `failureReason`, `claimedAt`, `releasedAt` | 5.8, 5.5.5 (200), 8.3 |
| `SlotQuotaRequest` | `maxActiveJobs` | 5.11 |
| `UnmanagedItemList` | `items` | 5.14 |
| `UnmanagedItem` | `provider`, `providerItemId`, `kind`, `name`, `sizeBytes`, `state`, `firstSeenAt`, `lastSeenAt` | 5.14 |
| `OutboxMessageList` | `messages` | 5.15 |
| `OutboxMessage` | `id`, `kind`, `remoteItemId`, `attempts`, `lastError`, `createdAt`, `parkedAt` | 5.15 |
| `IssueSignedLinkRequest` / `LinkTarget` | `target.fileId`, `target.bundle` | 5.6.6 |
| `SignedLink` | `url`, `expiresAt`, `rangeSupported`, `fileId`, `bundle` | 5.6.6, 5.7 |
| `SignedLinkList` | `links` | 5.6.6 |
| `ClientConfig` (rev 13) | `version`, `oidcEnabled`, `uploadLimitBytes`, `signedLinkTTLSeconds`, `passwordMinLength` | 4.2, 5.1, 5.5.3, 5.6.6, 5.9 |
| Event `job.upserted` (rev 13) | Job | 8.3 |
| Event `job.released` | `jobId`, `historyEntry` | 8.3 |
| Event `quota.updated` | QuotaUsage | 8.3 |
| Event `provider.stale` | `providerId`, `stale` | 8.3 |
| Event `ready` | `scope` | 8.2 |
| Cookie `debroxy_oidc`, `debroxy_session` | — | browser-managed, never read by the client |

---

## 16. Client decisions

| ID | Decision | Owner answer / reason |
|---|---|---|
| FE-01 | React 19 + TypeScript strict, Vite | owner: "most up-to-date frontend technologies, TypeScript with React" |
| FE-02 | TanStack Router (file-based) + TanStack Query | owner choice |
| FE-03 | shadcn/ui (Radix) + Tailwind CSS v4 | owner choice |
| FE-04 | Cookie-only auth; the client never handles tokens | owner choice; XSS cannot steal a session |
| FE-05 | Biome + pnpm; Node LTS pinned | owner choice |
| FE-06 | TanStack Form + Zod | owner choice |
| FE-07 | Served by debroxy from the embedded build (ADR 0022) | owner choice; one origin |
| FE-08 | Desktop only, ≥ 1024 px | owner choice |
| FE-09 | English only, no i18n framework | owner choice (i18n not selected) |
| FE-10 | No PWA | owner choice |
| FE-11 | Dense, power-user look | owner choice |
| FE-12 | No custom keyboard shortcuts | owner choice |
| FE-13 | Single submit, no batch | owner choice |
| FE-14 | Pre-submit cached check with in-browser info hash | owner choice |
| FE-15 | Provider preselected as last used | owner choice |
| FE-16 | Job list: search, state-group and provider filters, sortable columns, all in the URL | owner choice |
| FE-17 | No bulk release | owner choice |
| FE-18 | Copy link / copy all; aria2 and plain-text exports; link expiry shown and links re-issued automatically | owner choice |
| FE-19 | Files as a folder tree | owner choice |
| FE-20 | In-browser player for all media, with a warning for types browsers may not decode | owner choice |
| FE-21 | In-app toasts only for ready/failed | owner choice |
| FE-22 | Job detail as its own page | owner choice |
| FE-23 | IEC units and relative times | owner choice |
| FE-24 | Release with a confirmation that explains `shared` | owner choice |
| FE-25 | Display names limited to 255 characters by the client | the server has no limit; long names break the dense layout (OQ-FE-04) |
| FE-26 | Live updates over the Event Stream (server C13-01) with a fallback to 10 s polling | owner choice: SSE now |
| FE-27 | A full slug → text/behaviour table | owner choice |
| FE-28 | Vitest + Testing Library + MSW | owner choice |
| FE-29 | Playwright visual regression in both themes; no automated accessibility audits and no end-to-end tests against a real backend in v1 | owner choice |
| FE-30 | Evergreen browsers of the last two years (Chrome, Edge, Firefox, Safari desktop); no polyfills | owner choice |

---

## 17. Open questions

| ID | Question |
|---|---|
| OQ-FE-01 | Admins cannot read a user's slot quota or the instance default (no API). Add it (for example `maxActiveJobs` in the admin user view) in a later server revision? |
| OQ-FE-02 | A failed OIDC callback answers with an error response, not a redirect, so the browser shows a raw JSON error. Should the server redirect to `/login?error=oidc` instead? |
| OQ-FE-03 | When usenet is enabled (server v2), the NZB tabs need their own validation and texts. |
| OQ-FE-04 | Should the server limit `displayName` length (the client uses 255)? |
| OQ-FE-05 | `job-already-exists` does not return the existing job's ID; the client can only point to the list. Include `jobId` in that error? |
| OQ-FE-06 | Subtitles: load sibling `.srt`/`.vtt` files into the player (needs `.srt` → WebVTT conversion in the browser)? |
| OQ-FE-07 | The admin cannot see a user's linked issuer and subject (only `oidcLinked`). Expose them to admins? |
| OQ-FE-08 | The meaning of `dailyLimit`/`dailyUsed` differs per provider (TorBox: links per day). The client shows plain numbers, "{used} / {limit} today". Should the API state the unit? |
| OQ-FE-09 | Do long-lived Event Streams extend the session's idle timeout? The client assumes not (§5.2.1). The server spec should say. |
