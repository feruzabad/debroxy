import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Eye, EyeOff } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { describeError, isApiError } from "@/api/errors";
import { keys } from "@/api/keys";
import { resetUserPassword } from "@/api/mutations/admin";
import { configQueryOptions } from "@/api/queries/config";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { generatePassword, newPasswordSchema } from "@/features/admin-users/schema";
import { copyText } from "@/lib/browser";

function FieldError({ id, error }: { id: string; error: string | null }) {
  if (error === null) {
    return null;
  }
  return (
    <p id={id} role="alert" className="text-state-failed text-xs">
      {error}
    </p>
  );
}

/** The reset-password dialog (§5.11), shared rules with §5.10. */
export function ResetPasswordDialog({
  userId,
  username,
  open,
  onOpenChange,
}: {
  userId: string;
  username: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const { data: config } = useQuery(configQueryOptions);
  const minLength = config?.passwordMinLength ?? 12;

  const [password, setPassword] = useState("");
  const [repeat, setRepeat] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [passwordError, setPasswordError] = useState<string | null>(null);
  const [repeatError, setRepeatError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const generate = () => {
    const value = generatePassword();
    setPassword(value);
    setRepeat(value);
    setShowPassword(true);
    setPasswordError(null);
    setRepeatError(null);
  };

  const submit = async () => {
    setPasswordError(null);
    setRepeatError(null);
    const passwordResult = newPasswordSchema(minLength).safeParse(password);
    if (!passwordResult.success) {
      setPasswordError(
        passwordResult.error.issues[0]?.message ?? `Use at least ${minLength} characters.`,
      );
      return;
    }
    if (repeat !== passwordResult.data) {
      setRepeatError("The passwords don't match.");
      return;
    }
    setSubmitting(true);
    try {
      await resetUserPassword(userId, passwordResult.data);
      toast.success(`Password reset. ${username} was logged out everywhere.`);
      await queryClient.invalidateQueries({ queryKey: keys.admin.user(userId) });
      onOpenChange(false);
    } catch (error) {
      if (isApiError(error) && error.slug === "invalid-password") {
        setPasswordError(`Use at least ${minLength} characters and at most 1024 bytes.`);
      } else {
        toast.error(describeError(error).title, { duration: 10_000 });
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Reset password</DialogTitle>
          <DialogDescription>Set a new password for {username}.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <Label htmlFor="reset-password">Password</Label>
          <div className="relative">
            <Input
              id="reset-password"
              type={showPassword ? "text" : "password"}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="new-password"
              aria-invalid={passwordError !== null}
              aria-describedby={passwordError !== null ? "reset-password-error" : undefined}
              className="pr-9"
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
          <FieldError id="reset-password-error" error={passwordError} />
          <Label htmlFor="reset-repeat">Repeat</Label>
          <Input
            id="reset-repeat"
            type="password"
            value={repeat}
            onChange={(event) => setRepeat(event.target.value)}
            autoComplete="new-password"
            aria-invalid={repeatError !== null}
            aria-describedby={repeatError !== null ? "reset-repeat-error" : undefined}
          />
          <FieldError id="reset-repeat-error" error={repeatError} />
          <div className="flex gap-2">
            <Button type="button" variant="outline" size="sm" onClick={generate}>
              Generate
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={password === ""}
              onClick={() => void copyText(password)}
            >
              Copy
            </Button>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button disabled={submitting} onClick={() => void submit()}>
            Reset
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
