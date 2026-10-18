import { createFileRoute } from "@tanstack/react-router";
import { meQueryOptions } from "@/api/queries/me";
import { AccountPage } from "@/features/account/account-page";

export const Route = createFileRoute("/_user/account")({
  loader: ({ context }) => context.queryClient.ensureQueryData(meQueryOptions),
  head: () => ({ meta: [{ title: "Account · debroxy" }] }),
  component: AccountPage,
});
