import { useForm } from "@tanstack/react-form";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Info } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { z } from "zod";
import { describeError, isApiError } from "@/api/errors";
import { keys } from "@/api/keys";
import { changeOwnPassword } from "@/api/mutations/identity";
import { configQueryOptions } from "@/api/queries/config";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

function changePasswordSchema(minLength: number) {
  return z
    .object({
      currentPassword: z.string().min(1, "Enter your current password."),
      newPassword: z
        .string()
        .min(minLength, `Use at least ${minLength} characters.`)
        .refine((value) => new TextEncoder().encode(value).length <= 1024, "That's too long."),
      repeat: z.string(),
    })
    .refine((value) => value.repeat === value.newPassword, {
      message: "The passwords don't match.",
      path: ["repeat"],
    });
}

interface FieldErrorProps {
  id: string;
  error: string | null;
}

function FieldError({ id, error }: FieldErrorProps) {
  if (error === null) {
    return null;
  }
  return (
    <p id={id} className="text-state-failed text-xs">
      {error}
    </p>
  );
}

/** The change-password form (§5.9): TanStack Form + Zod, mirrored server rules. */
export function ChangePasswordForm() {
  const queryClient = useQueryClient();
  const { data: config } = useQuery(configQueryOptions);
  const minLength = config?.passwordMinLength ?? 12;

  const [submitting, setSubmitting] = useState(false);
  const [currentError, setCurrentError] = useState<string | null>(null);
  const [newError, setNewError] = useState<string | null>(null);
  const [repeatError, setRepeatError] = useState<string | null>(null);

  const form = useForm({
    defaultValues: { currentPassword: "", newPassword: "", repeat: "" },
    onSubmit: async ({ value }) => {
      setCurrentError(null);
      setNewError(null);
      setRepeatError(null);
      const parsed = changePasswordSchema(minLength).safeParse(value);
      if (!parsed.success) {
        for (const issue of parsed.error.issues) {
          const field = issue.path[0];
          if (field === "currentPassword") {
            setCurrentError(issue.message);
          } else if (field === "newPassword") {
            setNewError(issue.message);
          } else if (field === "repeat") {
            setRepeatError(issue.message);
          }
        }
        return;
      }
      setSubmitting(true);
      try {
        await changeOwnPassword(value.currentPassword, value.newPassword);
        toast.success("Password changed. Other sessions were logged out.");
        await queryClient.invalidateQueries({ queryKey: keys.me });
        form.reset();
      } catch (error) {
        if (isApiError(error) && error.slug === "wrong-current-password") {
          setCurrentError("That isn't your current password.");
        } else if (isApiError(error) && error.slug === "invalid-password") {
          setNewError(error.message);
        } else {
          toast.error(describeError(error).title, { duration: 10_000 });
        }
      } finally {
        setSubmitting(false);
      }
    },
  });

  return (
    <div className="max-w-md space-y-3">
      <h2 className="text-sm font-semibold">Change password</h2>
      <form
        className="space-y-3"
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
          void form.handleSubmit();
        }}
      >
        <form.Field name="currentPassword">
          {(field) => (
            <div className="grid gap-1.5">
              <Label htmlFor="current-password">Current password</Label>
              <Input
                id="current-password"
                name={field.name}
                type="password"
                value={field.state.value}
                onChange={(event) => field.handleChange(event.target.value)}
                onBlur={field.handleBlur}
                autoComplete="current-password"
                aria-invalid={currentError !== null}
                aria-describedby={currentError !== null ? "current-password-error" : undefined}
              />
              <FieldError id="current-password-error" error={currentError} />
            </div>
          )}
        </form.Field>
        <form.Field name="newPassword">
          {(field) => (
            <div className="grid gap-1.5">
              <Label htmlFor="new-password">
                New password
                <span className="text-muted-foreground font-normal">
                  at least {minLength} characters
                </span>
              </Label>
              <Input
                id="new-password"
                name={field.name}
                type="password"
                value={field.state.value}
                onChange={(event) => field.handleChange(event.target.value)}
                onBlur={field.handleBlur}
                autoComplete="new-password"
                aria-invalid={newError !== null}
                aria-describedby={newError !== null ? "new-password-error" : undefined}
              />
              <FieldError id="new-password-error" error={newError} />
            </div>
          )}
        </form.Field>
        <form.Field name="repeat">
          {(field) => (
            <div className="grid gap-1.5">
              <Label htmlFor="repeat-password">Repeat new</Label>
              <Input
                id="repeat-password"
                name={field.name}
                type="password"
                value={field.state.value}
                onChange={(event) => field.handleChange(event.target.value)}
                onBlur={field.handleBlur}
                autoComplete="new-password"
                aria-invalid={repeatError !== null}
                aria-describedby={repeatError !== null ? "repeat-password-error" : undefined}
              />
              <FieldError id="repeat-password-error" error={repeatError} />
            </div>
          )}
        </form.Field>
        <p className="text-muted-foreground flex items-center gap-1.5 text-xs">
          <Info className="size-3.5 shrink-0" aria-hidden="true" />
          Changing your password logs you out everywhere else.
        </p>
        <Button type="submit" disabled={submitting}>
          Change password
        </Button>
      </form>
    </div>
  );
}
