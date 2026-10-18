import { createFileRoute } from "@tanstack/react-router";
import { outboxMessagesQueryOptions } from "@/api/queries/admin";
import { OutboxPage } from "@/features/admin-outbox/outbox-page";

export const Route = createFileRoute("/_user/admin/outbox/")({
  loader: ({ context }) => {
    void context.queryClient.prefetchQuery(outboxMessagesQueryOptions);
  },
  head: () => ({ meta: [{ title: "Stuck deletions · Admin · debroxy" }] }),
  component: OutboxPage,
});
