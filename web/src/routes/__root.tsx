import type { QueryClient } from "@tanstack/react-query";
import { createRootRouteWithContext, HeadContent, Outlet } from "@tanstack/react-router";
import { Loader2 } from "lucide-react";
import { configQueryOptions } from "@/api/queries/config";
import { BootstrapErrorPage, NotFoundPage } from "@/components/app/error-pages";

export interface RouterContext {
  queryClient: QueryClient;
}

export const Route = createRootRouteWithContext<RouterContext>()({
  beforeLoad: async ({ context }) => {
    await context.queryClient.ensureQueryData(configQueryOptions);
  },
  pendingComponent: () => (
    <div className="bg-background text-muted-foreground flex min-h-screen flex-col items-center justify-center gap-3">
      <Loader2 className="size-5 animate-spin" aria-hidden="true" />
      <p className="text-sm">Loading debroxy…</p>
    </div>
  ),
  errorComponent: ({ error }) => <BootstrapErrorPage error={error} />,
  notFoundComponent: NotFoundPage,
  component: () => (
    <>
      <HeadContent />
      <Outlet />
    </>
  ),
});
