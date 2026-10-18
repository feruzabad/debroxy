import { createFileRoute } from "@tanstack/react-router";
import { configQueryOptions } from "@/api/queries/config";
import { NewUserDialog } from "@/features/admin-users/new-user-dialog";
import { UsersPage } from "@/features/admin-users/users-page";

export const Route = createFileRoute("/_user/admin/users/new")({
  loader: ({ context }) => context.queryClient.ensureQueryData(configQueryOptions),
  head: () => ({ meta: [{ title: "New user · Admin · debroxy" }] }),
  component: NewUserRoute,
});

function NewUserRoute() {
  return (
    <>
      <UsersPage search={{}} onSearchChange={() => {}} />
      <NewUserDialog />
    </>
  );
}
