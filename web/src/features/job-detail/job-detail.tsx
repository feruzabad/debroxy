import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { isApiError } from "@/api/errors";
import { removeJob } from "@/api/events";
import { keys } from "@/api/keys";
import { useConnectionState, useReleasedEntry } from "@/api/live";
import type { JobList } from "@/api/queries/jobs";
import { jobQueryOptions, liveStaleTime, providersQueryOptions } from "@/api/queries/jobs";
import { ErrorPanel } from "@/components/app/error-panel";
import { ReleaseDialog } from "@/components/app/release-dialog";
import { RenameDialog } from "@/components/app/rename-dialog";
import { StateBadge } from "@/components/app/state-badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { FileEntry } from "@/lib/filetree";
import { defaultHistorySearch, defaultJobsSearch } from "@/lib/search";
import { stateInfo } from "@/lib/states";
import { DetailsTab } from "./details-tab";
import { FilesTab } from "./files-tab";
import { JobHeader } from "./job-header";

export type JobTab = "files" | "details";

const SKELETON_KEYS = ["sk-1", "sk-2", "sk-3", "sk-4", "sk-5", "sk-6"];

/** The job detail page (§5.6): header, Files and Details tabs. */
export function JobDetail({
  jobId,
  tab,
  onTabChange,
  onPlay,
  onReleased,
}: {
  jobId: string;
  tab: JobTab;
  onTabChange: (tab: JobTab) => void;
  onPlay: (file: FileEntry) => void;
  onReleased: () => void;
}) {
  const queryClient = useQueryClient();
  const live = useConnectionState() === "live";
  const jobQuery = useQuery({ ...jobQueryOptions(jobId), staleTime: liveStaleTime(live) });
  const providersQuery = useQuery(providersQueryOptions);

  const [renaming, setRenaming] = useState(false);
  const [releasing, setReleasing] = useState(false);

  // A polite live region announcing state changes (§10: "{name}: Ready").
  const jobData = jobQuery.data;
  const prevStateRef = useRef<string | null>(null);
  const [announcement, setAnnouncement] = useState("");

  useEffect(() => {
    if (jobData === undefined) {
      return;
    }
    const label = stateInfo(jobData.state).label;
    if (prevStateRef.current !== null && prevStateRef.current !== jobData.state) {
      setAnnouncement(`${jobData.displayName}: ${label}`);
    }
    prevStateRef.current = jobData.state;
  }, [jobData]);

  const releasedEntry = useReleasedEntry(jobId);

  useEffect(() => {
    if (jobQuery.isError && isApiError(jobQuery.error) && jobQuery.error.slug === "job-not-found") {
      queryClient.setQueryData<JobList>(keys.jobs, (list) =>
        list === undefined ? list : { ...list, jobs: removeJob(list.jobs, jobId) },
      );
    }
  }, [jobQuery.isError, jobQuery.error, jobId, queryClient]);

  if (releasedEntry !== undefined) {
    const providerName =
      providersQuery.data?.providers.find((provider) => provider.id === releasedEntry.provider)
        ?.displayName ?? releasedEntry.provider;
    return (
      <div className="flex flex-col items-start gap-3 p-6">
        <h1 className="text-base font-semibold">This job was released.</h1>
        <StateBadge
          info={stateInfo(releasedEntry.finalState, {
            provider: providerName,
            failureReason: releasedEntry.failureReason ?? null,
          })}
        />
        <Link
          to="/history"
          search={defaultHistorySearch}
          className="text-primary text-sm hover:underline"
        >
          See history
        </Link>
      </div>
    );
  }

  if (jobQuery.isPending) {
    return (
      <div className="space-y-1 p-6">
        {SKELETON_KEYS.map((key) => (
          <Skeleton key={key} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  if (jobQuery.isError) {
    if (isApiError(jobQuery.error) && jobQuery.error.slug === "job-not-found") {
      return (
        <div className="flex flex-col items-start gap-3 p-6">
          <h1 className="text-base font-semibold">
            This job doesn&apos;t exist or isn&apos;t yours.
          </h1>
          <Link to="/" search={defaultJobsSearch} className="text-primary text-sm hover:underline">
            Back to jobs
          </Link>
        </div>
      );
    }
    return (
      <div className="p-6">
        <ErrorPanel error={jobQuery.error} onRetry={() => void jobQuery.refetch()} />
      </div>
    );
  }

  const job = jobQuery.data;
  const providerName =
    providersQuery.data?.providers.find((provider) => provider.id === job.provider)?.displayName ??
    job.provider;

  return (
    <div className="flex h-full flex-col">
      <div role="status" aria-live="polite" className="sr-only">
        {announcement}
      </div>
      <JobHeader
        job={job}
        providerName={providerName}
        stale={job.stale === true}
        onRename={() => setRenaming(true)}
        onRelease={() => setReleasing(true)}
      />
      <Tabs
        value={tab}
        onValueChange={(value) => onTabChange(value === "files" ? "files" : "details")}
      >
        <TabsList className="mx-6 mt-1">
          <TabsTrigger value="files">Files ({job.files.length})</TabsTrigger>
          <TabsTrigger value="details">Details</TabsTrigger>
        </TabsList>
        <TabsContent value="files">
          <FilesTab job={job} providerName={providerName} onPlay={onPlay} />
        </TabsContent>
        <TabsContent value="details">
          <DetailsTab job={job} providerName={providerName} />
        </TabsContent>
      </Tabs>

      {renaming ? (
        <RenameDialog job={job} open onOpenChange={(open) => !open && setRenaming(false)} />
      ) : null}
      {releasing ? (
        <ReleaseDialog
          job={job}
          providerName={providerName}
          open
          onOpenChange={(open) => !open && setReleasing(false)}
          onReleased={onReleased}
        />
      ) : null}
    </div>
  );
}
