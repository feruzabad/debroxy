import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import type { HistoryEntry } from "../mocks/fixtures";
import { historyEntryFixture, meFixture, providersFixture } from "../mocks/fixtures";
import { server } from "../mocks/server";
import { renderAppAt } from "./render";

function historyHandlers(entries: HistoryEntry[]) {
  server.use(
    http.get("/api/v1/me", () => HttpResponse.json(meFixture())),
    http.get("/api/v1/jobs", () => HttpResponse.json({ jobs: [], stale: false })),
    http.get("/api/v1/jobs/history", () => HttpResponse.json({ entries })),
    http.get("/api/v1/providers", () => HttpResponse.json(providersFixture())),
  );
}

describe("history", () => {
  it("renders history entries", async () => {
    historyHandlers([
      historyEntryFixture({ displayName: "Sintel" }),
      historyEntryFixture({
        jobId: "job-2",
        displayName: "debian-13",
        finalState: "failed",
        failureReason: "provider-rejected",
      }),
    ]);
    renderAppAt("/history");
    expect(await screen.findByRole("heading", { level: 1, name: "History" })).toBeInTheDocument();
    expect(await screen.findByText("Sintel")).toBeInTheDocument();
    expect(screen.getByText("debian-13")).toBeInTheDocument();
  });

  it("filters by name from the URL search", async () => {
    const user = userEvent.setup();
    historyHandlers([
      historyEntryFixture({ displayName: "Sintel" }),
      historyEntryFixture({ jobId: "job-2", displayName: "debian-13" }),
    ]);
    renderAppAt("/history");
    await screen.findByText("Sintel");
    await user.type(screen.getByRole("textbox", { name: "Filter history by name" }), "debian");
    expect(await screen.findByText("debian-13")).toBeInTheDocument();
    expect(screen.queryByText("Sintel")).not.toBeInTheDocument();
  });

  it("shows the empty state", async () => {
    historyHandlers([]);
    renderAppAt("/history");
    expect(await screen.findByText("Nothing released yet.")).toBeInTheDocument();
  });
});
