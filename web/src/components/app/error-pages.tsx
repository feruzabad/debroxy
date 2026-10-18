import { Link, useRouter, useRouterState } from "@tanstack/react-router";
import { isApiError } from "@/api/errors";
import { Button } from "@/components/ui/button";
import { defaultJobsSearch } from "@/lib/search";

export class ForbiddenError extends Error {
  constructor() {
    super("forbidden");
    this.name = "ForbiddenError";
  }
}

function describeError(error: unknown): string {
  if (isApiError(error)) {
    return error.message !== "" ? `${error.slug}: ${error.message}` : error.slug;
  }
  return "debroxy hit an unexpected error.";
}

export function NotFoundPage() {
  const path = useRouterState({ select: (state) => state.location.pathname });
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-3 p-6 text-center">
      <h1 className="text-base font-semibold">Page not found</h1>
      <p className="text-muted-foreground text-sm">There's nothing at {path}.</p>
      <Button asChild>
        <Link to="/" search={defaultJobsSearch}>
          Go to jobs
        </Link>
      </Button>
    </div>
  );
}

export function ForbiddenPage() {
  return (
    <div className="flex flex-col items-center justify-center gap-3 p-12 text-center">
      <h1 className="text-base font-semibold">Admins only</h1>
      <p className="text-muted-foreground text-sm">You don't have access to this page.</p>
      <Button asChild>
        <Link to="/" search={defaultJobsSearch}>
          Go to jobs
        </Link>
      </Button>
    </div>
  );
}

export function RouteErrorPanel({ error }: { error: unknown }) {
  const router = useRouter();
  return (
    <div className="flex flex-col items-start gap-3 p-6">
      <h1 className="text-base font-semibold">Something went wrong.</h1>
      <p className="text-muted-foreground text-sm">{describeError(error)}</p>
      <Button onClick={() => void router.invalidate()}>Try again</Button>
    </div>
  );
}

export function BootstrapErrorPage({ error }: { error: unknown }) {
  return (
    <div className="bg-background text-foreground flex min-h-screen flex-col items-center justify-center gap-3 p-6 text-center">
      <h1 className="text-base font-semibold">Can't load debroxy</h1>
      <p className="text-muted-foreground text-sm">{describeError(error)}</p>
      <Button onClick={() => window.location.reload()}>Try again</Button>
    </div>
  );
}
