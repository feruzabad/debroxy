# debroxy

debroxy is a self-hosted proxy that lets several users share one debrid provider account (TorBox, Real-Debrid). Users submit magnets, `.torrent` files or web links; debroxy deduplicates them into one provider item, keeps per-user jobs and slot quotas, and streams the finished files through itself. All provider traffic leaves from one IP, CDN URLs never reach users, and no file is ever stored on disk.

The design is in [`docs/specs/`](docs/specs/) (the highest revision is current) and [`docs/adr/`](docs/adr/).

## Run with Docker

```sh
cp .env.example .env        # fill in the URL, providers, API keys and a signing key
docker compose up -d        # builds the image, stores everything in the debroxy-data volume
```

The image runs `debroxy serve` as a non-root user on Alpine, keeps the database in the `/data` volume and reports its health through `debroxy healthcheck`. `make docker` builds it as `debroxy:dev`.

**First admin.** Set `DEBROXY_BOOTSTRAP_ADMIN_USERNAME` and `DEBROXY_BOOTSTRAP_ADMIN_PASSWORD` (at least 12 characters): `serve` creates that admin on an empty database and skips the step once users exist, so the variables can stay or go afterwards. Alternatively:

```sh
docker compose run --rm -T debroxy bootstrap-admin --username root < password.txt
```

All later users are created by an admin through the API (`POST /api/v1/admin/users`).

**Released images.** `.github/workflows/image.yaml` builds the image for `linux/amd64` and `linux/arm64` on every push, on a GitHub-hosted runner. A tag `vX.Y.Z` also pushes `ghcr.io/feruzabad/debroxy:X.Y.Z` and `:latest`. Merge to `main` only when that check is green (GitHub can't enforce it for this private repository on the free plan).

```sh
git tag -s v0.1.0 -m "v0.1.0" && git push origin v0.1.0
```

## Build and run without Docker

Requirements: Go 1.27.

```sh
go build -o bin/debroxy ./cmd/debroxy
make check                  # vet, race tests, architecture test, lint (lint runs in Docker)

set -a; . ./.env; set +a    # or export the DEBROXY_* variables another way
bin/debroxy migrate         # apply database migrations
bin/debroxy bootstrap-admin --username root < password.txt
bin/debroxy serve           # also applies pending migrations
```

`serve` stops on `SIGINT`/`SIGTERM`: it stops answering `/readyz`, lets running requests finish within `DEBROXY_SERVER_SHUTDOWN_TIMEOUT`, cuts the rest (long downloads), waits for background work, and closes the database.

Probes: `GET /healthz` (process alive) and `GET /readyz` (database reachable; not ready while shutting down). `debroxy healthcheck` exits 0 when `/readyz` on `DEBROXY_SERVER_LISTEN` answers 200.

## Configuration

debroxy is configured **only by environment variables**. [`.env.example`](.env.example) lists every one with its default; uncommented lines are required. Startup validates everything and lists every problem at once; an unknown `DEBROXY_*` variable (a typo, an old name) is an error.

- **Lists** are comma-separated, **durations** are Go durations (`30s`, `720h`), **booleans** are `true`/`false`.
- **Providers** are listed by ID in `DEBROXY_PROVIDERS=torbox,realdebrid`; each one's settings are `DEBROXY_PROVIDER_<ID>_*`, with the ID upper-cased and `-` written as `_`. Rate budgets are `DEBROXY_PROVIDER_<ID>_RATE_BUDGET_<CLASS>=<requests>/<duration>`.
- **Storage:** `DEBROXY_DATA_DIR` must be an existing directory; it holds `debroxy.db` and its lock file and nothing else.

**Secrets** go directly into their variable, or into a file named by the same variable with `_FILE` appended (for Docker secrets; one of the two, not both):

| Secret | Variable | Notes |
|---|---|---|
| Provider API key | `DEBROXY_PROVIDER_<ID>_API_KEY` | |
| Webhook path secret | `DEBROXY_PROVIDER_<ID>_WEBHOOK_SECRET` | at least 32 bytes |
| Signed Link keys | `DEBROXY_SIGNING_KEY_<ID>` for each ID in `DEBROXY_SIGNING_KEYS` | at least 32 bytes each |
| OIDC client secret | `DEBROXY_IDENTITY_OIDC_CLIENT_SECRET` | only when OIDC is enabled |
| First admin password | `DEBROXY_BOOTSTRAP_ADMIN_PASSWORD` | only with `DEBROXY_BOOTSTRAP_ADMIN_USERNAME` |

Generate secrets with e.g. `openssl rand -base64 48`.

`DEBROXY_SERVER_PUBLIC_BASE_URL` must be the URL users reach debroxy at; Signed Links and the OIDC callback are built from it. Behind a reverse proxy, list the proxy's address range in `DEBROXY_SERVER_TRUSTED_PROXIES` so client IPs (used for login throttling) come from `X-Forwarded-For`, and configure the proxy **not to log URL paths of `/dl/` and `/webhooks/`**: they carry bearer tokens and secrets. debroxy's own access log redacts both.

### Exclusive account use

debroxy assumes it is the **only** user of each provider account. Items created outside debroxy appear as *unmanaged items* (`GET /api/v1/admin/unmanaged-items`) and are never deleted; but a person using the account directly can delete items that jobs depend on, or exhaust the account's rate limits for everyone. Use a dedicated provider account.

Sharing an account may violate the provider's terms; checking them is the operator's responsibility.

### Egress

All traffic of a provider account leaves through one pinned address family (`DEBROXY_PROVIDER_<ID>_EGRESS_IP_FAMILY`, `ipv4` or `ipv6`) and, if set, one local source address (`…_EGRESS_LOCAL_ADDRESS`), so the provider sees a single IP. HTTP proxy environment variables are ignored. At startup debroxy opens a TCP connection to the API host and to each known CDN host through the pinned dialer and refuses to start if one fails; `DEBROXY_PROVIDER_<ID>_CDN_PROBE_HOSTS` overrides the probed CDN hosts.

### Webhooks (TorBox)

Set `DEBROXY_PROVIDER_<ID>_WEBHOOK_ENABLED=true` and `…_WEBHOOK_SECRET`, then point the TorBox webhook at `https://<public base URL>/webhooks/<providerId>/<secret>`. A webhook only triggers a refresh (at most one immediate and one trailing refresh per `DEBROXY_JOB_WEBHOOK_REFRESH_INTERVAL`); it never carries state, and debroxy also refreshes on reads and on a timer, so webhooks are optional.

### OIDC login

Set `DEBROXY_IDENTITY_OIDC_ENABLED=true` with `…_ISSUER_URL`, `…_CLIENT_ID` and `…_CLIENT_SECRET`, and register `https://<public base URL>/api/v1/auth/oidc/callback` as the redirect URI at the identity provider. Browsers start at `GET /api/v1/auth/oidc/login`. Only users an admin has linked beforehand can log in (`PUT /api/v1/admin/users/{id}/oidc-identity` with the provider's exact `iss` value and the user's `sub`); there is no automatic provisioning.

## Operations

### Rotating a provider API key

A CDN URL embeds access to the provider account, and deleting the item does not revoke it. If a CDN URL may have leaked (for example through a proxy log), **rotate the API key**:

1. Create a new key in the provider's account settings.
2. Put it into `DEBROXY_PROVIDER_<ID>_API_KEY` (or the file named by `…_API_KEY_FILE`).
3. Restart debroxy (`docker compose up -d` recreates the container after `.env` changed); cached CDN links are memory-only and die with the process.
4. Revoke the old key at the provider.

### Rotating the webhook secret

Anyone who knows the webhook path can trigger refreshes and spend the shared rate budget. To rotate: generate a new secret, set it in `DEBROXY_PROVIDER_<ID>_WEBHOOK_SECRET`, restart debroxy, then update the webhook URL at the provider. Webhooks sent to the old path in between are answered with 404 and are harmless; the timer refresh covers the gap.

### Rotating Signed Link keys

The first ID in `DEBROXY_SIGNING_KEYS` signs new links; every listed key verifies. To rotate, add a new ID **first** in the list with its `DEBROXY_SIGNING_KEY_<ID>`, and restart; remove the old key after `DEBROXY_DELIVERY_SIGNED_LINK_TTL` has passed. Removing a key at once revokes every link signed with it.

### Database and backups

Everything lives in one SQLite file, `debroxy.db` in `DEBROXY_DATA_DIR` (the `/data` volume in Docker), written by a single connection; a lock file next to it stops a second process from opening it. Back it up with `sqlite3 debroxy.db ".backup backup.db"` while running, or copy it while debroxy is stopped (with Docker: `docker compose stop`, `docker compose cp debroxy:/data/debroxy.db ./debroxy-backup.db`, `docker compose start`). Old history, processed outbox messages and deleted items are pruned hourly per `DEBROXY_JOB_RETENTION_*`.

### Things to watch

- **Parked deletions** (`GET /api/v1/admin/outbox`): a provider item debroxy failed to delete 20 times. It still exists at the provider and counts against the account's limits; delete it there by hand.
- **Logs with `operator_alert=credentials`**: the provider rejected the API key.
- **Stream caps** (`DEBROXY_DELIVERY_MAX_STREAMS_PER_USER`, `DEBROXY_DELIVERY_MAX_CONCURRENT_STREAMS`): every download passes through the debroxy host, so size bandwidth for the expected number of concurrent streams.

## License

[MIT](LICENSE)
