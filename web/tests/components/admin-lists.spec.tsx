import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { adminMeFixture, providersFixture } from "../mocks/fixtures";
import { server } from "../mocks/server";
import { renderAppAt } from "./render";

function shellHandlers() {
  server.use(
    http.get("/api/v1/me", () => HttpResponse.json(adminMeFixture())),
    http.get("/api/v1/jobs", () => HttpResponse.json({ jobs: [], stale: false })),
    http.get("/api/v1/providers", () => HttpResponse.json(providersFixture())),
    http.get("/api/v1/admin/outbox", () => HttpResponse.json({ messages: [] })),
  );
}

const unmanagedItem = {
  provider: "torbox",
  providerItemId: "pi-1",
  kind: "torrent",
  name: "<b>old-torrent</b>",
  sizeBytes: 2048,
  state: "completed",
  firstSeenAt: "2026-10-01T00:00:00Z",
  lastSeenAt: "2026-10-13T00:00:00Z",
};

const outboxMessage = {
  id: 7,
  kind: "delete-remote-item",
  remoteItemId: "ri-1",
  attempts: 4,
  lastError: "provider timeout",
  createdAt: "2026-10-12T00:00:00Z",
  parkedAt: "2026-10-13T00:00:00Z",
};

describe("admin unmanaged items", () => {
  it("lists items with the name as plain text", async () => {
    shellHandlers();
    server.use(
      http.get("/api/v1/admin/unmanaged-items", () =>
        HttpResponse.json({ items: [unmanagedItem] }),
      ),
    );
    renderAppAt("/admin/unmanaged");
    expect(await screen.findByText("<b>old-torrent</b>")).toBeInTheDocument();
    expect(screen.getByText("TorBox")).toBeInTheDocument();
    expect(screen.getByText("2 KiB")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy provider item ID" })).toBeInTheDocument();
  });

  it("filters by provider from the URL", async () => {
    shellHandlers();
    server.use(
      http.get("/api/v1/admin/unmanaged-items", () =>
        HttpResponse.json({
          items: [
            unmanagedItem,
            { ...unmanagedItem, provider: "realdebrid", providerItemId: "pi-2", name: "rd-item" },
          ],
        }),
      ),
    );
    renderAppAt("/admin/unmanaged?provider=realdebrid");
    expect(await screen.findByText("rd-item")).toBeInTheDocument();
    expect(screen.queryByText("<b>old-torrent</b>")).not.toBeInTheDocument();
  });

  it("shows the empty state", async () => {
    shellHandlers();
    server.use(http.get("/api/v1/admin/unmanaged-items", () => HttpResponse.json({ items: [] })));
    renderAppAt("/admin/unmanaged");
    expect(
      await screen.findByText(
        "Nothing unmanaged. The provider accounts contain only what debroxy added.",
      ),
    ).toBeInTheDocument();
  });

  it("shows an error with retry", async () => {
    const user = userEvent.setup();
    let calls = 0;
    shellHandlers();
    server.use(
      http.get("/api/v1/admin/unmanaged-items", () => {
        calls++;
        return calls === 1
          ? HttpResponse.json({ slug: "internal-error", message: "" }, { status: 500 })
          : HttpResponse.json({ items: [unmanagedItem] });
      }),
    );
    renderAppAt("/admin/unmanaged");
    await user.click(await screen.findByRole("button", { name: "Try again" }));
    expect(await screen.findByText("<b>old-torrent</b>")).toBeInTheDocument();
  });
});

describe("admin stuck deletions", () => {
  it("lists parked messages", async () => {
    shellHandlers();
    server.use(
      http.get("/api/v1/admin/outbox", () => HttpResponse.json({ messages: [outboxMessage] })),
    );
    renderAppAt("/admin/outbox");
    expect(await screen.findByText("provider timeout")).toBeInTheDocument();
    expect(screen.getByText("ri-1")).toBeInTheDocument();
    expect(screen.getByText("delete-remote-item")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy remote item ID" })).toBeInTheDocument();
  });

  it("shows the empty state", async () => {
    shellHandlers();
    renderAppAt("/admin/outbox");
    expect(await screen.findByText("No stuck deletions.")).toBeInTheDocument();
  });

  it("reloads on demand", async () => {
    const user = userEvent.setup();
    let calls = 0;
    shellHandlers();
    server.use(
      http.get("/api/v1/admin/outbox", () => {
        calls++;
        return HttpResponse.json({ messages: calls > 1 ? [outboxMessage] : [] });
      }),
    );
    renderAppAt("/admin/outbox");
    await screen.findByText("No stuck deletions.");
    await user.click(screen.getByRole("button", { name: "Reload" }));
    expect(await screen.findByText("provider timeout")).toBeInTheDocument();
  });
});
