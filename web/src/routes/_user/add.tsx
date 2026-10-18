import { createFileRoute } from "@tanstack/react-router";
import { configQueryOptions } from "@/api/queries/config";
import { providersQueryOptions } from "@/api/queries/jobs";
import { AddJobDialog } from "@/features/add-job/add-job-dialog";
import { AddJobSearch } from "@/features/add-job/schema";
import { JobsPage } from "@/features/jobs/jobs-page";
import { defaultJobsSearch } from "@/lib/search";

export const Route = createFileRoute("/_user/add")({
  validateSearch: (search: Record<string, unknown>) => AddJobSearch.parse(search),
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(configQueryOptions);
    void context.queryClient.prefetchQuery(providersQueryOptions);
  },
  head: () => ({ meta: [{ title: "Add job · debroxy" }] }),
  component: AddJobRoute,
});

function AddJobRoute() {
  const search = Route.useSearch();
  return (
    <>
      <JobsPage search={defaultJobsSearch} onSearchChange={() => {}} />
      <AddJobDialog search={search} />
    </>
  );
}
