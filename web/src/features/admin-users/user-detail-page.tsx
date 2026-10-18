import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { z } from "zod";
import { describeError, isApiError } from "@/api/errors";
import { keys } from "@/api/keys";
import { setSlotQuota, unlinkOIDCIdentity, updateUser } from "@/api/mutations/admin";
import {
  type AdminUser,
  type AdminUserList,
  adminUserQueryOptions,
  type SlotQuotaView,
  slotQuotaQueryOptions,
} from "@/api/queries/admin";
import { configQueryOptions } from "@/api/queries/config";
import { meQueryOptions } from "@/api/queries/me";
import { ErrorPanel } from "@/components/app/error-panel";
import { Alert, AlertTitle } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { LinkOIDCDialog } from "@/features/admin-users/link-oidc-dialog";
import { ResetPasswordDialog } from "@/features/admin-users/reset-password-dialog";
import { copyText } from "@/lib/browser";
import { defaultJobsSearch } from "@/lib/search";

const slotQuotaSchema = z.coerce.number().int().min(0, "Enter a whole number, 0 or more.");

const createdFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

function upsertUser(list: AdminUserList | undefined, user: AdminUser): AdminUserList {
  const users = list?.users ?? [];
  const without = users.filter((entry) => entry.id !== user.id);
  return { users: [...without, user].sort((a, b) => a.username.localeCompare(b.username)) };
}

/** The admin user detail page (§5.11). */
export function UserDetailPage({ userId }: { userId: string }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const userQuery = useQuery(adminUserQueryOptions(userId));
  const quotaQuery = useQuery(slotQuotaQueryOptions(userId));
  const meQuery = useQuery(meQueryOptions);
  const configQuery = useQuery(configQueryOptions);

  const [role, setRole] = useState<"user" | "admin">("user");
  const [status, setStatus] = useState<"active" | "disabled">("active");
  const [saving, setSaving] = useState(false);
  const [saveAlert, setSaveAlert] = useState<string | null>(null);
  const [disableConfirm, setDisableConfirm] = useState(false);

  const [quota, setQuota] = useState("");
  const [quotaError, setQuotaError] = useState<string | null>(null);
  const [quotaSaving, setQuotaSaving] = useState(false);
  const [quotaConfirm, setQuotaConfirm] = useState<number | null>(null);

  const [resetOpen, setResetOpen] = useState(false);
  const [linkOpen, setLinkOpen] = useState(false);
  const [unlinkConfirm, setUnlinkConfirm] = useState(false);
  const [unlinkAlert, setUnlinkAlert] = useState<string | null>(null);

  const user = userQuery.data;
  const quotaView: SlotQuotaView | undefined = quotaQuery.data;
  const isSelf = user !== undefined && meQuery.data?.user.id === user.id;

  useEffect(() => {
    if (user !== undefined) {
      setRole(user.role);
      setStatus(user.status);
    }
  }, [user]);

  useEffect(() => {
    if (quotaView !== undefined) {
      setQuota(String(quotaView.maxActiveJobs));
    }
  }, [quotaView]);

  if (userQuery.isPending) {
    return (
      <div className="space-y-3 p-6">
        <Skeleton className="h-6 w-40" />
        <Skeleton className="h-24 w-full max-w-2xl" />
        <Skeleton className="h-24 w-full max-w-2xl" />
      </div>
    );
  }

  if (userQuery.isError) {
    if (isApiError(userQuery.error) && userQuery.error.slug === "user-not-found") {
      return (
        <div className="flex flex-col items-start gap-3 p-6">
          <h1 className="text-base font-semibold">This user doesn&apos;t exist.</h1>
          <Button asChild variant="outline" size="sm">
            <Link to="/admin/users">Back to users</Link>
          </Button>
        </div>
      );
    }
    return (
      <div className="p-6">
        <ErrorPanel error={userQuery.error} onRetry={() => void userQuery.refetch()} />
      </div>
    );
  }

  if (user === undefined) {
    return null;
  }

  const doSave = async (request: { role?: "user" | "admin"; status?: "active" | "disabled" }) => {
    setSaving(true);
    setSaveAlert(null);
    try {
      const updated = await updateUser(user.id, request);
      queryClient.setQueryData(keys.admin.user(userId), updated);
      queryClient.setQueryData<AdminUserList>(keys.admin.users, (list) =>
        upsertUser(list, updated),
      );
      if (isSelf) {
        await queryClient.invalidateQueries({ queryKey: keys.me });
      }
      toast.success("Saved.");
      if (isSelf && updated.role === "user") {
        void navigate({ to: "/", search: defaultJobsSearch, replace: true });
      }
    } catch (error) {
      // The server applies role then status, not atomically (§4.4); refetch to
      // show what was actually saved.
      await queryClient.invalidateQueries({ queryKey: keys.admin.user(userId) });
      if (isApiError(error) && error.slug === "last-admin") {
        setSaveAlert(describeError(error, { username: user.username }).text);
      } else if (isApiError(error)) {
        setSaveAlert(describeError(error).title);
      } else {
        setSaveAlert("Something went wrong.");
      }
    } finally {
      setSaving(false);
    }
  };

  const save = () => {
    const request: { role?: "user" | "admin"; status?: "active" | "disabled" } = {};
    if (role !== user.role) {
      request.role = role;
    }
    if (status !== user.status) {
      request.status = status;
    }
    if (request.role === undefined && request.status === undefined) {
      return;
    }
    if (status === "disabled" && user.status === "active") {
      setDisableConfirm(true);
      return;
    }
    void doSave(request);
  };

  const saveQuota = () => {
    setQuotaError(null);
    const result = slotQuotaSchema.safeParse(quota);
    if (!result.success) {
      setQuotaError(result.error.issues[0]?.message ?? "Enter a whole number, 0 or more.");
      return;
    }
    const value = result.data;
    if (quotaView !== undefined && value < quotaView.slotsUsed) {
      setQuotaConfirm(value);
      return;
    }
    void doSetQuota(value);
  };

  const doSetQuota = async (value: number) => {
    setQuotaSaving(true);
    setQuotaError(null);
    try {
      await setSlotQuota(user.id, value);
      toast.success(`Quota set to ${value}.`);
      await queryClient.invalidateQueries({ queryKey: keys.admin.quota(userId) });
    } catch (error) {
      if (isApiError(error) && error.slug === "invalid-slot-quota") {
        setQuotaError("Enter a whole number, 0 or more.");
      } else {
        setQuotaError(describeError(error).title);
      }
    } finally {
      setQuotaSaving(false);
    }
  };

  const unlink = async () => {
    setUnlinkAlert(null);
    try {
      const updated = await unlinkOIDCIdentity(user.id);
      queryClient.setQueryData(keys.admin.user(userId), updated);
      queryClient.setQueryData<AdminUserList>(keys.admin.users, (list) =>
        upsertUser(list, updated),
      );
    } catch (error) {
      if (isApiError(error) && error.slug === "no-login-method") {
        setUnlinkAlert(describeError(error, { username: user.username }).text);
      } else {
        setUnlinkAlert(describeError(error).title);
      }
    } finally {
      setUnlinkConfirm(false);
    }
  };

  const oidc = user.oidcIdentity;
  const oidcEnabled = configQuery.data?.oidcEnabled === true;

  return (
    <div className="max-w-2xl space-y-6 p-6">
      <div className="space-y-2">
        <Button asChild variant="ghost" size="sm" className="-ml-2 text-muted-foreground">
          <Link to="/admin/users">← Users</Link>
        </Button>
        <h1 className="text-base font-semibold">
          {user.username} · created {createdFormat.format(new Date(user.createdAt))}
        </h1>
      </div>

      <div className="space-y-3">
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
          <Label>Status</Label>
          <RadioGroup
            value={status}
            onValueChange={(value) => setStatus(value === "disabled" ? "disabled" : "active")}
            className="flex gap-6"
          >
            <Label className="font-normal">
              <RadioGroupItem value="active" /> Active
            </Label>
            <Label className="font-normal">
              <RadioGroupItem value="disabled" /> Disabled
            </Label>
          </RadioGroup>
          <p className="text-muted-foreground text-xs">
            Disabling logs {user.username} out everywhere and stops their download links.
          </p>
        </div>
        {isSelf ? (
          <Alert>
            <AlertTitle>You&apos;re changing your own account.</AlertTitle>
          </Alert>
        ) : null}
        {saveAlert !== null ? (
          <Alert variant="destructive">
            <AlertTitle>{saveAlert}</AlertTitle>
          </Alert>
        ) : null}
        <Button
          size="sm"
          disabled={saving || (role === user.role && status === user.status)}
          onClick={() => void save()}
        >
          Save
        </Button>
      </div>

      <Separator />

      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <div className="text-sm">
            <span className="font-medium">Password</span>{" "}
            <span className="text-muted-foreground">{user.passwordLogin ? "set ✓" : "none"}</span>
          </div>
          <Button variant="outline" size="sm" onClick={() => setResetOpen(true)}>
            Reset password…
          </Button>
        </div>

        <div className="flex items-start justify-between gap-4">
          <div className="text-sm">
            <span className="font-medium">Single sign-on</span>{" "}
            {oidc !== undefined ? (
              <span className="text-muted-foreground">
                {oidc.issuer} · <span className="font-mono">{oidc.subject}</span>{" "}
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label="Copy subject"
                  onClick={() => void copyText(oidc.subject)}
                >
                  ⧉
                </Button>
              </span>
            ) : (
              <span className="text-muted-foreground">not linked</span>
            )}
            {!oidcEnabled ? (
              <p className="text-muted-foreground text-xs">Single sign-on is off on this server.</p>
            ) : null}
          </div>
          <div className="flex shrink-0 gap-2">
            {oidc !== undefined ? (
              <Button
                variant="outline"
                size="sm"
                disabled={!oidcEnabled}
                onClick={() => setUnlinkConfirm(true)}
              >
                Unlink
              </Button>
            ) : null}
            <Button
              variant="outline"
              size="sm"
              disabled={!oidcEnabled}
              onClick={() => setLinkOpen(true)}
            >
              {oidc !== undefined ? "Change…" : "Link…"}
            </Button>
          </div>
        </div>

        {unlinkAlert !== null ? (
          <Alert variant="destructive">
            <AlertTitle>{unlinkAlert}</AlertTitle>
          </Alert>
        ) : null}

        <div className="flex items-end justify-between gap-4">
          <div className="grid gap-2">
            <Label htmlFor="slot-quota">Slot quota</Label>
            <div className="flex items-center gap-2">
              <Input
                id="slot-quota"
                className="h-8 w-24"
                value={quota}
                onChange={(event) => setQuota(event.target.value)}
                aria-invalid={quotaError !== null}
                aria-describedby={quotaError !== null ? "slot-quota-error" : undefined}
              />
              <span className="text-sm">
                {`active jobs${quotaView?.isDefault === true ? " (default)" : ""} · ${quotaView?.slotsUsed ?? 0} in use`}
              </span>
            </div>
            {quotaError !== null ? (
              <p id="slot-quota-error" role="alert" className="text-state-failed text-xs">
                {quotaError}
              </p>
            ) : null}
          </div>
          <Button size="sm" variant="outline" disabled={quotaSaving} onClick={saveQuota}>
            Set
          </Button>
        </div>

        <div className="flex items-center gap-2 text-sm">
          <span className="font-medium">Jobs</span>
          <Link
            to="/admin/jobs"
            search={{ ...defaultJobsSearch, owner: user.username }}
            className="text-primary hover:underline"
          >
            Show {user.username}&apos;s jobs →
          </Link>
        </div>
      </div>

      <AlertDialog open={disableConfirm} onOpenChange={setDisableConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Disable {user.username}?</AlertDialogTitle>
            <AlertDialogDescription>
              They&apos;re logged out at once and their download links stop working. Their jobs
              stay.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                setDisableConfirm(false);
                void doSave({ status: "disabled" });
              }}
            >
              Disable
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={unlinkConfirm} onOpenChange={setUnlinkConfirm}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Unlink single sign-on?</AlertDialogTitle>
            <AlertDialogDescription>
              {user.username} will sign in with their password only.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => void unlink()}>
              Unlink
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={quotaConfirm !== null}
        onOpenChange={(open) => !open && setQuotaConfirm(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Lower {user.username}&apos;s quota?</AlertDialogTitle>
            <AlertDialogDescription>
              {user.username} already has {quotaView?.slotsUsed ?? 0} active jobs. They keep them,
              but can&apos;t add more until they&apos;re below {quotaConfirm ?? 0}.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                const value = quotaConfirm;
                setQuotaConfirm(null);
                if (value !== null) {
                  void doSetQuota(value);
                }
              }}
            >
              Set
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {resetOpen ? (
        <ResetPasswordDialog
          userId={user.id}
          username={user.username}
          open
          onOpenChange={(open) => !open && setResetOpen(false)}
        />
      ) : null}
      {linkOpen ? (
        <LinkOIDCDialog
          userId={user.id}
          initial={oidc}
          open
          onOpenChange={(open) => !open && setLinkOpen(false)}
        />
      ) : null}
    </div>
  );
}
