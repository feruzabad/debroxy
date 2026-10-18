import { createFileRoute, redirect } from "@tanstack/react-router";
import { isApiError } from "@/api/errors";
import { type Me, meQueryOptions } from "@/api/queries/me";
import { AppShell } from "@/components/app/shell";

export const Route = createFileRoute("/_user")({
  beforeLoad: async ({ context, location }) => {
    let me: Me;
    try {
      me = await context.queryClient.ensureQueryData(meQueryOptions);
    } catch (error) {
      if (isApiError(error) && error.status === 401) {
        throw redirect({ to: "/login", search: { redirect: location.href }, replace: true });
      }
      throw error;
    }
    return { me };
  },
  component: () => {
    const { me } = Route.useRouteContext();
    return <AppShell me={me} />;
  },
});
