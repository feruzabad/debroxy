import { createFileRoute } from "@tanstack/react-router";
import { jobsQueryOptions, providersQueryOptions } from "@/api/queries/jobs";
import { JobsPage } from "@/features/jobs/jobs-page";
import { JobsSearch } from "@/lib/search";

export const Route = createFileRoute("/_user/")({
  validateSearch: (search: Record<string, unknown>) => JobsSearch.parse(search),
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(jobsQueryOptions);
    void context.queryClient.prefetchQuery(providersQueryOptions);
  },
  head: () => ({ meta: [{ title: "Jobs · debroxy" }] }),
  component: JobsRoute,
});

function JobsRoute() {
  const navigate = Route.useNavigate();
  const search = Route.useSearch();
  return (
    <JobsPage
      search={search}
      onSearchChange={(next) => void navigate({ search: next, replace: true })}
    />
  );
}
