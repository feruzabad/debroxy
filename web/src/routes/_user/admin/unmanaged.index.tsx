import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { unmanagedItemsQueryOptions } from "@/api/queries/admin";
import { UnmanagedPage } from "@/features/admin-unmanaged/unmanaged-page";

const AdminUnmanagedSearch = z.object({
  provider: z.string().optional().catch(undefined),
});

export const Route = createFileRoute("/_user/admin/unmanaged/")({
  validateSearch: (search: Record<string, unknown>) => AdminUnmanagedSearch.parse(search),
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(unmanagedItemsQueryOptions);
  },
  head: () => ({ meta: [{ title: "Unmanaged items · Admin · debroxy" }] }),
  component: UnmanagedRoute,
});

function UnmanagedRoute() {
  const navigate = Route.useNavigate();
  const search = Route.useSearch();
  return (
    <UnmanagedPage
      search={search}
      onSearchChange={(next) => void navigate({ search: next, replace: true })}
    />
  );
}
