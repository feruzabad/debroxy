import { useEffect, useState } from "react";
import type { Me } from "@/api/queries/me";
import { Button } from "@/components/ui/button";
import { formatCountdown } from "@/lib/format";

const IDLE_WARN_MS = 5 * 60_000;
const ABSOLUTE_WARN_MS = 10 * 60_000;

function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(id);
  }, [intervalMs]);
  return now;
}

/**
 * The session idle/absolute expiry banners (§5.2.1). Renders nothing until one
 * of the deadlines is close; a 1 s tick drives the live countdown.
 */
export function SessionBanner({
  me,
  onStayLoggedIn,
  onLogInAgain,
}: {
  me: Me;
  onStayLoggedIn: () => void;
  onLogInAgain: () => void;
}) {
  const now = useNow(1000);
  const idleRemaining = Date.parse(me.session.idleExpiresAt) - now;
  const absoluteRemaining = Date.parse(me.session.absoluteExpiresAt) - now;

  if (idleRemaining > 0 && idleRemaining <= IDLE_WARN_MS) {
    return (
      <div
        role="alert"
        className="bg-state-warning/10 text-state-warning flex items-center justify-between gap-3 border-b px-4 py-2 text-sm"
      >
        <span>
          You&apos;ll be logged out in {formatCountdown(idleRemaining / 1000)} because of
          inactivity.
        </span>
        <Button size="sm" variant="outline" onClick={onStayLoggedIn}>
          Stay logged in
        </Button>
      </div>
    );
  }

  if (absoluteRemaining > 0 && absoluteRemaining <= ABSOLUTE_WARN_MS) {
    const minutes = Math.ceil(absoluteRemaining / 60_000);
    return (
      <div
        role="alert"
        className="bg-state-warning/10 text-state-warning flex items-center justify-between gap-3 border-b px-4 py-2 text-sm"
      >
        <span>Your session ends in {minutes}m. Log in again to continue.</span>
        <Button size="sm" variant="outline" onClick={onLogInAgain}>
          Log in again
        </Button>
      </div>
    );
  }

  return null;
}
