import { test } from "@playwright/test";
import { meFixture, mockApi, snapshot, themed } from "./helpers";

test("account: password user", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page);
    await page.goto("/account");
    await snapshot(page, `account-password-${theme}`);
  });
});

test("account: SSO-only user", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, {
      me: meFixture({
        user: {
          id: "u-1",
          username: "alice",
          role: "user",
          status: "active",
          passwordLogin: false,
          oidcLinked: true,
          createdAt: "2026-10-12T10:00:00.000Z",
        },
      }),
    });
    await page.goto("/account");
    await snapshot(page, `account-sso-${theme}`);
  });
});
