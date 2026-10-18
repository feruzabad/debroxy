import { useQuery } from "@tanstack/react-query";
import { meQueryOptions } from "@/api/queries/me";
import { ErrorPanel } from "@/components/app/error-panel";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { ChangePasswordForm } from "@/features/account/change-password-form";
import { formatExactTime, formatRelativeTime } from "@/lib/format";

const memberSinceFormat = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

function mark(yes: boolean): string {
  return yes ? "✓" : "—";
}

function titleCase(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1);
}

/** The account page (§5.9): profile, session, submit rate and the password form. */
export function AccountPage() {
  const meQuery = useQuery(meQueryOptions);

  if (meQuery.isPending) {
    return (
      <div className="space-y-1 p-6">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-6 w-96" />
        <Skeleton className="h-6 w-72" />
        <Skeleton className="h-6 w-64" />
      </div>
    );
  }

  if (meQuery.isError) {
    return (
      <div className="p-6">
        <ErrorPanel error={meQuery.error} onRetry={() => void meQuery.refetch()} />
      </div>
    );
  }

  const me = meQuery.data;
  const { user, session, quota, submitRate } = me;

  return (
    <div className="max-w-2xl space-y-6 p-6">
      <h1 className="text-base font-semibold">Account</h1>
      <div className="space-y-1 text-sm">
        <p>
          {user.username} · {titleCase(user.role)} · {user.status} · member since{" "}
          {memberSinceFormat.format(new Date(user.createdAt))}
        </p>
        <p>
          Sign-in methods: Password {mark(user.passwordLogin)} · Single sign-on{" "}
          {mark(user.oidcLinked)}
        </p>
        <p>
          Session: idle logout {formatRelativeTime(session.idleExpiresAt)} · ends{" "}
          {formatExactTime(session.absoluteExpiresAt)}
        </p>
        <p>
          Slots: <span className="tabular-nums">{quota.slotsUsed}</span> of{" "}
          <span className="tabular-nums">{quota.maxActiveJobs}</span> · Submits:{" "}
          <span className="tabular-nums">{submitRate.uncached.used}</span> of{" "}
          <span className="tabular-nums">{submitRate.uncached.perHour}</span> this hour · cached{" "}
          <span className="tabular-nums">{submitRate.cached.used}</span> of{" "}
          <span className="tabular-nums">{submitRate.cached.perHour}</span>
        </p>
      </div>
      <Separator />
      {user.passwordLogin ? (
        <ChangePasswordForm />
      ) : (
        <p className="text-muted-foreground text-sm">
          You sign in with single sign-on. You have no password here.
        </p>
      )}
    </div>
  );
}
