import { useForm } from "@tanstack/react-form";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouter, useSearch } from "@tanstack/react-router";
import { Eye, EyeOff } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { z } from "zod";
import { isApiError } from "@/api/errors";
import { keys } from "@/api/keys";
import { logIn } from "@/api/mutations/auth";
import { configQueryOptions } from "@/api/queries/config";
import { useCountdown } from "@/components/app/use-countdown";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { formatCountdown } from "@/lib/format";

const loginSchema = z.object({
  username: z.string().trim().min(1),
  password: z.string().min(1),
});

const REQUIRED = "Enter a username and a password.";

/** The login form (§5.1). */
export function LoginForm() {
  const { data: config } = useQuery(configQueryOptions);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const router = useRouter();
  const search = useSearch({ from: "/login" });

  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  const [throttleUntil, setThrottleUntil] = useState<number | null>(null);
  const [oidcFailed, setOidcFailed] = useState(search.error === "oidc");
  const passwordRef = useRef<HTMLInputElement>(null);

  const throttleRemaining = useCountdown(throttleUntil);

  // Show the OIDC failure message once, then drop ?error=oidc so a reload
  // doesn't repeat it (§5.1).
  useEffect(() => {
    if (search.error !== "oidc") {
      return;
    }
    setOidcFailed(true);
    const next = { ...search };
    delete next.error;
    void router.navigate({ to: "/login", search: next, replace: true });
  }, [router, search]);

  const form = useForm({
    defaultValues: { username: "", password: "" },
    onSubmit: async ({ value }) => {
      const parsed = loginSchema.safeParse(value);
      if (!parsed.success) {
        setFormError(REQUIRED);
        return;
      }
      setFormError(null);
      setSubmitting(true);
      try {
        await logIn(parsed.data.username, parsed.data.password);
        await queryClient.invalidateQueries({ queryKey: keys.me });
        await navigate({ to: search.redirect ?? "/", replace: true });
      } catch (error) {
        if (isApiError(error) && error.slug === "invalid-credentials") {
          setFormError("Wrong username or password.");
          form.setFieldValue("password", "");
          passwordRef.current?.focus();
        } else if (isApiError(error) && error.slug === "login-throttled") {
          setThrottleUntil(Date.now() + (error.retryAfter ?? 0) * 1000);
        } else if (isApiError(error) && error.slug === "invalid-request") {
          setFormError(REQUIRED);
        } else {
          setFormError("Can't reach debroxy. Check your connection and try again.");
        }
      } finally {
        setSubmitting(false);
      }
    },
  });

  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-6 p-6">
      <h1 className="text-base font-semibold">◆ debroxy</h1>
      <div className="w-full max-w-sm space-y-4">
        {oidcFailed ? (
          <Alert variant="destructive">
            <AlertTitle>Single sign-on didn&apos;t work.</AlertTitle>
            <AlertDescription>Your account may not be linked yet; ask an admin.</AlertDescription>
          </Alert>
        ) : null}
        {formError !== null ? (
          <Alert id="login-error" variant="destructive">
            <AlertTitle>{formError}</AlertTitle>
          </Alert>
        ) : null}
        {throttleRemaining > 0 ? (
          <Alert variant="destructive">
            <AlertTitle>Too many attempts.</AlertTitle>
            <AlertDescription>Try again in {formatCountdown(throttleRemaining)}.</AlertDescription>
          </Alert>
        ) : null}
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            event.stopPropagation();
            void form.handleSubmit();
          }}
        >
          <form.Field name="username">
            {(field) => (
              <div className="grid gap-1.5">
                <Label htmlFor="username">Username</Label>
                <Input
                  id="username"
                  name={field.name}
                  value={field.state.value}
                  onChange={(event) => field.handleChange(event.target.value)}
                  onBlur={field.handleBlur}
                  autoComplete="username"
                  autoFocus
                />
              </div>
            )}
          </form.Field>
          <form.Field name="password">
            {(field) => (
              <div className="grid gap-1.5">
                <Label htmlFor="password">Password</Label>
                <div className="relative">
                  <Input
                    id="password"
                    name={field.name}
                    type={showPassword ? "text" : "password"}
                    value={field.state.value}
                    onChange={(event) => field.handleChange(event.target.value)}
                    onBlur={field.handleBlur}
                    autoComplete="current-password"
                    ref={passwordRef}
                    className="pr-9"
                    aria-invalid={formError !== null}
                    aria-describedby={formError !== null ? "login-error" : undefined}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    className="absolute top-1/2 right-1 -translate-y-1/2"
                    aria-label={showPassword ? "Hide password" : "Show password"}
                    aria-pressed={showPassword}
                    onClick={() => setShowPassword((value) => !value)}
                  >
                    {showPassword ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
                  </Button>
                </div>
              </div>
            )}
          </form.Field>
          <Button type="submit" className="w-full" disabled={submitting || throttleRemaining > 0}>
            Log in
          </Button>
        </form>
        {config?.oidcEnabled === true ? (
          <>
            <div className="text-muted-foreground flex items-center gap-3 text-xs">
              <span className="h-px flex-1 bg-border" aria-hidden="true" />
              or
              <span className="h-px flex-1 bg-border" aria-hidden="true" />
            </div>
            <Button
              type="button"
              variant="outline"
              className="w-full"
              onClick={() => {
                window.location.href = "/api/v1/auth/oidc/login";
              }}
            >
              Log in with single sign-on
            </Button>
          </>
        ) : null}
      </div>
      {config ? <p className="text-muted-foreground text-xs">v{config.version}</p> : null}
    </div>
  );
}
