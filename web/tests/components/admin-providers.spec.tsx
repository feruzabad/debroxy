import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { adminMeFixture, hosterFixture, type Provider, providersFixture } from "../mocks/fixtures";
import { server } from "../mocks/server";
import { renderAppAt } from "./render";

function providerHandlers(providers: Provider[] = providersFixture().providers) {
  server.use(
    http.get("/api/v1/me", () => HttpResponse.json(adminMeFixture())),
    http.get("/api/v1/jobs", () => HttpResponse.json({ jobs: [], stale: false })),
    http.get("/api/v1/providers", () => HttpResponse.json({ providers })),
    http.get("/api/v1/providers/:providerId/hosters", () =>
      HttpResponse.json({ hosters: [hosterFixture()] }),
    ),
  );
}

function torbox(): Provider {
  const [provider] = providersFixture().providers;
  if (provider === undefined) {
    throw new Error("fixture has no providers");
  }
  return provider;
}

async function openProviders() {
  renderAppAt("/admin/providers");
  expect(await screen.findByRole("heading", { level: 1, name: "Providers" })).toBeInTheDocument();
}

describe("admin providers", () => {
  it("shows the hoster usage text", async () => {
    providerHandlers();
    await openProviders();
    expect(await screen.findByText("12 / 50 links today")).toBeInTheDocument();
  });

  it("labels capability states and the ones not used in v1", async () => {
    providerHandlers([
      {
        ...torbox(),
        enabled: false,
        capabilities: [
          { name: "SubmitWebURL", supported: true, enabled: true, state: "unverified" },
          { name: "BundleDelivery", supported: true, enabled: true, state: "unavailable" },
          { name: "SubmitNZBFile", supported: true, enabled: false },
        ],
      },
    ]);
    await openProviders();
    expect(screen.getByText("disabled in the configuration")).toBeInTheDocument();
    expect(screen.getByText("not yet tested")).toBeInTheDocument();
    expect(screen.getByText("unavailable")).toBeInTheDocument();
    expect(screen.getByText("✕ (not in v1)")).toBeInTheDocument();
  });

  it("says so when the provider takes no web links", async () => {
    const provider = torbox();
    providerHandlers([
      { ...provider, capabilities: provider.capabilities.filter((c) => c.name !== "SubmitWebURL") },
    ]);
    await openProviders();
    expect(screen.getByText("This provider doesn't take web links.")).toBeInTheDocument();
  });

  it.each([
    [501, "capability-not-supported", "No site list for this provider."],
    [409, "provider-disabled", "Provider disabled."],
    [503, "provider-unavailable", "Can't load the site list right now."],
  ])("shows the hoster error for %i %s", async (status, slug, text) => {
    providerHandlers();
    server.use(
      http.get("/api/v1/providers/:providerId/hosters", () =>
        HttpResponse.json({ slug, message: "" }, { status }),
      ),
    );
    await openProviders();
    expect(await screen.findByText(text)).toBeInTheDocument();
  });

  it("refreshes and reports a stale result", async () => {
    const user = userEvent.setup();
    providerHandlers();
    server.use(
      http.post("/api/v1/providers/torbox/refresh", () => HttpResponse.json({ stale: true })),
    );
    await openProviders();
    await user.click(screen.getByRole("button", { name: "⟳ Refresh now" }));
    expect(
      await screen.findByText("TorBox couldn't be reached. Showing the last known state."),
    ).toBeInTheDocument();
  });

  it("refreshes successfully", async () => {
    const user = userEvent.setup();
    providerHandlers();
    server.use(
      http.post("/api/v1/providers/torbox/refresh", () => HttpResponse.json({ stale: false })),
    );
    await openProviders();
    await user.click(screen.getByRole("button", { name: "⟳ Refresh now" }));
    expect(await screen.findByText("TorBox refreshed.")).toBeInTheDocument();
  });

  it("asks for a capability re-test", async () => {
    const user = userEvent.setup();
    let probed = false;
    providerHandlers();
    server.use(
      http.post("/api/v1/admin/providers/torbox/probe-capabilities", () => {
        probed = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    await openProviders();
    await user.click(screen.getByRole("button", { name: "Re-test unavailable capabilities" }));
    expect(
      await screen.findByText(
        "debroxy will test unavailable capabilities again the next time they're used.",
      ),
    ).toBeInTheDocument();
    expect(probed).toBe(true);
  });
});
