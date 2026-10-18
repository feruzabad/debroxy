import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { z } from "zod";
import { jobQueryOptions } from "@/api/queries/jobs";
import { JobDetail, type JobTab } from "@/features/job-detail/job-detail";
import { Player } from "@/features/player/player";
import type { FileEntry } from "@/lib/filetree";
import { defaultJobsSearch } from "@/lib/search";

const JobDetailSearch = z.object({
  tab: z.enum(["files", "details"]).default("files").catch("files"),
});

export const Route = createFileRoute("/_user/jobs/$jobId")({
  validateSearch: (search: Record<string, unknown>) => JobDetailSearch.parse(search),
  loader: async ({ context, params }) => {
    try {
      const job = await context.queryClient.ensureQueryData(jobQueryOptions(params.jobId));
      return { job };
    } catch {
      // 404 and other errors are rendered by the component's useQuery state.
      return { job: null };
    }
  },
  head: ({ loaderData }) => ({
    meta: [{ title: `${loaderData?.job?.displayName ?? "Job"} · debroxy` }],
  }),
  component: JobDetailRoute,
});

function JobDetailRoute() {
  const { jobId } = Route.useParams();
  const search = Route.useSearch();
  const navigate = Route.useNavigate();
  const [playing, setPlaying] = useState<FileEntry | null>(null);

  return (
    <>
      <JobDetail
        jobId={jobId}
        tab={search.tab}
        onTabChange={(tab: JobTab) => void navigate({ search: { tab }, replace: true })}
        onPlay={setPlaying}
        onReleased={() => void navigate({ to: "/", search: defaultJobsSearch })}
      />
      {playing !== null ? (
        <Player
          jobId={jobId}
          file={playing}
          open
          onOpenChange={(open) => {
            if (!open) {
              setPlaying(null);
            }
          }}
        />
      ) : null}
    </>
  );
}
