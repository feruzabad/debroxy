import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useAdminConnectionState, useAdminEventStream } from "@/api/live";
import type { Job } from "@/api/queries/jobs";
import { allJobsQueryOptions, liveStaleTime, providersQueryOptions } from "@/api/queries/jobs";
import { meQueryOptions } from "@/api/queries/me";
import type { ProviderWithStale } from "@/api/types-rev13";
import { ErrorPanel } from "@/components/app/error-panel";
import { JobTable } from "@/components/app/job-table";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { AdminJobDetail } from "@/features/admin-jobs/admin-job-detail";
import { AdminReleaseDialog } from "@/features/admin-jobs/admin-release-dialog";
import { filterJobs, groupCounts, type JobSortKey, sortJobs } from "@/lib/jobs";
import { defaultJobsSearch, type JobsSearch } from "@/lib/search";
import { cn } from "@/lib/utils";

export type AdminJobsSearch = JobsSearch & { owner?: string | undefined };

const GROUPS = ["all", "active", "ready", "problems"] as const;
const SKELETON_KEYS = ["sk-1", "sk-2", "sk-3", "sk-4", "sk-5", "sk-6", "sk-7", "sk-8"];

/** The admin all-jobs page (§5.12): JobTable with owner column, kept live by the `scope=all` stream. */
export function AdminJobsPage({
  search,
  onSearchChange,
}: {
  search: AdminJobsSearch;
  onSearchChange: (next: AdminJobsSearch) => void;
}) {
  const live = useAdminConnectionState() === "live";
  useAdminEventStream();
  const navigate = useNavigate();

  const jobsQuery = useQuery({
    ...allJobsQueryOptions,
    staleTime: liveStaleTime(live),
    refetchOnWindowFocus: !live,
  });
  const providersQuery = useQuery(providersQueryOptions);
  const meQuery = useQuery(meQueryOptions);
  const ownUsername = meQuery.data?.user.username;

  const jobs = jobsQuery.data?.jobs ?? [];
  const providers: ProviderWithStale[] = providersQuery.data?.providers ?? [];
  const providerNames = new Map(providers.map((p) => [p.id, p.displayName]));

  const ownerFiltered =
    search.owner !== undefined ? jobs.filter((job) => job.ownerUsername === search.owner) : jobs;
  const counts = groupCounts(ownerFiltered);
  const filtered = filterJobs(ownerFiltered, {
    q: search.q ?? "",
    group: search.group,
    provider: search.provider ?? null,
  });
  const sorted = sortJobs(filtered, search.sort, search.dir, providerNames);

  const [expandedId, setExpandedId] = useState<string | null>(null);
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

  const clearOwner = () => {
    const next = { ...search };
    delete next.owner;
    onSearchChange(next);
  };

  const onSortChange = (key: JobSortKey) => {
    onSearchChange(
      search.sort === key
        ? { ...search, dir: search.dir === "asc" ? "desc" : "asc" }
        : { ...search, sort: key, dir: "desc" },
    );
  };

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
        <h1 className="text-base font-semibold">All jobs</h1>
        {search.owner !== undefined ? (
          <button
            type="button"
            className="text-muted-foreground text-sm hover:underline"
            onClick={clearOwner}
          >
            Showing {search.owner}&apos;s jobs · clear
          </button>
        ) : null}
      </div>

      {jobsQuery.data?.stale === true ? (
        <div
          role="alert"
          className="bg-state-warning/10 text-state-warning border-y px-6 py-2 text-sm"
        >
          Not up to date: debroxy couldn&apos;t reach the provider just now. Showing the last known
          state.
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
          <SelectTrigger size="sm" className="ml-2">
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
          admin
          ownUsername={ownUsername}
          expandedId={expandedId}
          onToggleExpanded={(job) => setExpandedId((id) => (id === job.id ? null : job.id))}
          renderExpanded={(job) => (
            <AdminJobDetail
              job={job}
              providerName={providerNames.get(job.provider) ?? job.provider}
            />
          )}
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
          onCopyAll={() => {}}
          onRename={() => {}}
          onRelease={setReleasing}
        />
      )}

      {releasing !== null ? (
        <AdminReleaseDialog
          job={releasing}
          ownUsername={ownUsername}
          open
          onOpenChange={(open) => !open && setReleasing(null)}
          onReleased={(jobId) => {
            setExpandedId((id) => (id === jobId ? null : id));
          }}
        />
      ) : null}
    </div>
  );
}
