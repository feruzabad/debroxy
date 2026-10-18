import { createFileRoute, redirect } from "@tanstack/react-router";
import { z } from "zod";
import { configQueryOptions } from "@/api/queries/config";
import { meQueryOptions } from "@/api/queries/me";
import { LoginForm } from "@/features/auth/login-form";

const LoginSearch = z.object({
  redirect: z
    .string()
    .regex(/^\/(?!\/)/)
    .optional()
    .catch(undefined),
  error: z.enum(["oidc"]).optional().catch(undefined),
});

export const Route = createFileRoute("/login")({
  validateSearch: (search: Record<string, unknown>) => LoginSearch.parse(search),
  beforeLoad: ({ context, search }) => {
    // §5.1: If already logged in (['me'] resolves in cache), redirect to redirect or /
    const me = context.queryClient.getQueryData(meQueryOptions.queryKey);
    if (me) {
      throw redirect({ to: search.redirect ?? "/", replace: true });
    }
  },
  loader: ({ context }) => context.queryClient.ensureQueryData(configQueryOptions),
  head: () => ({ meta: [{ title: "Log in · debroxy" }] }),
  component: () => <LoginForm />,
});
