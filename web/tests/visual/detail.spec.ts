import { test } from "@playwright/test";
import { iso, jobFixture, mockApi, snapshot, themed } from "./helpers";

test("job detail: ready multi-file tree", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    const job = jobFixture("ready", 1, {
      displayName: "Sintel",
      sizeBytes: 123_456_789,
      files: [
        { id: "f1", path: "Sintel/Sintel.mp4", sizeBytes: 120_000_000 },
        { id: "f2", path: "Sintel/Sintel.srt", sizeBytes: 2100 },
        { id: "f3", path: "Sintel/Extras/poster.jpg", sizeBytes: 98_000 },
      ],
    });
    await mockApi(page, { job });
    await page.goto("/jobs/job-1");
    await snapshot(page, `detail-ready-${theme}`);
  });
});

test("job detail: downloading", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    const job = jobFixture("downloading", 1, {
      displayName: "debian-13.7.0-amd64-netinst",
      telemetry: {
        progress: 0.624,
        bytesPerSecond: 12_300_000,
        etaSeconds: 41,
        peers: 18,
        stalled: false,
      },
    });
    await mockApi(page, { job });
    await page.goto("/jobs/job-1");
    await snapshot(page, `detail-downloading-${theme}`);
  });
});

test("job detail: failed", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    const job = jobFixture("failed", 1, { failureReason: "provider-rejected" });
    await mockApi(page, { job });
    await page.goto("/jobs/job-1");
    await snapshot(page, `detail-failed-${theme}`);
  });
});

test("job detail: RD bundle-only", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    const job = jobFixture("ready", 1, {
      displayName: "Sintel",
      provider: "realdebrid",
      delivery: { perFile: false, bundle: { format: "rar", rangeSupported: false } },
    });
    await mockApi(page, { job });
    await page.goto("/jobs/job-1");
    await snapshot(page, `detail-bundle-only-${theme}`);
  });
});

test("job detail: released", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    const entry = {
      jobId: "job-1",
      displayName: "Sintel",
      provider: "torbox",
      kind: "torrent",
      name: "Sintel",
      finalState: "ready",
      failureReason: null,
      claimedAt: iso(-2 * 86400_000),
      releasedAt: iso(-3600_000),
    };
    await mockApi(page, {
      job: jobFixture("ready", 1, { displayName: "Sintel" }),
      events: `event: job.released\ndata: ${JSON.stringify({ jobId: "job-1", historyEntry: entry })}\n\n`,
    });
    await page.goto("/jobs/job-1");
    await page.getByText("This job was released.").waitFor();
    await snapshot(page, `detail-released-${theme}`);
  });
});

test("player: video", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    const job = jobFixture("ready", 1, {
      displayName: "Sintel",
      files: [{ id: "f1", path: "Sintel.mp4", sizeBytes: 120_000_000 }],
    });
    await mockApi(page, { job });
    // Keep the media element loading (never deliver the video bytes).
    await page.route(/\/dl\//, () => new Promise<never>(() => {}));
    await page.goto("/jobs/job-1");
    await page.getByRole("button", { name: "Play" }).click();
    await page.getByText("Loading…").waitFor({ state: "hidden" });
    await snapshot(page, `player-video-${theme}`);
  });
});

test("player: warning banner", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    const job = jobFixture("ready", 1, {
      displayName: "Sintel",
      files: [{ id: "f1", path: "Sintel.mkv", sizeBytes: 120_000_000 }],
    });
    await mockApi(page, { job });
    await page.route(/\/dl\//, () => new Promise<never>(() => {}));
    await page.goto("/jobs/job-1");
    await page.getByRole("button", { name: "Play" }).click();
    await page.getByText(/Your browser may not be able to play/).waitFor();
    await snapshot(page, `player-warning-${theme}`);
  });
});

test("player: can't play", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    const job = jobFixture("ready", 1, {
      displayName: "Sintel",
      files: [{ id: "f1", path: "Sintel.mp4", sizeBytes: 120_000_000 }],
    });
    await mockApi(page, { job });
    await page.route(/\/dl\//, (route) => route.fulfill({ status: 404 }));
    await page.goto("/jobs/job-1");
    await page.getByRole("button", { name: "Play" }).click();
    await page.getByText("This file can't be played here.").waitFor();
    await snapshot(page, `player-cant-play-${theme}`);
  });
});
