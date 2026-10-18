import { test } from "@playwright/test";
import { manyJobsFixture, mockApi, snapshot, themed } from "./helpers";

test("jobs: many jobs", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { jobs: manyJobsFixture() });
    await page.goto("/");
    await snapshot(page, `jobs-many-${theme}`);
  });
});

test("jobs: empty", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { jobs: { jobs: [], stale: false } });
    await page.goto("/");
    await snapshot(page, `jobs-empty-${theme}`);
  });
});

test("jobs: stale banner", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { jobs: { ...manyJobsFixture(), stale: true } });
    await page.goto("/");
    await snapshot(page, `jobs-stale-${theme}`);
  });
});

test("jobs: filters active", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { jobs: manyJobsFixture() });
    await page.goto("/?group=ready&provider=torbox&q=movie");
    await snapshot(page, `jobs-filters-${theme}`);
  });
});

test("history: entries", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page);
    await page.goto("/history");
    await snapshot(page, `history-${theme}`);
  });
});
