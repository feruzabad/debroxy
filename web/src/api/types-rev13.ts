import type { components as clientComponents } from "./generated/client";
import type { components } from "./generated/job";

// Names for the revision 13/14 shapes, taken from the generated OpenAPI types,
// plus the Event Stream payloads, which the OpenAPI files describe only in
// prose (docs/specs/revision14.md §5.11).

export type ClientConfig = clientComponents["schemas"]["ClientConfig"];

/** `/me` (§10.4) with `session` and `submitRate`. */
export type Me = components["schemas"]["Me"];

/** `GET /api/v1/admin/users/{id}/slot-quota` (§10.5). */
export type SlotQuotaView = components["schemas"]["SlotQuota"];

// Event Stream payloads (§5.11). `job.upserted` carries the generated `Job` and
// `quota.updated` the generated `QuotaUsage`; the envelope shapes below are new.

export type EventScope = "own" | "all";

export interface ReadyEvent {
  scope: EventScope;
}

export interface JobReleasedEvent {
  jobId: string;
  historyEntry: components["schemas"]["HistoryEntry"];
}

export interface ProviderStaleEvent {
  providerId: string;
  stale: boolean;
}

// Client-only provider augmentation: the `provider.stale` events keep a `stale`
// flag per provider under `['providers']` (docs/frontend/revision2.md §5.2.2).
export type ProviderWithStale = components["schemas"]["Provider"] & { stale?: boolean };

export interface ProviderListWithStale {
  providers: ProviderWithStale[];
}
