import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { act } from "react";
import { beforeAll, describe, expect, it, vi } from "vitest";
import type { Job } from "../mocks/fixtures";
import {
  historyEntryFixture,
  jobFixture,
  meFixture,
  providersFixture,
  signedLinkFixture,
} from "../mocks/fixtures";
import { server } from "../mocks/server";
import { renderAppAt } from "./render";

beforeAll(() => {
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
    configurable: true,
  });
});

interface MockEventSourceInstance {
  listeners: Map<string, Array<(event: { data: string }) => void>>;
}

/** Emits one SSE event through the test EventSource mock (see tests/setup.ts). */
function emitStreamEvent(name: string, data: unknown): void {
  const EventSourceClass = globalThis.EventSource as unknown as {
    instances: MockEventSourceInstance[];
  };
  const instance = EventSourceClass.instances[0];
  if (instance === undefined) {
    throw new Error("no EventSource instance");
  }
  for (const listener of instance.listeners.get(name) ?? []) {
    listener({ data: JSON.stringify(data) });
  }
}

function readyJob(overrides: Partial<Job> = {}): Job {
  return jobFixture({
    state: "ready",
    displayName: "My Movie",
    delivery: { perFile: true, bundle: { format: "zip", rangeSupported: false } },
    files: [
      { id: "f1", path: "Sintel/Sintel.mp4", sizeBytes: 1000 },
      { id: "f2", path: "Sintel/Sintel.srt", sizeBytes: 2000 },
      { id: "f3", path: "Sintel/Extras/poster.jpg", sizeBytes: 3000 },
    ],
    ...overrides,
  });
}

function jobDetailHandlers(job: Job) {
  server.use(
    http.get("/api/v1/me", () => HttpResponse.json(meFixture())),
    http.get("/api/v1/jobs", () => HttpResponse.json({ jobs: [], stale: false })),
    http.get("/api/v1/jobs/:jobId", () => HttpResponse.json(job)),
    http.get("/api/v1/providers", () => HttpResponse.json(providersFixture())),
    http.post("/api/v1/jobs/:jobId/links", () => HttpResponse.json(signedLinkFixture())),
    http.post("/api/v1/jobs/:jobId/links/all", () =>
      HttpResponse.json({ links: [signedLinkFixture()] }),
    ),
  );
}

describe("job detail", () => {
  it("renders the folder tree", async () => {
    jobDetailHandlers(readyJob());
    renderAppAt("/jobs/job-1");
    expect(await screen.findByRole("heading", { level: 1, name: "My Movie" })).toBeInTheDocument();
    expect(await screen.findByText("Extras")).toBeInTheDocument();
    expect(screen.getByText("Sintel.mp4")).toBeInTheDocument();
    expect(screen.getByText("poster.jpg")).toBeInTheDocument();
  });

  it("copies a file link with the expiry toast", async () => {
    const user = userEvent.setup();
    jobDetailHandlers(readyJob());
    renderAppAt("/jobs/job-1");
    await screen.findByRole("heading", { level: 1, name: "My Movie" });
    await user.click(screen.getByRole("button", { name: "Copy link for Sintel.mp4" }));
    expect(
      await screen.findByText(/Link copied\. It works for anyone who has it until/),
    ).toBeInTheDocument();
  });

  it("releases the job and navigates to the jobs page", async () => {
    const user = userEvent.setup();
    jobDetailHandlers(readyJob());
    server.use(http.delete("/api/v1/jobs/:jobId", () => new HttpResponse(null, { status: 204 })));
    renderAppAt("/jobs/job-1");
    await screen.findByRole("heading", { level: 1, name: "My Movie" });
    await user.click(screen.getByRole("button", { name: "Release" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Release" }));
    expect(await screen.findByRole("heading", { level: 1, name: "Jobs" })).toBeInTheDocument();
  });

  it("shows the released state when a release event arrives", async () => {
    jobDetailHandlers(readyJob());
    renderAppAt("/jobs/job-1");
    await screen.findByRole("heading", { level: 1, name: "My Movie" });

    act(() => {
      emitStreamEvent("job.released", {
        jobId: "job-1",
        historyEntry: historyEntryFixture({ displayName: "My Movie" }),
      });
    });

    expect(await screen.findByText("This job was released.")).toBeInTheDocument();
    expect(screen.getByText("See history")).toBeInTheDocument();
  });

  it("announces state changes through a polite live region", async () => {
    jobDetailHandlers(jobFixture({ id: "job-2", displayName: "My Movie", state: "downloading" }));
    renderAppAt("/jobs/job-2");
    await screen.findByRole("heading", { level: 1, name: "My Movie" });

    act(() => {
      emitStreamEvent(
        "job.upserted",
        jobFixture({ id: "job-2", displayName: "My Movie", state: "ready" }),
      );
    });

    expect(await screen.findByText("My Movie: Ready")).toBeInTheDocument();
  });
});
