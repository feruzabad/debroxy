import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { jobFixture, meFixture, providersFixture } from "../mocks/fixtures";
import { server } from "../mocks/server";
import { renderAppAt } from "./render";

const MAGNET = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567";

function addHandlers(overrides?: { submit?: () => Response }) {
  server.use(
    http.get("/api/v1/me", () => HttpResponse.json(meFixture())),
    http.get("/api/v1/jobs", () => HttpResponse.json({ jobs: [], stale: false })),
    http.get("/api/v1/providers", () => HttpResponse.json(providersFixture())),
    http.post("/api/v1/providers/:providerId/cached-availability", () =>
      HttpResponse.json({ cached: {} }),
    ),
    http.post(
      "/api/v1/jobs",
      (overrides?.submit ??
        (() =>
          HttpResponse.json(jobFixture({ id: "new-job", displayName: "Sintel" }), {
            status: 201,
          }))) as never,
    ),
  );
}

describe("add job dialog", () => {
  it("rejects a malformed magnet link", async () => {
    const user = userEvent.setup();
    addHandlers();
    renderAppAt("/add");
    await user.type(await screen.findByLabelText("Magnet link"), "not-a-magnet");
    await user.click(screen.getByRole("button", { name: "Add job" }));
    expect(await screen.findByText("This doesn't look like a magnet link.")).toBeInTheDocument();
  });

  it("shows a toast on a created job", async () => {
    const user = userEvent.setup();
    addHandlers();
    renderAppAt("/add");
    await user.type(await screen.findByLabelText("Magnet link"), MAGNET);
    await user.click(screen.getByRole("button", { name: "Add job" }));
    expect(await screen.findByText("Added Sintel.")).toBeInTheDocument();
  });

  it("links to the existing job on job-already-exists", async () => {
    const user = userEvent.setup();
    addHandlers({
      submit: () =>
        HttpResponse.json(
          { slug: "job-already-exists", message: "" },
          { status: 409, headers: { Location: "/api/v1/jobs/existing-job" } },
        ),
    });
    renderAppAt("/add");
    await user.type(await screen.findByLabelText("Magnet link"), MAGNET);
    await user.click(screen.getByRole("button", { name: "Add job" }));
    expect(await screen.findByText("You already have this.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open it" })).toHaveAttribute(
      "href",
      expect.stringContaining("/jobs/existing-job"),
    );
  });

  it("shows the slot-quota alert", async () => {
    const user = userEvent.setup();
    addHandlers({
      submit: () =>
        HttpResponse.json({ slug: "slot-quota-exceeded", message: "" }, { status: 429 }),
    });
    renderAppAt("/add");
    await user.type(await screen.findByLabelText("Magnet link"), MAGNET);
    await user.click(screen.getByRole("button", { name: "Add job" }));
    expect(await screen.findByText("No free slots.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Show my active jobs" })).toHaveAttribute(
      "href",
      expect.stringContaining("group=active"),
    );
  });

  it("offers a safe retry on a network error", async () => {
    const user = userEvent.setup();
    addHandlers({ submit: () => HttpResponse.error() });
    renderAppAt("/add");
    await user.type(await screen.findByLabelText("Magnet link"), MAGNET);
    await user.click(screen.getByRole("button", { name: "Add job" }));
    expect(
      await screen.findByText("Couldn't reach debroxy. Your job may still have been added."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });

  it("shows the provider-unavailable countdown", async () => {
    const user = userEvent.setup();
    addHandlers({
      submit: () =>
        HttpResponse.json(
          { slug: "provider-unavailable", message: "", retryAfter: 5 },
          { status: 502 },
        ),
    });
    renderAppAt("/add");
    await user.type(await screen.findByLabelText("Magnet link"), MAGNET);
    await user.click(screen.getByRole("button", { name: "Add job" }));
    expect(await screen.findByText("TorBox isn't responding.")).toBeInTheDocument();
    expect(screen.getByText(/Try again in 5s\./)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeDisabled();
  });

  it("links to the jobs list when job-already-exists has no Location", async () => {
    const user = userEvent.setup();
    addHandlers({
      submit: () => HttpResponse.json({ slug: "job-already-exists", message: "" }, { status: 409 }),
    });
    renderAppAt("/add");
    await user.type(await screen.findByLabelText("Magnet link"), MAGNET);
    await user.click(screen.getByRole("button", { name: "Add job" }));
    expect(await screen.findByText("You already have this.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open it" })).toHaveAttribute(
      "href",
      expect.stringContaining("group=all"),
    );
  });
});
