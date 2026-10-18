import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRouter, RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { toast } from "sonner";
import { resetSessionEnded, setSessionEndedHandler, wasJustLoggedOut } from "@/api/client";
import { isApiError } from "@/api/errors";
import { closeAllStreams } from "@/api/live";
import { meQueryOptions } from "@/api/queries/me";
import { Toaster } from "@/components/ui/sonner";
import { applyStoredTheme, startThemeSync } from "@/lib/theme";
import { routeTree } from "./routeTree.gen";
import "sonner/dist/styles.css";
import "@/styles/app.css";

applyStoredTheme();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      gcTime: 5 * 60_000,
      refetchOnWindowFocus: true, // off for queries kept live by the stream
      retry: (count, error) =>
        isApiError(error) &&
        (error.status === 0 || error.status >= 500) &&
        error.status !== 501 &&
        count < 2,
      retryDelay: (n, error) =>
        isApiError(error) && error.retryAfter ? error.retryAfter * 1000 : 1000 * 2 ** n,
    },
    mutations: { retry: false },
  },
});

const router = createRouter({ routeTree, context: { queryClient } });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

setSessionEndedHandler(() => {
  const { pathname, search } = window.location;
  if (pathname.startsWith("/login")) {
    return;
  }
  if (queryClient.getQueryData(meQueryOptions.queryKey) === undefined) {
    // Not logged in yet: the bootstrap 401 on getMe only redirects (§3.6); the
    // route guard does that. Re-arm the flow for a session that ends later.
    resetSessionEnded();
    return;
  }
  // §7.4: close streams, drop the cache, redirect, and toast unless the user
  // just logged out.
  closeAllStreams();
  void queryClient.cancelQueries();
  queryClient.clear();
  void router.navigate({ to: "/login", search: { redirect: pathname + search }, replace: true });
  if (!wasJustLoggedOut()) {
    toast.error("Your session ended. Please log in again.", { duration: 10_000 });
  }
});

startThemeSync();

const rootElement = document.getElementById("root");
if (rootElement) {
  createRoot(rootElement).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
        <Toaster />
      </QueryClientProvider>
    </StrictMode>,
  );
}
