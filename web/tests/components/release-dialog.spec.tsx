import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { ReleaseDialog } from "@/components/app/release-dialog";
import { Toaster } from "@/components/ui/sonner";
import { type Job, jobFixture } from "../mocks/fixtures";
import { server } from "../mocks/server";

function renderDialog(job: Job) {
  const queryClient = new QueryClient();
  return render(
    <QueryClientProvider client={queryClient}>
      <ReleaseDialog job={job} providerName="TorBox" open onOpenChange={() => {}} />
      <Toaster />
    </QueryClientProvider>,
  );
}

describe("release dialog", () => {
  it("explains shared content", () => {
    renderDialog(jobFixture({ shared: true, slotCharged: false, state: "ready" }));
    expect(
      screen.getByText(
        "Others also use this content. It stays at the provider for them; it just leaves your list.",
      ),
    ).toBeInTheDocument();
  });

  it("explains non-shared content with the provider name", () => {
    renderDialog(jobFixture({ shared: false, slotCharged: false, state: "ready" }));
    expect(
      screen.getByText(
        "Nobody else uses this content, so debroxy will delete it at TorBox. Download links stop working.",
      ),
    ).toBeInTheDocument();
  });

  it("warns when the job is still active", () => {
    renderDialog(jobFixture({ shared: false, state: "downloading" }));
    expect(screen.getByText("It isn't finished yet.")).toBeInTheDocument();
  });

  it("notes that a charged slot is freed", () => {
    renderDialog(jobFixture({ shared: false, slotCharged: true, state: "ready" }));
    expect(screen.getByText("This frees one of your slots.")).toBeInTheDocument();
  });

  it("shows the server error title when release fails", async () => {
    const user = userEvent.setup();
    server.use(
      http.delete("/api/v1/jobs/:jobId", () =>
        HttpResponse.json({ slug: "internal-error", message: "boom" }, { status: 500 }),
      ),
    );
    renderDialog(jobFixture({ shared: false, state: "ready" }));
    await user.click(screen.getByRole("button", { name: "Release" }));
    expect(await screen.findByText("Something went wrong.")).toBeInTheDocument();
  });
});
