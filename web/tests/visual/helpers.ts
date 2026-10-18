import { type Browser, expect, type Page, type Route } from "@playwright/test";

// Visual-regression helpers (docs/frontend/revision2.md §14.3): fixed clock,
// theme via localStorage, and the API mocked with route interception.

export const NOW = "2026-10-13T12:00:00.000Z";
const NOW_MS = Date.parse(NOW);

export function iso(offsetMs: number): string {
  return new Date(NOW_MS + offsetMs).toISOString();
}

async function setTheme(page: Page, theme: "light" | "dark"): Promise<void> {
  await page.addInitScript((value) => {
    try {
      localStorage.setItem("debroxy.theme", value);
    } catch {
      // storage unavailable
    }
  }, theme);
}

// The time stays at NOW: a running clock turns "now" into "3 seconds ago" on a
// slow machine and shifts the table columns.
async function installClock(page: Page): Promise<void> {
  await page.clock.setFixedTime(new Date(NOW_MS));
}

export async function snapshot(page: Page, name: string): Promise<void> {
  await expect(page).toHaveScreenshot(`${name}.png`);
}

/**
 * Runs `fn` once per theme, each in a fresh context so the `localStorage`
 * theme value is isolated. `fn` ends with one or more `snapshot` calls.
 */
export async function themed(
  browser: Browser,
  fn: (page: Page, theme: "light" | "dark") => Promise<void>,
): Promise<void> {
  for (const theme of ["light", "dark"] as const) {
    const context = await browser.newContext({
      viewport: { width: 1440, height: 900 },
      locale: "en-US",
      timezoneId: "UTC",
    });
    const page = await context.newPage();
    await installClock(page);
    await setTheme(page, theme);
    await fn(page, theme);
    await context.close();
  }
}

// --- fixtures ---------------------------------------------------------------

export const configFixture = {
  version: "0.2.0",
  oidcEnabled: false,
  uploadLimitBytes: 10_485_760,
  signedLinkTTLSeconds: 21_600,
  passwordMinLength: 12,
};

export function meFixture(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    user: {
      id: "u-1",
      username: "alice",
      role: "user",
      status: "active",
      passwordLogin: true,
      oidcLinked: false,
      createdAt: iso(-36 * 3600_000),
    },
    quota: { slotsUsed: 2, maxActiveJobs: 3 },
    session: {
      idleExpiresAt: iso(2 * 3600_000),
      absoluteExpiresAt: iso(24 * 3600_000),
    },
    submitRate: {
      uncached: { used: 3, perHour: 10, resetsAt: iso(42 * 60_000) },
      cached: { used: 0, perHour: 60, resetsAt: null },
    },
    ...overrides,
  };
}

export function adminMeFixture(): Record<string, unknown> {
  return meFixture({ user: { ...(meFixture().user as Record<string, unknown>), role: "admin" } });
}

function capabilityFixture(name: string): Record<string, unknown> {
  return { name, supported: true, enabled: true, state: "available" };
}

function providerFixture(id: string, displayName: string, kind: string): Record<string, unknown> {
  return {
    id,
    kind,
    displayName,
    enabled: true,
    capabilities: [
      "SubmitMagnet",
      "SubmitTorrentFile",
      "SubmitWebURL",
      "CachedCheck",
      "PerFileDelivery",
      "BundleDelivery",
      "ProviderDedup",
    ].map(capabilityFixture),
  };
}

export const providersFixture = {
  providers: [
    providerFixture("torbox", "TorBox", "torbox"),
    providerFixture("realdebrid", "Real-Debrid", "realdebrid"),
  ],
};

const STATE_ORDER = [
  "submitting",
  "queued",
  "resolving_metadata",
  "awaiting_file_selection",
  "downloading",
  "processing",
  "ready",
  "failed",
  "gone",
  "deleting",
  "deleted",
];

export function jobFixture(
  state: string,
  index: number,
  overrides: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    id: `job-${index}`,
    displayName: `movie-${index}.mkv`,
    provider: "torbox",
    kind: "torrent",
    state,
    telemetry: {
      progress: 0.5,
      bytesPerSecond: 12_000_000,
      etaSeconds: 60,
      peers: 3,
      stalled: false,
    },
    sizeBytes: 754 * 1024 * 1024,
    expiresAt: iso(5 * 86400_000),
    shared: index % 2 === 0,
    slotCharged: index % 2 === 1,
    delivery: { perFile: true, bundle: { format: "zip", rangeSupported: false } },
    files: [
      { id: "f1", path: "movie.mkv", sizeBytes: 700 * 1024 * 1024 },
      { id: "f2", path: "movie.srt", sizeBytes: 2000 },
    ],
    createdAt: iso(-index * 3600_000),
    failureReason: null,
    ...overrides,
  };
}

export function manyJobsFixture(): { jobs: Record<string, unknown>[]; stale: boolean } {
  const jobs = STATE_ORDER.map((state, index) => jobFixture(state, index));
  jobs.push(
    jobFixture("downloading", 99, {
      displayName: "stalled-download",
      telemetry: {
        progress: 0.62,
        bytesPerSecond: 0,
        etaSeconds: null,
        peers: 18,
        stalled: true,
      },
    }),
  );
  return { jobs, stale: false };
}

export function historyFixture(): { entries: Record<string, unknown>[] } {
  return {
    entries: [0, 1, 2].map((index) => ({
      jobId: `job-${index}`,
      displayName: `movie-${index}`,
      provider: "torbox",
      kind: "torrent",
      name: `movie-${index}`,
      finalState: "ready",
      failureReason: null,
      claimedAt: iso(-(index + 2) * 86400_000),
      releasedAt: iso(-index * 86400_000),
    })),
  };
}

export const hostersFixture = {
  hosters: [
    {
      name: "1fichier",
      domains: ["1fichier.com", "alterupload.com"],
      up: true,
      dailyUsed: 12,
      dailyLimit: 50,
    },
    {
      name: "Rapidgator",
      domains: ["rapidgator.net"],
      up: true,
      dailyUsed: 0,
      dailyLimit: 30,
    },
    { name: "Mega", domains: ["mega.nz"], up: false, dailyUsed: 0, dailyLimit: null },
  ],
};

export function adminUserFixture(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "u-1",
    username: "alice",
    role: "admin",
    status: "active",
    passwordLogin: true,
    oidcLinked: false,
    createdAt: iso(-36 * 3600_000),
    ...overrides,
  };
}

// --- API mock ---------------------------------------------------------------

export interface ApiOverrides {
  config?: unknown;
  me?: unknown;
  providers?: unknown;
  jobs?: unknown;
  history?: unknown;
  job?: unknown;
  users?: unknown;
  user?: unknown;
  quota?: unknown;
  adminJobs?: unknown;
  unmanaged?: unknown;
  outbox?: unknown;
  hosters?: unknown;
  /** Raw SSE body for the event stream; undefined keeps it open (Connecting…). */
  events?: string;
}

function json(route: Route, body: unknown, status = 200): Promise<unknown> {
  return route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

/** Registers route interception for every endpoint the client calls. */
export async function mockApi(page: Page, overrides: ApiOverrides = {}): Promise<void> {
  await page.route(/\/api\/v1\/config$/, (route) => json(route, overrides.config ?? configFixture));
  await page.route(/\/api\/v1\/me$/, (route) => json(route, overrides.me ?? meFixture()));
  await page.route(/\/api\/v1\/providers$/, (route) =>
    json(route, overrides.providers ?? providersFixture),
  );
  await page.route(/\/api\/v1\/jobs\/history$/, (route) =>
    json(route, overrides.history ?? historyFixture()),
  );
  await page.route(/\/api\/v1\/jobs$/, (route) =>
    json(route, overrides.jobs ?? { jobs: [], stale: false }),
  );
  await page.route(/\/api\/v1\/jobs\/[^/]+$/, (route) =>
    json(route, overrides.job ?? jobFixture("ready", 1, { displayName: "Sintel" })),
  );
  await page.route(/\/api\/v1\/jobs\/[^/]+\/links$/, (route) =>
    json(route, {
      url: "http://localhost:4173/dl/token-1/Sintel.mp4",
      expiresAt: iso(3600_000),
      rangeSupported: true,
      fileId: "f1",
    }),
  );
  await page.route(/\/api\/v1\/admin\/jobs$/, (route) =>
    json(route, overrides.adminJobs ?? { jobs: [], stale: false }),
  );
  await page.route(/\/api\/v1\/admin\/users$/, (route) =>
    json(route, overrides.users ?? { users: [] }),
  );
  await page.route(/\/api\/v1\/admin\/users\/[^/]+$/, (route) =>
    json(route, overrides.user ?? adminUserFixture()),
  );
  await page.route(/\/api\/v1\/admin\/users\/[^/]+\/slot-quota$/, (route) =>
    json(route, overrides.quota ?? { maxActiveJobs: 3, isDefault: true, slotsUsed: 1 }),
  );
  await page.route(/\/api\/v1\/admin\/unmanaged-items$/, (route) =>
    json(route, overrides.unmanaged ?? { items: [] }),
  );
  await page.route(/\/api\/v1\/admin\/outbox$/, (route) =>
    json(route, overrides.outbox ?? { messages: [] }),
  );
  await page.route(/\/api\/v1\/providers\/[^/]+\/hosters$/, (route) =>
    json(route, overrides.hosters ?? hostersFixture),
  );
  await page.route(/\/api\/v1\/providers\/[^/]+\/cached-availability$/, async (route) => {
    const body = route.request().postDataJSON() as { fingerprints?: string[] };
    const cached = Object.fromEntries((body.fingerprints ?? []).map((fp) => [fp, true]));
    await json(route, { cached });
  });
  // The event stream stays open (Connecting…) unless a raw SSE body is given.
  await page.route(/\/api\/v1\/events/, (route) => {
    if (overrides.events === undefined) {
      return new Promise<never>(() => {});
    }
    return route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: overrides.events,
    });
  });
}
