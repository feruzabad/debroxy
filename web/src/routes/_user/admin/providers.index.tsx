import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { providersQueryOptions } from "@/api/queries/jobs";
import { ProvidersPage } from "@/features/admin-providers/providers-page";

const AdminProvidersSearch = z.object({
  provider: z.string().optional().catch(undefined),
});

export const Route = createFileRoute("/_user/admin/providers/")({
  validateSearch: (search: Record<string, unknown>) => AdminProvidersSearch.parse(search),
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(providersQueryOptions);
  },
  head: () => ({ meta: [{ title: "Providers · Admin · debroxy" }] }),
  component: ProvidersRoute,
});

function ProvidersRoute() {
  const navigate = Route.useNavigate();
  const search = Route.useSearch();
  return (
    <ProvidersPage
      search={search}
      onSearchChange={(next) => void navigate({ search: next, replace: true })}
    />
  );
}
