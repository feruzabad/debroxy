import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { describeError, isApiError } from "@/api/errors";
import { keys } from "@/api/keys";
import { linkOIDCIdentity } from "@/api/mutations/admin";
import type { AdminUserList } from "@/api/queries/admin";
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
import { oidcIssuerSchema, oidcSubjectSchema } from "@/features/admin-users/schema";

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

/** The link/change OIDC identity dialog (§5.11). */
export function LinkOIDCDialog({
  userId,
  initial,
  open,
  onOpenChange,
}: {
  userId: string;
  initial: { issuer: string; subject: string } | undefined;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const [issuer, setIssuer] = useState(initial?.issuer ?? "");
  const [subject, setSubject] = useState(initial?.subject ?? "");
  const [issuerError, setIssuerError] = useState<string | null>(null);
  const [subjectError, setSubjectError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const submit = async () => {
    setIssuerError(null);
    setSubjectError(null);
    const issuerResult = oidcIssuerSchema.safeParse(issuer);
    if (!issuerResult.success) {
      setIssuerError(issuerResult.error.issues[0]?.message ?? "The issuer must be an https URL.");
      return;
    }
    const subjectResult = oidcSubjectSchema.safeParse(subject);
    if (!subjectResult.success) {
      setSubjectError(subjectResult.error.issues[0]?.message ?? "The subject must not be empty.");
      return;
    }
    setSubmitting(true);
    try {
      const updated = await linkOIDCIdentity(userId, {
        issuer: issuerResult.data,
        subject: subjectResult.data,
      });
      queryClient.setQueryData(keys.admin.user(userId), updated);
      queryClient.setQueryData<AdminUserList>(keys.admin.users, (list) => {
        const users = list?.users ?? [];
        const without = users.filter((entry) => entry.id !== updated.id);
        return {
          users: [...without, updated].sort((a, b) => a.username.localeCompare(b.username)),
        };
      });
      onOpenChange(false);
    } catch (error) {
      if (isApiError(error) && error.slug === "oidc-identity-taken") {
        setSubjectError("Another user already has this identity.");
      } else if (isApiError(error) && error.slug === "invalid-oidc-identity") {
        setIssuerError("The issuer must be an https URL and the subject must not be empty.");
      } else {
        setSubjectError(describeError(error).title);
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>
            {initial !== undefined ? "Change single sign-on" : "Link single sign-on"}
          </DialogTitle>
          <DialogDescription>
            The identity the user signs in with at the provider.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <Label htmlFor="oidc-issuer">Issuer</Label>
          <Input
            id="oidc-issuer"
            value={issuer}
            onChange={(event) => setIssuer(event.target.value)}
            placeholder="https://accounts.example.org"
            aria-invalid={issuerError !== null}
            aria-describedby={issuerError !== null ? "oidc-issuer-error" : undefined}
          />
          <FieldError id="oidc-issuer-error" error={issuerError} />
          <Label htmlFor="oidc-subject">Subject</Label>
          <Input
            id="oidc-subject"
            value={subject}
            onChange={(event) => setSubject(event.target.value)}
            aria-invalid={subjectError !== null}
            aria-describedby={subjectError !== null ? "oidc-subject-error" : undefined}
          />
          <FieldError id="oidc-subject-error" error={subjectError} />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button disabled={submitting} onClick={() => void submit()}>
            Save
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
