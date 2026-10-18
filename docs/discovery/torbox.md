# TorBox API discovery

| | |
|---|---|
| Date | 2026-10-11 |
| Account | plan `1`, subscribed, no additional slots; usenet **not** included in this plan |
| Method | ad-hoc curl, User-Agent `debroxy-discovery/0.1`; sanitized transcripts in [`docs/discovery/torbox/`](torbox/) |
| Test material | Debian 13.7 netinst torrents (cached at TorBox), `debian-mac` netinst (uncached), archive.org `AvengeDe1942` (public domain; multi-file torrent + single files) |
| Input to | `docs/specs/revision2.md` and later revisions |

Fixture references below are file names in `docs/discovery/torbox/`.

## 1. Assumption register (revision 1 §9.1)

| ASM | Verdict | Observation | Fixtures |
|---|---|---|---|
| T01 base URL + Bearer | **Confirmed** | `https://api.torbox.app/v1/api`, `Authorization: Bearer <key>`. Invalid key → 403 `AUTH_ERROR`. Missing header → 401 `{"detail":"Not authenticated"}` | `01-auth-*` |
| T02 envelope | **Partly refuted** | Normal responses use `{success, error, detail, data}`. Exceptions: (a) auth and validation errors are FastAPI-style `{"detail": …}` with no envelope (401/422); (b) **business errors come back as HTTP 500** (`PLAN_RESTRICTED_FEATURE`, `UNSUPPORTED_SITE`); (c) not-found is **404 with `success:true`** and `error:"ITEM_NOT_FOUND"`; (d) deleting an already-deleted item gives 500 with `error` and `detail` **swapped** (`"detail":"DATABASE_ERROR"`) | `07-create-nzb-*`, `08-create-webdl-debian-iso`, `09-mylist-id-after-delete`, `09-delete-torrent-again`, `04-requestdl-header-only` |
| T03 createtorrent | **Confirmed** | Multipart `magnet=` or `file=@…torrent`, `allow_zip=true` accepted. Returns `{hash, torrent_id, auth_id}`. Cached: detail "Found Cached Torrent. Using Cached Torrent."; uncached: "Torrent Added Successfully". `seed`, `name`, `as_queued` not exercised | `02-create-*`, `05-create-torrentfile-multifile` |
| T04 provider-side dedup | **Confirmed** | Re-adding the same magnet, an uppercase bare `xt=urn:btih:` magnet, or the same content as a `.torrent` returns the **same `torrent_id`**. Web downloads dedup by MD5 of the URL ("Found cached web download") | `03-*`, `08-readd-same-webdl` |
| T05 mylist fields | **Confirmed, with additions** | `mylist?id=&bypass_cache=true` returns a single object; without `id`, an array. Field catalogue in §3. **`files` is `null` until the download has finished**; `expires_at` is `null` while active. A fresh item was visible immediately (no listing lag observed) | `02-poll-*`, `05-poll-*` |
| T06 controltorrent | **Confirmed (delete, reannounce)** | `POST /torrents/controltorrent` with JSON `{"torrent_id":…, "operation":"delete" \| "reannounce"}` → 200, `data:null`. Re-deleting → 500 `DATABASE_ERROR` (not idempotent). `pause`/`resume` not exercised. Web downloads: `POST /webdl/controlwebdownload` `{"webdl_id":…, "operation":"delete"}` | `09-*` |
| T07 requestdl | **Confirmed, important detail** | `GET /torrents/requestdl?token=<API KEY>&torrent_id=&file_id=` — the `token` query parameter is **required**; the Bearer header alone gives 422. Returns `data` = CDN URL. **The CDN URL itself embeds the account API key** (`…/dld/<uuid>?token=<API KEY>`; zip: `…/zip/<uuid>?token=<API KEY>`). Web downloads use `web_id=` instead of `torrent_id=` | `04-requestdl-*`, `08-webdl-requestdl` |
| T08 CDN behaviour | **Confirmed, with additions** | `Accept-Ranges: bytes`; ranged GET → 206 with `Content-Range`; `ETag` present; `Content-Disposition: attachment; filename="…"; filename*=UTF-8''…`; `Content-Type: application/octet-stream`. **Not IP-bound**: the link worked from a second IPv4 host and with a fake `user_ip`. `user_ip` only changed the CDN region. Still valid after 27 min **and after the item was deleted** | `04-cdn-*` |
| T09 checkcached | **Confirmed** | `GET /torrents/checkcached?hash=a,b&format=object\|list&list_files=true`. Uncached hashes are **absent** (no `false` entry). With `list_files` → files with `id`, `name`, `size`, `short_name`, `mimetype`. `/usenet/checkcached` and `/webdl/checkcached` also report a cached **torrent** hash, so the cache index is keyed by hash across kinds | `06-*` |
| T10 usenet | **Unverified (blocked)** | `createusenetdownload` → **500 `PLAN_RESTRICTED_FEATURE`** ("not allowed to use usenet downloads"). `/usenet/checkcached` with NZB-file MD5s → empty. The NZB hash scheme is unknown | `07-*` |
| T11 web downloads | **Partly refuted** | **Direct HTTP links are not accepted** (`UNSUPPORTED_SITE`, HTTP 500). Only hosters listed in `GET /webdl/hosters` work (162 entries with `regex`, `domains`, `status`, daily link and bandwidth limits, `per_link_size_limit`). `hash` = **MD5 of the URL string**. An archive.org *item* URL is delivered as **one zip file** | `08-*`, `05-create-webdl-archive-item`, `05-poll-webdl-item-*` |
| T12 webhooks | **Refuted (shape)** | Needs **both** `webhook_url` and `webhook_notifications=true` in account settings (web UI; no documented API). Payload: `{"type":"download.completed","timestamp":"…+00:00","data":{"title":"Download Completed","message":"Download <name> has finished processing and is ready to download."}}`. **No item ID, hash or kind**, and **no signature header** (UA `TorBox API`, Sentry trace headers). It fired for an uncached web-download completion (~10 s after). It did **not** fire for a cached add or for deletions | `12-webhook-received.json` |
| T13 rate limits | **Confirmed, with values** | Headers `x-ratelimit-limit`, `x-ratelimit-remaining`, `x-ratelimit-reset` (epoch float), `retry-after`. Buckets are **per endpoint**. General: **300 per ~60 s**. **Uncached creates (torrent file/magnet, web download): 60 per hour.** Cached **torrent** creates use the 300/min bucket; web creates report the 60/hour bucket even for a cached re-add (`08-readd-same-webdl`). The CDN zip endpoint shows its own 60/min limit; CDN file downloads carry no rate-limit headers | headers in all transcripts; `02-create-magnet-uncached`, `04-cdn-zip-head` |
| T14 states | **Partly confirmed** | Observed `download_state` values: `stalledDL`, `downloading`, `stalled (no seeds)`, `completed`; web downloads: `downloading` → `processing` → `completed`. `metaDL`, `paused`, `uploading`, error states not observed. Third-party code mentions `metaDL_done` | `05-poll-multifile-*`, `05-poll-webdl-item-*` |
| T15 expiry | **Confirmed, with values** | `expires_at` = completion **+24 h for torrents**, **+30 days for web downloads** (this plan). Disappearance after expiry not observed | `02-poll-uncached-01`, `08-poll-webdl-01` |
| T16 slot queueing | **Unverified** | `user/me` has **no slot-limit field**, only `additional_concurrent_slots: 0`. The queue endpoint `GET /queued/getqueued` exists (empty). Slots were not flooded (by design) | `01-auth-user-me`, `02-baseline-queued` |
| T17 zip download | **Confirmed (single- and multi-file, see §5; zip ignores Range)** | `requestdl … &zip_link=true` (no `file_id`) → `…/zip/<uuid>` URL: `application/zip`, known `Content-Length`, `Accept-Ranges: bytes`. Multi-file zip **not verified**: the multi-file torrent stalled permanently at 98.15 % | `04-requestdl-zip`, `04-cdn-zip-head`, `05-status-multifile-final` |
| T18 User-Agent | **Confirmed** | `debroxy-discovery/0.1` and `Mozilla/5.0` accepted (API and CDN) | all |
| T19 account info | **Partly refuted** | `GET /user/me?settings=true` gives `plan` (int), `is_subscribed`, `premium_expires_at`, **`cooldown_until`**, `additional_concurrent_slots`, counters and settings (`allow_zipped`, `seed_torrents`, `cdn_selection`, `webhook_*`). **No slot limit and no capability list**: plan features (e.g. usenet) are discovered only by a failing call | `01-auth-user-me` |

## 2. New findings (not covered by revision 1)

| ID | Finding | Evidence |
|---|---|---|
| TB-N01 | **CDN URLs contain the API key.** A raw CDN link handed to a user leaks full account access. CDN URLs are secrets: never returned to clients, never logged, never cached in shared places | `04-requestdl-*` |
| TB-N02 | **CDN links cannot be revoked**: still valid after the item is deleted | teardown check |
| TB-N03 | **Capabilities depend on the plan** (usenet blocked on plan 1). There is no API to list plan features, so capability state must be learned from errors (`PLAN_RESTRICTED_FEATURE`) and cached per account | `07-create-nzb-debian10` |
| TB-N04 | **HTTP status is unreliable for errors.** Classify by the envelope `error` code; handle non-envelope FastAPI bodies (`detail` string or validation array) | §1 T02 |
| TB-N05 | **Two create budgets**: 300/min (cached) vs 60/hour (uncached). An uncached submit is far more expensive; per-user submit limits must account for it | T13 |
| TB-N06 | **File list unknown until completion** (`files: null`). File IDs (0-based ints) only exist once `download_finished`. A job cannot offer per-file links before that. For a *cached* hash, `checkcached?list_files=true` gives files up front | `05-poll-multifile-*`, `06-checkcached-object-listfiles` |
| TB-N07 | Torrents can **stall permanently** (`stalled (no seeds)` at 98 % for 40+ min, `active:true`, `eta: 8640000` sentinel). There is no terminal failure state, so a debroxy-side "stalled" signal is needed | `05-status-multifile-final` |
| TB-N08 | `eta: 8640000` (100 days) is the "unknown" sentinel; `progress` is 0–1 float | polls |
| TB-N09 | Web-download states include `processing` (`download_finished:true`, `download_present:false`) before `completed` | `05-poll-webdl-item-*` |
| TB-N10 | The webhook only says "something completed" by human-readable name. It can only trigger a full refresh, never a targeted update | `12-webhook-received.json` |
| TB-N11 | Items carry `alternative_hashes` (MD5-like 32-hex values plus the btih) and `cached`/`cached_at`, `owner` (account UUID), `tags[]`, `airlocked`, `server` | §3 |
| TB-N12 | The account has a `cooldown_until` timestamp (meaning unknown; possibly a create cooldown). Must be watched in revision 2 | `01-auth-user-me` |
| TB-N13 | An archive.org *item* web download becomes a single zip; hosters may repackage content | `05-poll-webdl-item-*` |
| TB-N14 | The lookup-by-ID param differs per kind: `torrent_id`, `web_id` (requestdl), `webdl_id` (control); `usenet_id` presumed | transcripts |
| TB-N15 | **A finished torrent seeds and reports `uploading`** or **`uploading (no peers)`**, with `download_finished: true`, `download_present: true`, `progress: 1` and its files listed. These states mean "done and seeding", not post-processing | live check 2026-10-13 (not recorded) |

## 3. Field catalogue

**Torrent item** (`/torrents/mylist`): `id`, `auth_id`, `server`, `hash`, `name`, `magnet` (null for file uploads), `torrent_file` (bool), `size`, `active`, `created_at`, `updated_at`, `download_state`, `seeds`, `peers`, `ratio`, `progress` (0–1), `download_speed`, `upload_speed`, `eta`, `expires_at`, `download_present`, `download_finished`, `files` (null until finished), `download_path`, `availability`, `tracker`, `total_uploaded`, `total_downloaded`, `cached`, `cached_at`, `owner`, `seed_torrent`, `allow_zipped`, `long_term_seeding`, `tracker_message`, `private`, `alternative_hashes`, `tags`, `airlocked`.

**Web download item** (`/webdl/mylist`): `id`, `created_at`, `updated_at`, `auth_id`, `name`, `hash` (MD5 of the URL), `download_state`, `download_speed`, `original_url`, `eta`, `progress`, `size`, `download_id`, `active`, `cached`, `download_present`, `download_finished`, `expires_at`, `error`, `cached_at`, `server`, `alternative_hashes`, `tags`, `airlocked`, `files`.

**File**: `id` (int, 0-based), `md5`, `hash`, `name` (`<folder>/<file>`), `short_name`, `size`, `zipped`, `s3_path`, `absolute_path`, `infected`, `mimetype`, `opensubtitles_hash`.

**Create responses**: torrent `{hash, torrent_id, auth_id}`; web `{hash, webdownload_id, auth_id, jdownloader_id, link_list}`.

**Hoster** (`/webdl/hosters`): `id`, `name`, `domains[]`, `url`, `status`, `type` (`hoster` | `stream`), `note`, `nsfw`, `daily_link_limit`, `daily_link_used`, `daily_bandwidth_limit`, `daily_bandwidth_used`, `per_link_size_limit`, `regex`.

## 4. Observed state transitions (map for revision 2)

| Observed | Proposed Lifecycle |
|---|---|
| `stalledDL` (0 %, no peers yet) | `downloading` + stalled |
| `downloading` | `downloading` |
| `stalled (no seeds)` | `downloading` + stalled (long stall → user-visible warning) |
| `processing` (webdl, finished but not present) | `processing` |
| `completed` with `download_present:true` | `ready` |
| cached create ("Found Cached Torrent") | straight to `ready` |
| `mylist?id=` → 404 `ITEM_NOT_FOUND` | `gone` |

## 5. Addendum: multi-file round (same day)

Test material: WebTorrent free torrents (Blender open movies plus the WIRED CD; Creative Commons; all **cached** at TorBox) and the Academic Torrents test torrent `d984f67a…` (3 files, not cached). Fixtures `23-*`, `25-*`, `28-*`.

| Topic | Observation | Fixtures |
|---|---|---|
| T17 multi-file zip | **Confirmed.** Sintel (11 files): `zip_link=true` → `application/zip`, `Content-Disposition: attachment; filename=Sintel.zip`, known `Content-Length` | `25-requestdl-sintel-zip`, `25-cdn-sintel-zip-head` |
| **Zip ignores Range** | The zip URL advertises `Accept-Ranges: bytes`, but a ranged GET returned **200 with the full body**. Zip downloads cannot be resumed; debroxy must not forward Range for zips, or must say so | `25-cdn-sintel-zip-range` |
| **File IDs ≠ torrent order** | Sintel.mp4 is index 5 in the `.torrent` but TorBox `id` 10; `files[]` is not sorted by `id`. File identity = provider `id` + path, never the torrent index | `25-sintel-mylist` |
| New state `cached` | A cached add shows `download_state: "cached"` (with `download_present:true`), not `completed` | `25-sintel-mylist` |
| archive.org torrents | Three archive.org torrents stalled at **98–99.7 %** (`stalled (no seeds)`) at both providers, even with only media files selected. The cause is unclear (webseed-only swarms). They don't work as test material; use well-seeded torrents | `05-*`, `21-*` |
| Interrupted submit still created the item | A create whose client call was interrupted still produced the item at TorBox (orphan risk: a provider item without a local record) | `28-delete-soundie13` |

Mapping update: `cached` → `ready`.

## 6. Not verified (needs another account, plan or time)

- Usenet in full (T10, usenet part of T14): **deferred**; the user decided to handle usenet later, and this plan has no usenet.
- Slot limit and queueing (T16), `as_queued`, `pause`/`resume`.
- Expiry disappearance (T15) and CDN link maximum lifetime.
- Webhook events other than `download.completed` (failures).
- Meaning of `cooldown_until`.

## 7. Addendum: re-add after deletion (2026-10-11, later)

| Topic | Observation | Fixtures |
|---|---|---|
| Re-add after delete | A cached torrent deleted and re-added right away gets a **new `torrent_id`** (116601526 → 116601554), although the content is still cached ("Found Cached Torrent"). Provider item IDs are not reused for re-added content. This supports ADR 0007's deferral reasoning and the late deletion check (DEC-39) | `30-readd-after-delete-*` |
