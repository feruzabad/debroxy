import { test } from "@playwright/test";
import {
  adminMeFixture,
  adminUserFixture,
  iso,
  manyJobsFixture,
  mockApi,
  snapshot,
  themed,
} from "./helpers";

const adminUsers = {
  users: [
    adminUserFixture({ id: "u-1", username: "alice", role: "admin" }),
    adminUserFixture({ id: "u-2", username: "bob", role: "user" }),
    adminUserFixture({ id: "u-3", username: "carol", role: "user", status: "disabled" }),
    adminUserFixture({
      id: "u-4",
      username: "dave",
      role: "user",
      passwordLogin: false,
      oidcLinked: true,
    }),
  ],
};

test("admin users: list", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { me: adminMeFixture(), users: adminUsers });
    await page.goto("/admin/users");
    await snapshot(page, `admin-users-${theme}`);
  });
});

test("admin users: new-user dialog", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { me: adminMeFixture(), users: adminUsers });
    await page.goto("/admin/users/new");
    await snapshot(page, `admin-users-new-${theme}`);
  });
});

test("admin users: detail", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, {
      me: adminMeFixture(),
      user: adminUserFixture({ id: "u-2", username: "bob", role: "user" }),
      quota: { maxActiveJobs: 5, isDefault: false, slotsUsed: 2 },
    });
    await page.goto("/admin/users/u-2");
    await snapshot(page, `admin-user-detail-${theme}`);
  });
});

test("admin jobs: with owner column", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    const jobs = manyJobsFixture().jobs.map((job, index) => ({
      ...job,
      ownerUsername: index % 2 === 0 ? "alice" : "bob",
    }));
    await mockApi(page, { me: adminMeFixture(), adminJobs: { jobs, stale: false } });
    await page.goto("/admin/jobs");
    await snapshot(page, `admin-jobs-${theme}`);
  });
});

test("admin jobs: expanded read-only drawer", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    const jobs = manyJobsFixture().jobs.map((job, index) => ({
      ...job,
      ownerUsername: index % 2 === 0 ? "alice" : "bob",
      files: [
        { id: `f-${index}-1`, path: "Season 1/episode-01.mkv", sizeBytes: 734_003_200 },
        { id: `f-${index}-2`, path: "Season 1/episode-02.mkv", sizeBytes: 712_000_000 },
        { id: `f-${index}-3`, path: "README.txt", sizeBytes: 2048 },
      ],
    }));
    const target = jobs.find((job) => job.ownerUsername === "bob");
    if (target === undefined) {
      throw new Error("no job owned by bob");
    }
    await mockApi(page, { me: adminMeFixture(), adminJobs: { jobs, stale: false } });
    await page.goto("/admin/jobs");
    await page
      .getByRole("row")
      .filter({ hasText: String((target as { displayName?: unknown }).displayName) })
      .first()
      .click();
    await page.getByText("README.txt").waitFor();
    await snapshot(page, `admin-jobs-drawer-${theme}`);
  });
});

test("admin providers: capability matrix and hosters", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { me: adminMeFixture() });
    await page.goto("/admin/providers");
    await snapshot(page, `admin-providers-${theme}`);
  });
});

test("admin unmanaged: data", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, {
      me: adminMeFixture(),
      unmanaged: {
        items: [
          {
            provider: "torbox",
            providerItemId: "pi-1",
            kind: "torrent",
            name: "old-torrent",
            sizeBytes: 4_000_000_000,
            state: "completed",
            firstSeenAt: iso(-10 * 86400_000),
            lastSeenAt: iso(-3600_000),
          },
        ],
      },
    });
    await page.goto("/admin/unmanaged");
    await snapshot(page, `admin-unmanaged-${theme}`);
  });
});

test("admin outbox: parked deletions", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, {
      me: adminMeFixture(),
      outbox: {
        messages: [
          {
            id: 1,
            kind: "delete-remote-item",
            remoteItemId: "ri-1",
            attempts: 4,
            lastError: "provider timeout",
            createdAt: iso(-3 * 3600_000),
            parkedAt: iso(-3600_000),
          },
          {
            id: 2,
            kind: "delete-remote-item",
            remoteItemId: "ri-2",
            attempts: 2,
            lastError: "account locked",
            createdAt: iso(-2 * 3600_000),
            parkedAt: iso(-1800_000),
          },
        ],
      },
    });
    await page.goto("/admin/outbox");
    await snapshot(page, `admin-outbox-${theme}`);
  });
});

test("admin unmanaged: empty", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { me: adminMeFixture() });
    await page.goto("/admin/unmanaged");
    await page.getByText("Nothing unmanaged.", { exact: false }).waitFor();
    await snapshot(page, `admin-unmanaged-empty-${theme}`);
  });
});

test("admin outbox: empty", async ({ browser }) => {
  await themed(browser, async (page, theme) => {
    await mockApi(page, { me: adminMeFixture() });
    await page.goto("/admin/outbox");
    await page.getByText("No stuck deletions.").waitFor();
    await snapshot(page, `admin-outbox-empty-${theme}`);
  });
});
