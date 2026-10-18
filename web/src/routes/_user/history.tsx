import { createFileRoute } from "@tanstack/react-router";
import { historyQueryOptions } from "@/api/queries/jobs";
import { HistoryPage } from "@/features/history/history-page";
import { HistorySearch } from "@/lib/search";

export const Route = createFileRoute("/_user/history")({
  validateSearch: (search: Record<string, unknown>) => HistorySearch.parse(search),
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(historyQueryOptions);
  },
  head: () => ({ meta: [{ title: "History · debroxy" }] }),
  component: HistoryRoute,
});

function HistoryRoute() {
  const navigate = Route.useNavigate();
  const search = Route.useSearch();
  return (
    <HistoryPage
      search={search}
      onSearchChange={(next) => void navigate({ search: next, replace: true })}
    />
  );
}
