import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { jobFixture, meFixture, providersFixture } from "../mocks/fixtures";
import { server } from "../mocks/server";
import { renderAppAt } from "./render";

function jobsHandlers(jobs: unknown[], stale = false) {
  server.use(
    http.get("/api/v1/me", () => HttpResponse.json(meFixture())),
    http.get("/api/v1/jobs", () => HttpResponse.json({ jobs, stale })),
    http.get("/api/v1/providers", () => HttpResponse.json(providersFixture())),
  );
}

describe("jobs page", () => {
  it("renders job rows", async () => {
    jobsHandlers([
      jobFixture({ id: "a", displayName: "Sintel", state: "ready" }),
      jobFixture({ id: "b", displayName: "debian-13", state: "downloading" }),
    ]);
    renderAppAt("/");
    expect(await screen.findByRole("heading", { level: 1, name: "Jobs" })).toBeInTheDocument();
    expect(await screen.findByText("Sintel")).toBeInTheDocument();
    expect(screen.getByText("debian-13")).toBeInTheDocument();
  });

  it("shows the empty state when there are no jobs", async () => {
    jobsHandlers([]);
    renderAppAt("/");
    expect(await screen.findByText("No jobs yet")).toBeInTheDocument();
  });

  it("shows the filtered-empty state", async () => {
    jobsHandlers([jobFixture({ id: "a", displayName: "Sintel", state: "ready" })]);
    renderAppAt("/?group=active");
    expect(await screen.findByText("No jobs match these filters.")).toBeInTheDocument();
  });

  it("filters by group chip", async () => {
    const user = userEvent.setup();
    jobsHandlers([
      jobFixture({ id: "a", displayName: "Sintel", state: "ready" }),
      jobFixture({ id: "b", displayName: "debian-13", state: "downloading" }),
    ]);
    renderAppAt("/");
    await screen.findByText("Sintel");
    await user.click(screen.getByRole("button", { name: /active/i }));
    expect(await screen.findByText("debian-13")).toBeInTheDocument();
    expect(screen.queryByText("Sintel")).not.toBeInTheDocument();
  });

  it("shows the stale banner", async () => {
    jobsHandlers([jobFixture({ id: "a", displayName: "Sintel" })], true);
    renderAppAt("/");
    expect(await screen.findByText(/Not up to date: debroxy couldn't reach/)).toBeInTheDocument();
  });

  it("opens the job when Enter is pressed on a focused row", async () => {
    const user = userEvent.setup();
    jobsHandlers([jobFixture({ id: "job-a", displayName: "Sintel", state: "ready" })]);
    const { router } = renderAppAt("/");
    await screen.findByText("Sintel");
    const rows = screen.getAllByRole("row");
    const jobRow = rows[1];
    if (jobRow === undefined) {
      throw new Error("expected a job row");
    }
    jobRow.focus();
    await user.keyboard("{Enter}");
    await waitFor(() => expect(router.state.location.pathname).toBe("/jobs/job-a"));
  });
});
