import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { allJobsQueryOptions, providersQueryOptions } from "@/api/queries/jobs";
import { AdminJobsPage } from "@/features/admin-jobs/admin-jobs-page";
import { JobsSearch } from "@/lib/search";

const AdminJobsSearch = JobsSearch.extend({
  owner: z.string().optional().catch(undefined),
});

export const Route = createFileRoute("/_user/admin/jobs/")({
  validateSearch: (search: Record<string, unknown>) => AdminJobsSearch.parse(search),
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(allJobsQueryOptions);
    void context.queryClient.prefetchQuery(providersQueryOptions);
  },
  head: () => ({ meta: [{ title: "All jobs · Admin · debroxy" }] }),
  component: AdminJobsRoute,
});

function AdminJobsRoute() {
  const navigate = Route.useNavigate();
  const search = Route.useSearch();
  return (
    <AdminJobsPage
      search={search}
      onSearchChange={(next) => void navigate({ search: next, replace: true })}
    />
  );
}
