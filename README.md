# debroxy

debroxy is a self-hosted proxy that lets several users share one debrid provider account (TorBox, Real-Debrid). Users submit magnets, `.torrent` files or web links; debroxy deduplicates them into one provider item, keeps per-user jobs and slot quotas, and streams the finished files through itself. All provider traffic leaves from one IP, CDN URLs never reach users, and no file is ever stored on disk.

## Requirements

- **A host with Docker and Docker Compose**, `linux/amd64` or `linux/arm64`. debroxy needs little CPU and memory, but every download passes through it, so its bandwidth is what your users share.
- **A premium account at TorBox or Real-Debrid** (or both), with its API key. Use an account dedicated to debroxy (see [Exclusive account use](#exclusive-account-use)).
- **HTTPS**, through a reverse proxy such as Caddy, Traefik or nginx. The session cookie is `Secure`, so browsers only keep it over HTTPS; `http://localhost` is the one exception and is fine for trying debroxy out.

## Quick start

1. Create a directory with this `compose.yaml`:

   ```yaml
   services:
     debroxy:
       image: ghcr.io/feruzabad/debroxy:0.1.0
       env_file: .env
       ports:
         - "127.0.0.1:8080:8080"   # your reverse proxy forwards to this port
       volumes:
         - debroxy-data:/data       # the SQLite database; back it up
       restart: unless-stopped

   volumes:
     debroxy-data:
   ```

2. Next to it, create `.env` with the minimum configuration:

   ```sh
   # The URL users open in the browser: scheme and host, no path.
   DEBROXY_SERVER_PUBLIC_BASE_URL=https://debroxy.example.org

   # The provider account(s). The ID (here "torbox") is your choice.
   DEBROXY_PROVIDERS=torbox
   DEBROXY_PROVIDER_TORBOX_KIND=torbox             # torbox or realdebrid
   DEBROXY_PROVIDER_TORBOX_API_KEY=<your TorBox API key>

   # Key for signing download links: at least 32 random bytes.
   DEBROXY_SIGNING_KEYS=k1
   DEBROXY_SIGNING_KEY_K1=<output of: openssl rand -base64 48>

   # The first admin, created on the first start.
   DEBROXY_BOOTSTRAP_ADMIN_USERNAME=admin
   DEBROXY_BOOTSTRAP_ADMIN_PASSWORD=<at least 12 characters>
   ```

   For Real-Debrid, use `DEBROXY_PROVIDER_REALDEBRID_KIND=realdebrid` and `…_API_KEY` with `DEBROXY_PROVIDERS=realdebrid`; for both accounts, `DEBROXY_PROVIDERS=torbox,realdebrid` with both sets of variables.

3. Start it and check the log:

   ```sh
   docker compose up -d
   docker compose logs -f debroxy   # "first admin created", then "debroxy started"
   ```

   debroxy validates the whole configuration at startup and lists every problem at once. A rejected API key shows up as `provider … rejected the request` in the log.

4. Point your reverse proxy at `127.0.0.1:8080` (see [Reverse proxy](#reverse-proxy)), open the public URL and log in as the admin.

5. Create accounts for your users under **Admin → Users**. Each user then adds magnets, `.torrent` files or hoster links, follows the download, and downloads or plays the files from the browser; a job's links can also be copied for a download manager or player.

To try debroxy on your own machine without a proxy, set `DEBROXY_SERVER_PUBLIC_BASE_URL=http://localhost:8080` and open exactly that URL.

### Updating

Change the image tag in `compose.yaml` to the new version and run `docker compose pull && docker compose up -d`. Database migrations run on start. Released versions are listed under the repository's tags; `:latest` follows the newest one.

### Reverse proxy

- Forward everything to debroxy on the same host name; the web app, the API and the download links share one origin.
- Do not buffer responses and allow long requests: downloads and the live event stream (`/api/v1/events`) stay open.
- Set `DEBROXY_SERVER_TRUSTED_PROXIES` to the proxy's address (for Docker, its network range, e.g. `172.16.0.0/12`), so debroxy sees the client IPs that login throttling uses.
- **Do not log the URL paths of `/dl/` and `/webhooks/`**: they carry bearer tokens and secrets. debroxy's own access log redacts both.

A minimal Caddyfile (Caddy writes no access log unless one is configured):

```
debroxy.example.org {
	reverse_proxy 127.0.0.1:8080 {
		flush_interval -1
	}
}
```

## Configuration

debroxy is configured **only by environment variables**. [`.env.example`](.env.example) lists every one with its default; uncommented lines are required. Startup validates everything and lists every problem at once; an unknown `DEBROXY_*` variable (a typo, an old name) is an error.

- **Lists** are comma-separated, **durations** are Go durations (`30s`, `720h`), **booleans** are `true`/`false`.
- **Providers** are listed by ID in `DEBROXY_PROVIDERS=torbox,realdebrid`; each one's settings are `DEBROXY_PROVIDER_<ID>_*`, with the ID upper-cased and `-` written as `_`. Rate budgets are `DEBROXY_PROVIDER_<ID>_RATE_BUDGET_<CLASS>=<requests>/<duration>`.
- **Storage:** `DEBROXY_DATA_DIR` must be an existing directory; it holds `debroxy.db` and its lock file and nothing else. The image sets it to the `/data` volume.
- **Common limits:** `DEBROXY_JOB_DEFAULT_SLOT_QUOTA` (active jobs per user, default 3; admins can change it per user), `DEBROXY_JOB_SUBMIT_RATE_PER_HOUR`, and the stream caps `DEBROXY_DELIVERY_MAX_STREAMS_PER_USER` and `DEBROXY_DELIVERY_MAX_CONCURRENT_STREAMS`.

**Secrets** go directly into their variable, or into a file named by the same variable with `_FILE` appended (for Docker secrets; one of the two, not both):

| Secret | Variable | Notes |
|---|---|---|
| Provider API key | `DEBROXY_PROVIDER_<ID>_API_KEY` | |
| Webhook path secret | `DEBROXY_PROVIDER_<ID>_WEBHOOK_SECRET` | at least 32 bytes |
| Signed Link keys | `DEBROXY_SIGNING_KEY_<ID>` for each ID in `DEBROXY_SIGNING_KEYS` | at least 32 bytes each |
| OIDC client secret | `DEBROXY_IDENTITY_OIDC_CLIENT_SECRET` | only when OIDC is enabled |
| First admin password | `DEBROXY_BOOTSTRAP_ADMIN_PASSWORD` | only with `DEBROXY_BOOTSTRAP_ADMIN_USERNAME` |

Generate secrets with e.g. `openssl rand -base64 48`. With Docker secrets, the service gets e.g. `secrets: [torbox_api_key]` and `.env` sets `DEBROXY_PROVIDER_TORBOX_API_KEY_FILE=/run/secrets/torbox_api_key` instead of the key.

**First admin.** `serve` creates the bootstrap admin on an empty database and skips the step once users exist, so the two variables can stay or go afterwards. Alternatively, on an empty database:

```sh
docker compose run --rm -T debroxy bootstrap-admin --username root < password.txt
```

`DEBROXY_SERVER_PUBLIC_BASE_URL` must be the URL users reach debroxy at; Signed Links and the OIDC callback are built from it, and browser changes are rejected unless their `Origin` matches it.

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

`serve` stops on `SIGINT`/`SIGTERM`: it stops answering `/readyz`, lets running requests finish within `DEBROXY_SERVER_SHUTDOWN_TIMEOUT`, cuts the rest (long downloads), waits for background work, and closes the database.

Probes: `GET /healthz` (process alive) and `GET /readyz` (database reachable; not ready while shutting down). `debroxy healthcheck` exits 0 when `/readyz` on `DEBROXY_SERVER_LISTEN` answers 200; the image uses it as its Docker health check.

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

## Development

The design is in [`docs/specs/`](docs/specs/) (the highest revision is current) and [`docs/adr/`](docs/adr/); the browser client's in [`docs/frontend/`](docs/frontend/).

Requirements: Go 1.27 and Docker (the web client, lint and visual tests build in pinned containers).

```sh
make check                  # vet, race tests, architecture test, lint
make web-check              # web client: lint, types, unit tests, size budgets, visual tests
make docker                 # builds the image as debroxy:dev
docker compose up -d        # the repository's compose.yaml builds and runs debroxy:dev with ./.env
```

Running the binary directly:

```sh
make web                    # builds the client into web/dist (in a container); it is embedded into the binary
go build -o bin/debroxy ./cmd/debroxy
set -a; . ./.env; set +a    # or export the DEBROXY_* variables another way
bin/debroxy migrate         # apply database migrations (serve does this too)
bin/debroxy serve
```

**Releases.** `.github/workflows/image.yaml` builds the image for `linux/amd64` and `linux/arm64` on every push. A tag `vX.Y.Z` also pushes `ghcr.io/feruzabad/debroxy:X.Y.Z` and `:latest`. Merge to `main` only when that check is green.

```sh
git tag -s v0.2.0 -m "v0.2.0" && git push origin v0.2.0
```

## License

[MIT](LICENSE)
