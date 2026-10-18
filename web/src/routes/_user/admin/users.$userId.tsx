import { createFileRoute } from "@tanstack/react-router";
import { isApiError } from "@/api/errors";
import { adminUserQueryOptions, slotQuotaQueryOptions } from "@/api/queries/admin";
import { UserDetailPage } from "@/features/admin-users/user-detail-page";

export const Route = createFileRoute("/_user/admin/users/$userId")({
  loader: async ({ context, params }) => {
    try {
      const user = await context.queryClient.ensureQueryData(adminUserQueryOptions(params.userId));
      void context.queryClient.prefetchQuery(slotQuotaQueryOptions(params.userId));
      return { user };
    } catch (error) {
      if (isApiError(error) && error.slug === "user-not-found") {
        return { user: null };
      }
      throw error;
    }
  },
  head: ({ loaderData }) => ({
    meta: [{ title: `${loaderData?.user?.username ?? "User"} · Admin · debroxy` }],
  }),
  component: UserDetailRoute,
});

function UserDetailRoute() {
  const { userId } = Route.useParams();
  return <UserDetailPage userId={userId} />;
}
