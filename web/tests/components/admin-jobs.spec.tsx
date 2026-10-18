import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { Toaster } from "@/components/ui/sonner";
import { AdminReleaseDialog } from "@/features/admin-jobs/admin-release-dialog";
import { adminMeFixture, type Job, jobFixture, providersFixture } from "../mocks/fixtures";
import { server } from "../mocks/server";
import { renderAppAt } from "./render";

function shellHandlers() {
  server.use(
    http.get("/api/v1/me", () => HttpResponse.json(adminMeFixture())),
    http.get("/api/v1/jobs", () => HttpResponse.json({ jobs: [], stale: false })),
    http.get("/api/v1/providers", () => HttpResponse.json(providersFixture())),
  );
}

function adminJobsHandlers(jobs: unknown[]) {
  shellHandlers();
  server.use(http.get("/api/v1/admin/jobs", () => HttpResponse.json({ jobs, stale: false })));
}

function renderDialog(job: Job, ownUsername?: string) {
  const queryClient = new QueryClient();
  return render(
    <QueryClientProvider client={queryClient}>
      <AdminReleaseDialog job={job} ownUsername={ownUsername} open onOpenChange={() => {}} />
      <Toaster />
    </QueryClientProvider>,
  );
}

describe("admin jobs", () => {
  it("links the admin's own job to the job detail page", async () => {
    adminJobsHandlers([jobFixture({ id: "job-a", displayName: "Sintel", ownerUsername: "alice" })]);
    renderAppAt("/admin/jobs");
    expect(await screen.findByRole("heading", { level: 1, name: "All jobs" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Sintel" })).toHaveAttribute(
      "href",
      expect.stringContaining("/jobs/job-a"),
    );
  });

  it("expands an inline read-only detail for another user's job", async () => {
    adminJobsHandlers([
      jobFixture({
        id: "job-b",
        displayName: "BobJob",
        ownerUsername: "bob",
        state: "downloading",
        files: [{ id: "f1", path: "movie.mkv", sizeBytes: 2048 }],
      }),
    ]);
    renderAppAt("/admin/jobs");
    await screen.findByText("BobJob");
    expect(screen.queryByRole("link", { name: "BobJob" })).not.toBeInTheDocument();
    const row = screen.getByText("BobJob").closest("tr");
    if (row === null) {
      throw new Error("expected a job row");
    }
    fireEvent.click(row);
    expect(await screen.findByText("movie.mkv")).toBeInTheDocument();
  });

  it("shows file names, not full paths, inside folders of the drawer", async () => {
    adminJobsHandlers([
      jobFixture({
        id: "job-b",
        displayName: "BobJob",
        ownerUsername: "bob",
        files: [
          { id: "f1", path: "Season 1/episode-01.mkv", sizeBytes: 10 },
          { id: "f2", path: "Season 1/episode-02.mkv", sizeBytes: 10 },
        ],
      }),
    ]);
    renderAppAt("/admin/jobs");
    const row = (await screen.findByText("BobJob")).closest("tr");
    if (row === null) {
      throw new Error("expected a job row");
    }
    fireEvent.click(row);
    expect(await screen.findByText("episode-01.mkv")).toBeInTheDocument();
    expect(screen.queryByText("Season 1/episode-01.mkv")).not.toBeInTheDocument();
  });

  it("does not offer Open for another user's job", async () => {
    const user = userEvent.setup();
    adminJobsHandlers([
      jobFixture({ id: "job-a", displayName: "Sintel", ownerUsername: "alice" }),
      jobFixture({ id: "job-b", displayName: "BobJob", ownerUsername: "bob" }),
    ]);
    renderAppAt("/admin/jobs");
    await screen.findByText("BobJob");
    await user.click(screen.getByRole("button", { name: "Actions for BobJob" }));
    expect(await screen.findByText("Release…")).toBeInTheDocument();
    expect(screen.queryByText("Open")).not.toBeInTheDocument();
  });
});

describe("admin release dialog", () => {
  it("notes the owner when releasing another user's job", () => {
    renderDialog(jobFixture({ id: "job-b", displayName: "BobJob", ownerUsername: "bob" }), "alice");
    expect(screen.getByText("This is bob's job.")).toBeInTheDocument();
  });

  it("does not note the owner when releasing the admin's own job", () => {
    renderDialog(
      jobFixture({ id: "job-a", displayName: "Sintel", ownerUsername: "alice" }),
      "alice",
    );
    expect(screen.queryByText(/This is .*'s job\./)).not.toBeInTheDocument();
  });
});
