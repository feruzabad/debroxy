import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronDown, RefreshCw } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { describeError, isApiError } from "@/api/errors";
import { keys } from "@/api/keys";
import { issueAllLinks } from "@/api/links";
import { useConnectionState } from "@/api/live";
import { refreshProvider } from "@/api/mutations/jobs";
import type { Job, Provider } from "@/api/queries/jobs";
import { jobsQueryOptions, liveStaleTime, providersQueryOptions } from "@/api/queries/jobs";
import { meQueryOptions } from "@/api/queries/me";
import type { ProviderWithStale } from "@/api/types-rev13";
import { ErrorPanel } from "@/components/app/error-panel";
import { JobTable } from "@/components/app/job-table";
import { ReleaseDialog } from "@/components/app/release-dialog";
import { RenameDialog } from "@/components/app/rename-dialog";
import { useCountdown } from "@/components/app/use-countdown";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { copyText } from "@/lib/browser";
import { distinctProviders, filterJobs, groupCounts, type JobSortKey, sortJobs } from "@/lib/jobs";
import { defaultJobsSearch, type JobsSearch } from "@/lib/search";
import { cn } from "@/lib/utils";

const GROUPS = ["all", "active", "ready", "problems"] as const;

const SKELETON_KEYS = ["sk-1", "sk-2", "sk-3", "sk-4", "sk-5", "sk-6", "sk-7", "sk-8"];

export function JobsPage({
  search,
  onSearchChange,
}: {
  search: JobsSearch;
  onSearchChange: (next: JobsSearch) => void;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const live = useConnectionState() === "live";

  const jobsQuery = useQuery({
    ...jobsQueryOptions,
    staleTime: liveStaleTime(live),
    refetchOnWindowFocus: !live,
  });
  const providersQuery = useQuery(providersQueryOptions);
  const meQuery = useQuery(meQueryOptions);

  const jobs = jobsQuery.data?.jobs ?? [];
  const providers: ProviderWithStale[] = providersQuery.data?.providers ?? [];
  const providerNames = new Map(providers.map((p) => [p.id, p.displayName]));

  const counts = groupCounts(jobs);
  const filtered = filterJobs(jobs, {
    q: search.q ?? "",
    group: search.group,
    provider: search.provider ?? null,
  });
  const sorted = sortJobs(filtered, search.sort, search.dir, providerNames);

  const [renaming, setRenaming] = useState<Job | null>(null);
  const [releasing, setReleasing] = useState<Job | null>(null);

  const setGroup = (group: (typeof GROUPS)[number]) => {
    onSearchChange({ ...search, group });
  };

  const setProvider = (providerId: string | null) => {
    const next = { ...search };
    if (providerId === null) {
      delete next.provider;
    } else {
      next.provider = providerId;
    }
    onSearchChange(next);
  };

  const onSortChange = (key: JobSortKey) => {
    onSearchChange(
      search.sort === key
        ? { ...search, dir: search.dir === "asc" ? "desc" : "asc" }
        : { ...search, sort: key, dir: "desc" },
    );
  };

  const handleCopyAll = async (job: Job) => {
    try {
      const links = await issueAllLinks(queryClient, job.id);
      await copyText(links.map((link) => link.url).join("\n"));
      toast.success("Links copied.");
    } catch (error) {
      if (isApiError(error) && error.slug === "job-not-found") {
        queryClient.removeQueries({ queryKey: keys.job(job.id) });
      }
      toast.error(describeError(error).title, { duration: 10_000 });
    }
  };

  const refreshJobs = () => void queryClient.invalidateQueries({ queryKey: keys.jobs });

  const staleNames = providers
    .filter((p) => p.stale === true && jobs.some((job) => job.provider === p.id))
    .map((p) => p.displayName);
  const listStale = jobsQuery.data?.stale === true;
  const showStale = listStale || staleNames.length > 0;
  const bannerNames =
    staleNames.length > 0
      ? staleNames
      : distinctProviders(jobs).map((id) => providerNames.get(id) ?? id);

  if (jobsQuery.isPending) {
    return (
      <div className="space-y-1 p-6">
        {SKELETON_KEYS.map((key) => (
          <Skeleton key={key} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  if (jobsQuery.isError) {
    return (
      <div className="p-6">
        <ErrorPanel error={jobsQuery.error} onRetry={() => void jobsQuery.refetch()} />
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between gap-3 px-6 pt-4 pb-2">
        <h1 className="text-base font-semibold">Jobs</h1>
        <div className="flex items-center gap-2">
          <RefreshButton jobs={jobs} providers={providers} onRefreshed={refreshJobs} />
          <Button asChild size="sm">
            <Link to="/add" search={{}}>
              + Add job
            </Link>
          </Button>
        </div>
      </div>

      {showStale ? (
        <div
          role="alert"
          className="bg-state-warning/10 text-state-warning border-y px-6 py-2 text-sm"
        >
          Not up to date: debroxy couldn&apos;t reach {bannerNames.join(", ")} just now. Showing the
          last known state.
        </div>
      ) : null}

      <div className="flex flex-wrap items-center gap-2 px-6 py-2">
        {GROUPS.map((group) => (
          <button
            key={group}
            type="button"
            onClick={() => setGroup(group)}
            className={cn(
              "rounded-full border px-3 py-1 text-xs font-medium capitalize",
              search.group === group
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:bg-accent",
            )}
          >
            {group} {counts[group]}
          </button>
        ))}
        <Select
          value={search.provider ?? "all"}
          onValueChange={(value) => setProvider(value === "all" ? null : value)}
        >
          <SelectTrigger size="sm" className="ml-2" aria-label="Provider">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All providers</SelectItem>
            {providers.map((provider) => (
              <SelectItem key={provider.id} value={provider.id}>
                {provider.displayName}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {jobs.length === 0 ? (
        <div className="flex flex-col items-center gap-3 p-12 text-center">
          <p className="text-base font-semibold">No jobs yet</p>
          <p className="text-muted-foreground text-sm">
            Add a magnet link, a .torrent file or a web link to get started.
          </p>
          <Button asChild size="sm">
            <Link to="/add" search={{}}>
              Add job
            </Link>
          </Button>
        </div>
      ) : sorted.length === 0 ? (
        <div className="flex flex-col items-center gap-3 p-12 text-center">
          <p className="text-base font-semibold">No jobs match these filters.</p>
          <Button variant="outline" size="sm" onClick={() => onSearchChange(defaultJobsSearch)}>
            Clear filters
          </Button>
        </div>
      ) : (
        <JobTable
          jobs={sorted}
          providers={providers}
          sort={search.sort}
          dir={search.dir}
          onSortChange={onSortChange}
          onOpen={(job) =>
            void navigate({
              to: "/jobs/$jobId",
              params: { jobId: job.id },
              search: { tab: "files" },
            })
          }
          onCopyAll={(job) => void handleCopyAll(job)}
          onRename={setRenaming}
          onRelease={setReleasing}
        />
      )}

      <div className="text-muted-foreground mt-auto border-t px-6 py-2 text-xs">
        {jobs.length} jobs · {counts.active} active · {meQuery.data?.quota.slotsUsed ?? 0} of{" "}
        {meQuery.data?.quota.maxActiveJobs ?? 0} slots used
      </div>

      {renaming !== null ? (
        <RenameDialog job={renaming} open onOpenChange={(open) => !open && setRenaming(null)} />
      ) : null}
      {releasing !== null ? (
        <ReleaseDialog
          job={releasing}
          providerName={providerNames.get(releasing.provider) ?? releasing.provider}
          open
          onOpenChange={(open) => !open && setReleasing(null)}
        />
      ) : null}
    </div>
  );
}

function RefreshButton({
  jobs,
  providers,
  onRefreshed,
}: {
  jobs: Job[];
  providers: Provider[];
  onRefreshed: () => void;
}) {
  const queryClient = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [rateLimitedUntil, setRateLimitedUntil] = useState<number | null>(null);
  const remaining = useCountdown(rateLimitedUntil);
  const enabled = providers.filter((provider) => provider.enabled);

  const refreshOne = async (provider: Provider) => {
    try {
      const result = await refreshProvider(provider.id);
      if (result.stale) {
        toast.warning(
          `${provider.displayName} couldn't be reached. Showing the last known state.`,
          { duration: 10_000 },
        );
      } else {
        toast.success(`${provider.displayName} refreshed.`);
      }
      onRefreshed();
    } catch (error) {
      if (isApiError(error) && error.slug === "refresh-rate-exceeded") {
        setRateLimitedUntil(Date.now() + (error.retryAfter ?? 0) * 1000);
      } else if (isApiError(error) && error.slug === "provider-not-found") {
        void queryClient.invalidateQueries({ queryKey: keys.providers });
        toast.error(describeError(error).title, { duration: 10_000 });
      } else {
        toast.error(describeError(error).title, { duration: 10_000 });
      }
    }
  };

  const refreshAll = async () => {
    setBusy(true);
    for (const id of distinctProviders(jobs)) {
      const provider = providers.find((entry) => entry.id === id);
      if (provider?.enabled) {
        await refreshOne(provider);
      }
    }
    setBusy(false);
  };

  return (
    <div className="flex">
      <Button
        variant="outline"
        size="sm"
        className="rounded-r-none"
        disabled={busy || remaining > 0}
        onClick={() => void refreshAll()}
      >
        <RefreshCw className={cn("size-3.5", busy && "animate-spin")} aria-hidden="true" />
        {remaining > 0 ? `Try again in ${remaining}s` : "Refresh"}
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="outline"
            size="sm"
            className="rounded-l-none border-l-0 px-2"
            aria-label="Refresh one provider"
          >
            <ChevronDown className="size-3.5" aria-hidden="true" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {enabled.map((provider) => (
            <DropdownMenuItem key={provider.id} onSelect={() => void refreshOne(provider)}>
              Refresh {provider.displayName}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
