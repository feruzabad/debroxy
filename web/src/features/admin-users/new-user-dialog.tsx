import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Eye, EyeOff } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { describeError, isApiError } from "@/api/errors";
import { keys } from "@/api/keys";
import { type AdminUser, createUser } from "@/api/mutations/admin";
import type { AdminUserList } from "@/api/queries/admin";
import { configQueryOptions } from "@/api/queries/config";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
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
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  generatePassword,
  newPasswordSchema,
  oidcIssuerSchema,
  oidcSubjectSchema,
  USERNAME_RULES,
  usernameSchema,
} from "@/features/admin-users/schema";
import { copyText } from "@/lib/browser";
import { cn } from "@/lib/utils";

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

/** The create-user dialog (§5.10), overlaid on the users page. */
export function NewUserDialog() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { data: config } = useQuery(configQueryOptions);
  const minLength = config?.passwordMinLength ?? 12;
  const oidcEnabled = config?.oidcEnabled === true;

  const [username, setUsername] = useState("");
  const [role, setRole] = useState<"user" | "admin">("user");
  const [passwordEnabled, setPasswordEnabled] = useState(true);
  const [oidcChecked, setOidcChecked] = useState(false);
  const [password, setPassword] = useState("");
  const [repeat, setRepeat] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [issuer, setIssuer] = useState("");
  const [subject, setSubject] = useState("");

  const [usernameError, setUsernameError] = useState<string | null>(null);
  const [passwordError, setPasswordError] = useState<string | null>(null);
  const [repeatError, setRepeatError] = useState<string | null>(null);
  const [issuerError, setIssuerError] = useState<string | null>(null);
  const [subjectError, setSubjectError] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const close = () => void navigate({ to: "/admin/users", replace: true });

  const generate = () => {
    const value = generatePassword();
    setPassword(value);
    setRepeat(value);
    setShowPassword(true);
    setPasswordError(null);
    setRepeatError(null);
  };

  const copyGenerated = () => {
    void copyText(password);
    toast.success("Password copied.");
  };

  const submit = async () => {
    setUsernameError(null);
    setPasswordError(null);
    setRepeatError(null);
    setIssuerError(null);
    setSubjectError(null);
    setFormError(null);

    const usernameResult = usernameSchema.safeParse(username);
    if (!usernameResult.success) {
      setUsernameError(usernameResult.error.issues[0]?.message ?? USERNAME_RULES);
      return;
    }

    let passwordValue: string | null = null;
    if (passwordEnabled) {
      const passwordResult = newPasswordSchema(minLength).safeParse(password);
      if (!passwordResult.success) {
        setPasswordError(
          passwordResult.error.issues[0]?.message ?? `Use at least ${minLength} characters.`,
        );
        return;
      }
      passwordValue = passwordResult.data;
      if (repeat !== passwordValue) {
        setRepeatError("The passwords don't match.");
        return;
      }
    }

    let identity: { issuer: string; subject: string } | null = null;
    if (oidcChecked) {
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
      identity = { issuer: issuerResult.data, subject: subjectResult.data };
    }

    if (!passwordEnabled && !oidcChecked) {
      setFormError("Choose at least one way to sign in.");
      return;
    }

    setSubmitting(true);
    try {
      const created: AdminUser = await createUser({
        username: usernameResult.data,
        role,
        ...(passwordValue !== null ? { password: passwordValue } : {}),
        ...(identity !== null ? { oidcIdentity: identity } : {}),
      });
      queryClient.setQueryData<AdminUserList>(keys.admin.users, (list) =>
        list === undefined
          ? { users: [created] }
          : {
              users: [...list.users, created].sort((a, b) => a.username.localeCompare(b.username)),
            },
      );
      toast.success(`Created ${created.username}.`);
      void navigate({ to: "/admin/users/$userId", params: { userId: created.id } });
    } catch (error) {
      if (isApiError(error)) {
        switch (error.slug) {
          case "username-taken":
            setUsernameError("Pick another username.");
            return;
          case "invalid-username":
            setUsernameError(USERNAME_RULES);
            return;
          case "invalid-password":
            setPasswordError(`Use at least ${minLength} characters and at most 1024 bytes.`);
            return;
          case "invalid-role":
            setFormError("Pick User or Admin.");
            return;
          case "invalid-oidc-identity":
            setIssuerError("The issuer must be an https URL and the subject must not be empty.");
            return;
          case "oidc-identity-taken":
            setSubjectError("Another user already has this identity.");
            return;
          case "no-login-method":
            setFormError(
              `${usernameResult.data} would have no way to sign in. Set a password first.`,
            );
            return;
          default:
            break;
        }
      }
      const { title } = describeError(error);
      setFormError(title);
    } finally {
      setSubmitting(false);
    }
  };

  const preview = username.trim();

  return (
    <Dialog open onOpenChange={(open) => !open && close()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New user</DialogTitle>
          <DialogDescription>
            Create an account with a password or single sign-on.
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-2">
          <Label htmlFor="new-user-username">Username</Label>
          <Input
            id="new-user-username"
            value={username}
            onChange={(event) => setUsername(event.target.value.toLowerCase())}
            aria-invalid={usernameError !== null}
            aria-describedby={usernameError !== null ? "new-user-username-error" : undefined}
          />
          {preview !== "" && preview !== username ? (
            <p className="text-muted-foreground text-xs">will be saved as {preview}</p>
          ) : null}
          <FieldError id="new-user-username-error" error={usernameError} />
        </div>

        <div className="grid gap-2">
          <Label>Role</Label>
          <RadioGroup
            value={role}
            onValueChange={(value) => setRole(value === "admin" ? "admin" : "user")}
            className="flex gap-6"
          >
            <Label className="font-normal">
              <RadioGroupItem value="user" /> User
            </Label>
            <Label className="font-normal">
              <RadioGroupItem value="admin" /> Admin
            </Label>
          </RadioGroup>
        </div>

        <div className="grid gap-2">
          <Label>Sign-in</Label>
          <Label className="font-normal">
            <Checkbox
              checked={passwordEnabled}
              onCheckedChange={(checked) => setPasswordEnabled(checked === true)}
            />{" "}
            Password
          </Label>
          {oidcEnabled ? (
            <Label className="font-normal">
              <Checkbox
                checked={oidcChecked}
                onCheckedChange={(checked) => setOidcChecked(checked === true)}
              />{" "}
              Single sign-on
            </Label>
          ) : null}
        </div>

        {passwordEnabled ? (
          <div className="grid gap-2">
            <Label htmlFor="new-user-password">Password</Label>
            <div className="relative">
              <Input
                id="new-user-password"
                type={showPassword ? "text" : "password"}
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                autoComplete="new-password"
                aria-invalid={passwordError !== null}
                aria-describedby={passwordError !== null ? "new-user-password-error" : undefined}
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
            <FieldError id="new-user-password-error" error={passwordError} />
            <Label htmlFor="new-user-repeat">Repeat</Label>
            <Input
              id="new-user-repeat"
              type="password"
              value={repeat}
              onChange={(event) => setRepeat(event.target.value)}
              autoComplete="new-password"
              aria-invalid={repeatError !== null}
              aria-describedby={repeatError !== null ? "new-user-repeat-error" : undefined}
            />
            <FieldError id="new-user-repeat-error" error={repeatError} />
            <div className="flex gap-2">
              <Button type="button" variant="outline" size="sm" onClick={generate}>
                Generate
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={password === ""}
                onClick={copyGenerated}
              >
                Copy
              </Button>
            </div>
          </div>
        ) : null}

        {oidcChecked ? (
          <div className="grid gap-2">
            <Label htmlFor="new-user-issuer">Issuer</Label>
            <Input
              id="new-user-issuer"
              value={issuer}
              onChange={(event) => setIssuer(event.target.value)}
              placeholder="https://accounts.example.org"
              aria-invalid={issuerError !== null}
              aria-describedby={issuerError !== null ? "new-user-issuer-error" : undefined}
            />
            <p className="text-muted-foreground text-xs">
              Exactly the iss value of your identity provider, e.g. https://accounts.example.org
            </p>
            <FieldError id="new-user-issuer-error" error={issuerError} />
            <Label htmlFor="new-user-subject">Subject</Label>
            <Input
              id="new-user-subject"
              value={subject}
              onChange={(event) => setSubject(event.target.value)}
              aria-invalid={subjectError !== null}
              aria-describedby={subjectError !== null ? "new-user-subject-error" : undefined}
            />
            <p className="text-muted-foreground text-xs">
              The user&apos;s sub claim at that provider.
            </p>
            <FieldError id="new-user-subject-error" error={subjectError} />
          </div>
        ) : null}

        {formError !== null ? (
          <Alert variant="destructive">
            <AlertTitle>{formError}</AlertTitle>
            {formError === "Choose at least one way to sign in." ? (
              <AlertDescription>Pick a password or single sign-on.</AlertDescription>
            ) : null}
          </Alert>
        ) : null}

        <DialogFooter>
          <Button variant="outline" onClick={close}>
            Cancel
          </Button>
          <Button
            disabled={submitting}
            className={cn("bg-primary text-primary-foreground hover:bg-primary/90")}
            onClick={() => void submit()}
          >
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
