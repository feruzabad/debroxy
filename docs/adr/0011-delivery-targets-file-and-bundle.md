# 0011. Delivery targets: file and bundle

| | |
|---|---|
| Status | Accepted (2026-10-11, project owner); the path in rule 13 amended by [ADR 0018](0018-api-paths-without-colon-methods.md) |
| Date | 2026-10-11 |
| Deciders | project owner |
| Spec | revision 7 (unchanged sections from revisions 5–6 apply), §1.4, §2 (Delivery Target, Bundle Format), §5.1 (`files`, `bundle`, `perFileDelivery`), §5.9, §6.2 `OpenDeliveryStream`, §6.3, §7.1–§7.3, §10.1, §10.2 ; decisions DEC-29, DEC-15 (with DEC-16, DEC-18, DEC-40) ; change C5-09 |
| Related | ADR 0003, ADR 0007, ADR 0010, ADR 0013; ddd-rules OUT-3, R-9, DEP-5, D-4; spec changes C-04, C-16; open questions OQ-12, OQ-13; assumptions ASM-R15, ASM-U01; evidence TorBox T07, T11, T17, TB-N06, TB-N14, torbox.md §5; Real-Debrid R03, R05, RD-N01, realdebrid.md §4 |

Revised 2026-10-11 for revision 5.

## Context
A Remote Item may contain one file or many. Users want single files, the whole item, or both. What each provider can deliver differs, and debroxy offers only what the provider API can do (§1.2).

Observed provider behaviour:
- **TorBox torrents** deliver one file via `requestdl … file_id=<id>` and the whole item as a zip via `requestdl … zip_link=true` (torbox.md T07, T17; fixtures `25-requestdl-sintel-file10.http`, `25-requestdl-sintel-zip.http`). The zip has a known `Content-Length` (`25-cdn-sintel-zip-head.http`) but **ignores Range**: a ranged GET returned 200 with the full body (torbox.md §5; `25-cdn-sintel-zip-range.http`).
- **TorBox web downloads** hold one file and deliver it via `webdl/requestdl` with `web_id` and `file_id=0` (`08-webdl-requestdl.http`; TB-N14). An archive.org item URL arrived as that one file, which is itself a zip (torbox.md T11). A zip bundle of a web item was not tested.
- **TorBox usenet** delivery is unverified: the account's plan rejects usenet creates, and the endpoints are presumed to mirror the torrent ones (ASM-U01).
- TorBox `files` is `null` until the download finishes; a cached add reports files at once (TB-N06; `05-poll-multifile-*.http`, `25-sintel-mylist.http`).
- **TorBox file IDs are not torrent order.** Sintel.mp4 is index 5 in the `.torrent` but TorBox `id` 10, and `files[]` is not sorted by `id` (torbox.md §5; `25-sintel-mylist.http`).
- **Real-Debrid** needs a file selection before it downloads (R03). Selecting **more than one file returns one link to a RAR archive**, for a subset and for all files. Selecting exactly one file returns a direct link to that file (realdebrid.md §4; `26-unrestrict-sintel-link.http`, `27-info-bbb.http`, `27-info-cosmos.http`). The link becomes a CDN URL through unrestrict (R05).
- Real-Debrid file IDs are 1-based and in torrent order (realdebrid.md §4; `26-sintel-info-pre.http`). Whether the RAR CDN link honours Range is **not verified** (ASM-R15); Real-Debrid file links do.
- Real-Debrid does not deduplicate: every add creates a new item (RD-N01).

Constraints: emulating missing features (local zip or RAR creation, unpacking) is out of scope (§1.4), and debroxy stores no files (NFR-01). The `job` context must stay provider-neutral (DEC-15, R-9).

## Decision
A Signed Link points to exactly one **Delivery Target**: `file:<FileID>` or `bundle`. The provider adapter decides which targets an item offers; `job` stores the result; `delivery` serves it through `provider`'s `OpenDeliveryStream` (ADR 0010).

**Targets**
1. `file:<FileID>` is available when the item is `ready`, the file is reported, and `perFileDelivery = true`.
2. `bundle` is available when the item is `ready` and `bundle` is present.
3. `BundleAvailability{Format, RangeSupported}` is a struct value object. `Format` is a D-4 struct enum value object with the values `zip` and `rar`. The two values name archive formats, not providers; the glossary (§2, Bundle Format) records which provider produces which, so `job` stays provider-neutral (R-9).
4. Signed Link payloads encode the target as `tgt = f:<FileID>` or `tgt = b` (ADR 0010). The API request body is `{target: {fileId}}` or `{target: {bundle: true}}`.

**File identity**
5. A Remote File's identity is the provider's file ID, stored as a string: TorBox `id` (0-based integer), Real-Debrid `id` (1-based integer). Never the position in the `.torrent`, never the order of `files[]`. The path is for display only.
6. Files are empty until the provider reports them. A request for an unknown file returns `target-not-available` (409).

**Per provider and kind** (adapter mapping, OUT-3; §5.9, C5-09)
7. **TorBox torrents** (verified): `perFileDelivery = true` and `bundle = {zip, RangeSupported: false}`. `file` resolves via `requestdl` with `file_id`; `bundle` via `requestdl` with `zip_link=true`.
8. **TorBox web** (verified for the single file): `perFileDelivery = true`, **no bundle in v1**. The single `file` resolves via `webdl/requestdl` with `web_id` and the reported file ID. When the delivered file is itself an archive, it is still a `file` target; debroxy does not look inside it.
9. **TorBox usenet** (unverified, ASM-U01): the adapter maps it like torrents (`perFileDelivery = true`, `bundle = {zip, false}`, resolved through the usenet `requestdl`), because the endpoints are presumed to mirror the torrent ones. The usenet capabilities start `unverified` (§6.1), so the API already tells clients this path is unproven. The mapping is confirmed or changed when a plan with usenet is available (§15 step 4).
10. **Real-Debrid** (torrents only; DEC-16): the adapter selects **all** files right after creation (DEC-15) and counts the selected files:
    - exactly one → `perFileDelivery = true`, **no bundle**: the file is the whole item, so no wrapper exists. `file` resolves by unrestricting the item's single link;
    - more than one → `perFileDelivery = false`, `bundle = {rar, RangeSupported: true}`. **`RangeSupported: true` is an assumption (ASM-R15, unverified)**; Real-Debrid file links honour Range, RAR links were not tested. `bundle` resolves by unrestricting the single link.
11. If a target marked `RangeSupported: true` answers a ranged request with 200, the one runtime mechanism of ADR 0010 rule 20 applies: the client gets 200, the full body and `Accept-Ranges: none`. If discovery refutes ASM-R15, the adapter sets `RangeSupported: false` for RAR bundles, and ADR 0010 rule 19 (Range ignored) applies from the start.
12. Per-file delivery for Real-Debrid multi-file items is **not offered** in v1 (§1.4). `IssueSignedLink` for a file target on such an item returns `target-not-available` (409).

**Clients**
13. `IssueSignedLinksForAllFiles` (`POST /jobs/{id}/links:all`) returns one link per file when `perFileDelivery = true`, otherwise one bundle link. Links come in the stored order of `files`, which is the order the provider reported. It is not sorted by file ID or path, and clients must not infer anything from it.
14. The job representation shows `delivery: {perFile, bundle: {format, rangeSupported}}` (§10.2), so a client knows a RAR needs client-side extraction and a zip cannot resume.
15. Each link is an independent target. Every download through it is a separate stream with its own upstream request, subject to the per-user and global stream caps (DEC-18, ADR 0010).

**No emulation**
16. debroxy never builds an archive, never unpacks a RAR or zip, and never extracts a single file from a bundle.

## Consequences
### Positive
- One small target model covers both providers and maps 1:1 to provider calls.
- Each advertised target rests on an observed provider call, except the two marked assumptions (usenet mapping, RAR Range).
- File identity stays correct even though TorBox reorders files.
- No local CPU, disk or memory cost for archives; NFR-01 holds.
- `job` stays provider-neutral: it sees only `perFileDelivery` and `bundle`.

### Negative
- Real-Debrid users with multi-file torrents get only a RAR and must extract it themselves.
- TorBox zip downloads cannot be resumed.
- A user who wants one file from a large Real-Debrid torrent downloads the whole RAR.
- TorBox web items offer no zip bundle in v1, even if TorBox might build one.
- `links:all` on a many-file TorBox torrent returns many links; a client that starts them all at once hits the stream cap (4 per user by default) and gets 503 for the rest.

### Risks and mitigations
- **ASM-R15 is wrong** (RAR ignores Range). Mitigation: ADR 0010 rule 20 at runtime; discovery top-up (§15 step 4) to fix the static flag.
- **ASM-U01 is wrong** (usenet delivery differs). Mitigation: usenet capabilities are `unverified` in the API; adapter fixtures are added with the first usenet-capable plan.
- **A provider reports file IDs that change between polls.** Not observed. Mitigation: fixtures as regression tests; a missing file ID makes the target unavailable instead of serving another file.
- **Selecting all files on Real-Debrid wastes provider capacity** for users who wanted one file. Accepted for v1; OQ-12 tracks the alternative.

## Alternatives considered
**One Real-Debrid item per file** (OQ-12). Real-Debrid does not deduplicate (RD-N01), so debroxy could create one item per selected file and get a direct link for each. It multiplies items, slot usage and rate-budget spending, and breaks the one-item-per-fingerprint model (ADR 0007). Left open for a later revision, not adopted for v1.

**Local zip creation while streaming.** debroxy would fetch each file and build a zip on the fly. It is the emulation §1.4 excludes. It also needs one CDN request per file, cannot know `Content-Length` up front without extra work, and cannot resume.

**Files only.** Offer only `file` targets. TorBox would lose its zip, and Real-Debrid multi-file items would offer nothing, because they have no per-file links.

**Same targets for every TorBox kind** (previous draft). Simpler adapter code, but it would advertise a zip for web items and per-file targets for usenet that no observation supports (C5-09).

## Compliance
- Adapter tests (T-5) on `docs/discovery/`: `25-sintel-mylist.http` maps Sintel.mp4 to FileID `"10"`, not `"5"`; TorBox torrents get `bundle = {zip, false}`; a TorBox web item (`05-poll-webdl-item-*.http`, `08-webdl-requestdl.http`) gets `perFileDelivery = true` and no bundle; Real-Debrid `26-*` / `27-info-bbb.http` give `perFileDelivery = false` and `bundle = {rar, true}`; `27-info-cosmos.http` gives `perFileDelivery = true` and no bundle.
- Adapter test: the Real-Debrid adapter calls `selectFiles` with `files=all` after every create (`02-select-files-all.http`).
- App tests for `DeliveryTarget`: file before files are reported, file on a bundle-only item, bundle on an item without bundle (including TorBox web) → `target-not-available`.
- App test for `IssueSignedLinksForAllFiles`: link order equals the stored `files` order.
- Domain tests (T-1): `Format` accepts only `zip` and `rar`; `BundleAvailability` cannot be built with a zero `Format`.
- Review checklist: no code in `delivery` or `job` builds, unpacks or slices archives; no torrent index used as a file ID; every new kind mapping cites a fixture or an `ASM-*`.
- Open before acceptance: none from revision 5. ASM-R15 and ASM-U01 stay open as assumptions, not as blockers.
