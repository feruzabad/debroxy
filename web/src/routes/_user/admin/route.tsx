import { createFileRoute, Outlet } from "@tanstack/react-router";
import { ForbiddenError, ForbiddenPage, RouteErrorPanel } from "@/components/app/error-pages";

export const Route = createFileRoute("/_user/admin")({
  beforeLoad: ({ context }) => {
    if (context.me.user.role !== "admin") {
      throw new ForbiddenError();
    }
  },
  errorComponent: ({ error }) => {
    if (error instanceof ForbiddenError) {
      return <ForbiddenPage />;
    }
    return <RouteErrorPanel error={error} />;
  },
  component: () => <Outlet />,
});
