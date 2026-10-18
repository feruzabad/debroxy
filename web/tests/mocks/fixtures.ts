import type { components as deliveryComponents } from "@/api/generated/delivery";
import type { components as identityComponents } from "@/api/generated/identity";
import type { components } from "@/api/generated/job";
import type { ClientConfig } from "@/api/types-rev13";

export type Job = components["schemas"]["Job"];
export type Me = components["schemas"]["Me"];
export type Provider = components["schemas"]["Provider"];
export type Capability = components["schemas"]["Capability"];
export type HistoryEntry = components["schemas"]["HistoryEntry"];
export type SignedLink = deliveryComponents["schemas"]["SignedLink"];
export type AdminUser = identityComponents["schemas"]["User"];
export type SlotQuota = components["schemas"]["SlotQuota"];
export type Hoster = components["schemas"]["Hoster"];

export const configFixture: ClientConfig = {
  version: "0.2.0",
  oidcEnabled: false,
  uploadLimitBytes: 10_485_760,
  signedLinkTTLSeconds: 21_600,
  passwordMinLength: 12,
};

export function meFixture(overrides: Partial<Me> = {}): Me {
  return {
    user: {
      id: "u-1",
      username: "alice",
      role: "user",
      status: "active",
      passwordLogin: true,
      oidcLinked: false,
      createdAt: "2026-10-12T10:00:00Z",
    },
    quota: { slotsUsed: 2, maxActiveJobs: 3 },
    session: {
      idleExpiresAt: new Date(Date.now() + 3_600_000).toISOString(),
      absoluteExpiresAt: new Date(Date.now() + 12 * 3_600_000).toISOString(),
    },
    submitRate: {
      uncached: { used: 3, perHour: 10, resetsAt: new Date(Date.now() + 1_800_000).toISOString() },
      cached: { used: 0, perHour: 60, resetsAt: null },
    },
    ...overrides,
  };
}

export function jobFixture(overrides: Partial<Job> = {}): Job {
  return {
    id: "job-1",
    displayName: "Sintel",
    provider: "torbox",
    kind: "torrent",
    state: "downloading",
    telemetry: { progress: 0.5, bytesPerSecond: 100, etaSeconds: 60, peers: 3, stalled: false },
    sizeBytes: 1000,
    expiresAt: null,
    shared: false,
    slotCharged: true,
    delivery: { perFile: true, bundle: { format: "zip", rangeSupported: false } },
    files: [{ id: "f1", path: "a.mp4", sizeBytes: 1000 }],
    createdAt: "2026-10-13T00:00:00Z",
    failureReason: null,
    ...overrides,
  };
}

function capability(name: string, overrides: Partial<Capability> = {}): Capability {
  return { name, supported: true, enabled: true, state: "available", ...overrides };
}

export function historyEntryFixture(overrides: Partial<HistoryEntry> = {}): HistoryEntry {
  const displayName = overrides.displayName ?? "Sintel";
  return {
    jobId: "job-1",
    displayName,
    provider: "torbox",
    kind: "torrent",
    name: displayName,
    finalState: "ready",
    failureReason: null,
    claimedAt: "2026-10-10T00:00:00Z",
    releasedAt: "2026-12-10T00:00:00Z",
    ...overrides,
  };
}

export function signedLinkFixture(overrides: Partial<SignedLink> = {}): SignedLink {
  return {
    url: "http://localhost/dl/token-1/Sintel.mp4",
    expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
    rangeSupported: true,
    fileId: "f1",
    ...overrides,
  };
}

export function providersFixture(): { providers: Provider[] } {
  return {
    providers: [
      {
        id: "torbox",
        kind: "torbox",
        displayName: "TorBox",
        enabled: true,
        capabilities: [
          capability("SubmitMagnet"),
          capability("SubmitTorrentFile"),
          capability("SubmitWebURL"),
          capability("CachedCheck"),
          capability("PerFileDelivery"),
          capability("BundleDelivery"),
          capability("ProviderDedup"),
        ],
      },
      {
        id: "realdebrid",
        kind: "realdebrid",
        displayName: "Real-Debrid",
        enabled: true,
        capabilities: [
          capability("SubmitMagnet"),
          capability("SubmitTorrentFile"),
          capability("SubmitWebURL"),
          capability("CachedCheck"),
          capability("PerFileDelivery"),
          capability("BundleDelivery"),
          capability("ProviderDedup"),
        ],
      },
    ],
  };
}

export function adminMeFixture(): Me {
  return meFixture({ user: { ...meFixture().user, role: "admin" } });
}

export function adminUserFixture(overrides: Partial<AdminUser> = {}): AdminUser {
  return {
    id: "u-1",
    username: "alice",
    role: "admin",
    status: "active",
    passwordLogin: true,
    oidcLinked: false,
    createdAt: "2026-10-12T10:00:00Z",
    ...overrides,
  };
}

export function slotQuotaFixture(overrides: Partial<SlotQuota> = {}): SlotQuota {
  return { maxActiveJobs: 3, isDefault: true, slotsUsed: 1, ...overrides };
}

export function hosterFixture(overrides: Partial<Hoster> = {}): Hoster {
  return {
    name: "1fichier",
    domains: ["1fichier.com", "alterupload.com"],
    up: true,
    dailyUsed: 12,
    dailyLimit: 50,
    ...overrides,
  };
}
