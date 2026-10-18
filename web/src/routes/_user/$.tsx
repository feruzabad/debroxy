import { createFileRoute } from "@tanstack/react-router";
import { NotFoundPage } from "@/components/app/error-pages";

export const Route = createFileRoute("/_user/$")({
  head: () => ({ meta: [{ title: "Not found · debroxy" }] }),
  component: NotFoundPage,
});
