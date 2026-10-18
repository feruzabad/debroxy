# Real-Debrid API discovery

| | |
|---|---|
| Date | 2026-10-11 |
| Account | `type: premium` |
| Method | ad-hoc curl, User-Agent `debroxy-discovery/0.1`; sanitized transcripts in [`docs/discovery/realdebrid/`](realdebrid/) |
| Test material | Debian 13.7 netinst (magnet and `.torrent`), archive.org file and Debian mirror URL (hoster tests) |
| Input to | `docs/specs/revision2.md` and later revisions |

## 1. Assumption register (revision 1 §9.2)

| ASM | Verdict | Observation | Fixtures |
|---|---|---|---|
| R01 base URL + Bearer | **Confirmed** | `https://api.real-debrid.com/rest/1.0`, `Authorization: Bearer <token>`. Invalid → 401 `{"error":"bad_token","error_code":8}`. Errors are always `{error, error_code}` | `01-auth-*` |
| R02 add torrent | **Confirmed** | `POST /torrents/addMagnet` (form `magnet=`) and `PUT /torrents/addTorrent` (raw body, `Content-Type: application/x-bittorrent`) → **201** `{id, uri}` | `02-add-magnet`, `03-*` |
| R03 file selection required | **Confirmed** | New items stay in `waiting_files_selection` until `POST /torrents/selectFiles/{id}` `files=all` → 204. Cached content is `downloaded` within 1 s of selecting | `02-poll-info-01`, `02-select-files-all`, `02-poll-after-select-01` |
| R04 torrent info | **Partly refuted (see §4: multi-file selections arrive as one RAR link)** | `GET /torrents/info/{id}`: `id`, `filename`, `original_filename`, `hash`, `bytes`, `original_bytes`, `host`, `split`, `progress` (0–100), `status`, `added`, `files[{id, path, bytes, selected}]`, `links[]`, `ended` (when downloaded). **File IDs are 1-based**; paths start with `/`. One hoster link per selected file. **`added` is local time (CEST) wrongly suffixed `Z`** (+2 h). `ended` reflects when RD first cached the content (a past date), not this item | `02-poll-*` |
| R05 unrestrict | **Confirmed** | `POST /unrestrict/link` `link=<hoster link>` → `{id, filename, mimeType, filesize, link, host, host_icon, chunks, crc, download, streamable}`. `download` = CDN URL `https://<n>.download.real-debrid.com/d/<token>/<filename>`. **Each unrestrict creates a `/downloads` history entry** (the same link reuses one entry) | `04-unrestrict-link`, `09-downloads-history` |
| R06 delete | **Confirmed** | `DELETE /torrents/delete/{id}` → 204; again → 404 `{"error":"unknown_ressource","error_code":7}`. `info` after delete → same 404 | `09-*` |
| R07 pagination | **Confirmed** | `GET /torrents` is newest first; `limit` + `page` or `limit` + `offset`; `X-Total-Count` header. A page past the end → **204 with empty body**. List items: `id`, `filename`, `hash`, `bytes`, `host`, `split`, `progress`, `status`, `added`, `links`, `ended`/`seeders`. **No files.** **List `status` was stale** (`magnet_conversion` while `info` said `waiting_files_selection`) | `13-*`, `03-dup-*` |
| R08 no cached check | **Confirmed** | `GET /torrents/instantAvailability/{hash}` → 403 `{"error":"disabled_endpoint","error_code":37}` | `06-instant-availability` |
| R09 states | **Mostly confirmed (see §4)** | Observed: `waiting_files_selection`, `downloaded`; `magnet_conversion` only in the stale list. Not observed: `queued`, `downloading`, `compressing`, `uploading`, `error`, `virus`, `dead`, `magnet_error` | polls |
| R10 no usenet | **Assumed** | No usenet endpoints exercised; none known | — |
| R11 rate limit | **Unverified** | No rate-limit headers on any response | all |
| R12 no zip | **Assumed** | No zip endpoint known | — |
| R13 no webhooks | **Assumed** | `/user` exposes no webhook settings | `01-auth-user` |
| R14 IP-locked CDN | **Refuted** | The CDN URL worked from a second host (different IPv4). It also **kept working after the torrent and its `/downloads` entry were deleted** | `04-cdn-*`, teardown check |

## 2. New findings

| ID | Finding | Evidence |
|---|---|---|
| RD-N01 | **No provider-side dedup.** Each add of the same content (magnet or `.torrent`) creates a new item. debroxy's own fingerprint dedup is the only protection against duplicate items and slot waste | `03-readd-same-magnet`, `03-add-same-as-torrentfile` |
| RD-N02 | **CDN links cannot be revoked** (they survive deletion). Like TorBox, CDN URLs must never reach users | teardown |
| RD-N03 | **Timestamps are mislabelled** (`added` = local time + `Z`). Use debroxy's own clock for ordering; never trust RD timestamps as UTC | `02-poll-info-01` |
| RD-N04 | **The list endpoint is stale** for status. Sync must call `info` per non-terminal item (cost: one request per item) | `13-torrents-all` vs `03-dup-*` |
| RD-N05 | **unrestrict creates history entries** in `/downloads`, which should be cleaned (`DELETE /downloads/delete/{id}` → 204) or it accumulates | `09-delete-download-history` |
| RD-N06 | Hoster support is narrow for our sources: archive.org and plain direct links → 400 `{"error":"hoster_unsupported","error_code":16}`. `/hosts/domains` lists 281 domains. `/unrestrict/check` → 404 `unavailable_file` (24) | `08-*` |
| RD-N07 | CDN: `Server: Lity 2.0`, HTTP/1.1, `Accept-Ranges: bytes`, 206 on Range, `Content-Type: application/force-download`, `Content-Disposition: attachment` **without filename**, **no ETag / Last-Modified** (no `If-Range` support possible). The filename is only in the URL path | `04-cdn-*` |
| RD-N08 | Errors carry a numeric `error_code`. Seen: 7 unknown resource, 8 bad token, 16 hoster unsupported, 24 unavailable file, 37 disabled endpoint | transcripts |
| RD-N09 | **`files=all` can select nothing.** For a split-RAR release (63 files: `.rar`, `.r00`…`.r59`, `.nfo`, `.sfv`), `selectFiles` with `files=all` answered 204, yet `info` showed 0 files selected, `bytes: 0` and status `downloading`, which never progressed. Naming every file ID (`files=1,2,…,63`) selected all 63. A second `selectFiles` on the stuck item (202) changed nothing | live check 2026-10-13 (not recorded) |
| RD-N10 | **A re-add can return an existing item.** Adding the same magnet twice within a minute returned the first add's ID the second time (that item had all files selected); an older item of the same content in `downloading` with nothing selected was not returned. Contradicts RD-N01 in this case; the rule is unknown | live check 2026-10-13 (not recorded) |

## 3. Cross-provider finding

- **IPv6 egress.** This host reached both providers over IPv6, while the second test host used IPv4. A dual-stack debroxy host can present **two IPs** (v4 and v6) to a provider, so the single-IP goal needs an IP-family pin per provider in configuration.

## 4. Addendum: uncached and multi-file round (same day)

Test material: archive.org public-domain torrents (uncached; they stalled), WebTorrent free torrents (Sintel, Big Buck Bunny, Cosmos Laundromat). Fixtures `20-*` … `28-*`.

| Topic | Observation | Fixtures |
|---|---|---|
| R09 states | **Mostly confirmed.** Full path observed: `waiting_files_selection` → `queued` → `downloading` (progress 0–100, `speed`, `seeders`) → **`uploading`** (progress **resets to 0**, then 100) → `downloaded`. Error states still unobserved | `20-poll-avenge-*`, `26-poll-sintel-*` |
| Files before download | `.torrent` uploads show the **full file list immediately** (in `waiting_files_selection`), unlike TorBox | `20-poll-avenge-pre-01`, `26-sintel-info-pre` |
| File IDs | 1-based and **in torrent order** (Sintel: torrent index 5 = RD id 6) | `26-sintel-info-pre` |
| **R04 refuted: RAR packaging** | Selecting **more than one file** gave **one link** to a **RAR archive** (`Sintel.rar`, `Big Buck Bunny.rar`, `application/x-rar-compressed`), for both a subset (3/11) and all files (3/3). Selecting **exactly one file** gave a direct link to that file (`Cosmos Laundromat.en.srt`). Per-file links therefore need one RD item per file | `26-unrestrict-sintel-link`, `27-*` |
| Stalls | archive.org torrents stalled at 98.5–99.7 % in `downloading` with a trickle `speed`. RD has no stalled state; it stays `downloading` | `24-info-before-delete-*` |
| Mislabelled time | `added` again local time + `Z` | all info fixtures |

Mapping update: `uploading` → `processing` (confirmed); long `downloading` with no progress → debroxy-side stall detection.

## 5. Not verified

- Error states (`error`, `virus`, `dead`, `magnet_error`, `compressing`).
- Rate limits (R11).
- Unrestrict of a supported hoster link (only RD's own torrent links were used).
- CDN link maximum lifetime.
