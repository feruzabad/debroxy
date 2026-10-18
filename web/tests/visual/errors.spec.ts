import { test } from "@playwright/test";
import { mockApi, snapshot, themed } from "./helpers";

test("error: not found", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page);
    await page.goto("/does-not-exist");
    await page.getByText("Page not found").waitFor();
    await snapshot(page, `error-404-${theme}`);
  });
});

test("error: forbidden", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page);
    await page.goto("/admin/users");
    await page.getByText("Admins only").waitFor();
    await snapshot(page, `error-403-${theme}`);
  });
});

test("error: bootstrap failure", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page);
    await page.route(/\/api\/v1\/config$/, (route) =>
      route.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({ slug: "internal-error", message: "boom" }),
      }),
    );
    await page.goto("/");
    await page.getByText("Can't load debroxy").waitFor();
    await snapshot(page, `error-bootstrap-${theme}`);
  });
});
