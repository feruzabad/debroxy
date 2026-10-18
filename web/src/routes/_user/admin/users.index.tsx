import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { adminUsersQueryOptions } from "@/api/queries/admin";
import { UsersPage } from "@/features/admin-users/users-page";

const AdminUsersSearch = z.object({
  q: z.string().optional().catch(undefined),
});

export const Route = createFileRoute("/_user/admin/users/")({
  validateSearch: (search: Record<string, unknown>) => AdminUsersSearch.parse(search),
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(adminUsersQueryOptions);
  },
  head: () => ({ meta: [{ title: "Users · Admin · debroxy" }] }),
  component: UsersRoute,
});

function UsersRoute() {
  const navigate = Route.useNavigate();
  const search = Route.useSearch();
  return (
    <UsersPage
      search={search}
      onSearchChange={(next) => void navigate({ search: next, replace: true })}
    />
  );
}
