import { test } from "@playwright/test";
import { mockApi, snapshot, themed } from "./helpers";

const MAGNET = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567";

test("add job: magnet tab", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { jobs: { jobs: [], stale: false } });
    await page.goto("/add");
    await snapshot(page, `add-magnet-${theme}`);
  });
});

test("add job: torrent tab", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { jobs: { jobs: [], stale: false } });
    await page.goto("/add?source=torrent");
    await snapshot(page, `add-torrent-${theme}`);
  });
});

test("add job: web tab", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { jobs: { jobs: [], stale: false } });
    await page.goto("/add?source=web");
    await snapshot(page, `add-web-${theme}`);
  });
});

test("add job: cached hint", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { jobs: { jobs: [], stale: false } });
    await page.goto("/add");
    await page.getByLabel("Magnet link").fill(MAGNET);
    await page.getByText("Cached at TorBox").waitFor();
    await snapshot(page, `add-cached-${theme}`);
  });
});

test("add job: slot-quota alert", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { jobs: { jobs: [], stale: false } });
    await page.route(/\/api\/v1\/jobs$/, (route) => {
      if (route.request().method() !== "POST") {
        return route.fallback();
      }
      return route.fulfill({
        status: 429,
        contentType: "application/json",
        body: JSON.stringify({ slug: "slot-quota-exceeded", message: "No free slots." }),
      });
    });
    await page.goto("/add");
    await page.getByLabel("Magnet link").fill(MAGNET);
    await page.getByRole("button", { name: "Add job" }).click();
    await page.getByText(/Release a job to add another/).waitFor();
    await snapshot(page, `add-slot-quota-${theme}`);
  });
});

test("add job: submit-rate countdown", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { jobs: { jobs: [], stale: false } });
    await page.route(/\/api\/v1\/jobs$/, (route) => {
      if (route.request().method() !== "POST") {
        return route.fallback();
      }
      return route.fulfill({
        status: 429,
        contentType: "application/json",
        body: JSON.stringify({ slug: "submit-rate-exceeded", message: "", retryAfter: 90 }),
      });
    });
    await page.goto("/add");
    await page.getByLabel("Magnet link").fill(MAGNET);
    await page.getByRole("button", { name: "Add job" }).click();
    await page.getByText(/submissions this hour/).waitFor();
    await snapshot(page, `add-submit-rate-${theme}`);
  });
});
