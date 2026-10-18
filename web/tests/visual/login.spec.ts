import { test } from "@playwright/test";
import { configFixture, mockApi, snapshot, themed } from "./helpers";

test("login empty", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page);
    await page.goto("/login");
    await snapshot(page, `login-empty-${theme}`);
  });
});

test("login with SSO button", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { config: { ...configFixture, oidcEnabled: true } });
    await page.goto("/login");
    await snapshot(page, `login-sso-${theme}`);
  });
});

test("login throttled countdown", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page);
    await page.route(/\/api\/v1\/auth\/login$/, (route) =>
      route.fulfill({
        status: 429,
        contentType: "application/json",
        body: JSON.stringify({ slug: "login-throttled", message: "", retryAfter: 42 }),
      }),
    );
    await page.goto("/login");
    await page.getByRole("textbox", { name: "Username" }).fill("alice");
    await page.getByRole("textbox", { name: "Password" }).fill("wrong");
    await page.getByRole("button", { name: "Log in" }).click();
    await page.getByText("Too many attempts.").waitFor();
    await snapshot(page, `login-throttled-${theme}`);
  });
});
