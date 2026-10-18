import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowLeft, ChevronDown, Copy, Pencil } from "lucide-react";
import type { Job } from "@/api/queries/jobs";
import { jobStateInfo, StateIcon } from "@/components/app/state-badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatExactTime, formatRelativeTime, formatSize } from "@/lib/format";
import { defaultJobsSearch } from "@/lib/search";
import { expiryInfo } from "@/lib/states";
import { cn } from "@/lib/utils";
import { copyAllLinks, downloadAria2, downloadLinkList } from "./links";

function CopyAllMenu({ job }: { job: Job }) {
  const queryClient = useQueryClient();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" disabled={job.state !== "ready"}>
          <Copy className="size-3.5" aria-hidden="true" />
          Copy all links
          <ChevronDown className="size-3.5" aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={() => void copyAllLinks(queryClient, job.id)}>
          Copy all links
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => void downloadLinkList(queryClient, job)}>
          Download link list (.txt)
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => void downloadAria2(queryClient, job)}>
          Download for aria2 (.aria2)
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => void downloadLinkList(queryClient, job)}>
          Download for JDownloader (.txt)
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** The job detail header: back link, title, meta line and actions (§5.6.1–5.6.2). */
export function JobHeader({
  job,
  providerName,
  stale,
  onRename,
  onRelease,
}: {
  job: Job;
  providerName: string;
  stale: boolean;
  onRename: () => void;
  onRelease: () => void;
}) {
  const info = jobStateInfo(job, providerName);
  const expiry = expiryInfo(job.expiresAt);
  const size = job.sizeBytes === 0 ? "0 B" : formatSize(job.sizeBytes);

  return (
    <div>
      <div className="px-6 pt-4 pb-2">
        <Link
          to="/"
          search={defaultJobsSearch}
          className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-sm"
        >
          <ArrowLeft className="size-3.5" aria-hidden="true" />
          Jobs
        </Link>
        <div className="mt-1 flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h1 className="truncate text-base font-semibold">{job.displayName}</h1>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Rename ${job.displayName}`}
                    onClick={onRename}
                  >
                    <Pencil className="size-3.5" aria-hidden="true" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Rename</TooltipContent>
              </Tooltip>
            </div>
            <div className="text-muted-foreground mt-1 flex flex-wrap items-center gap-x-1.5 text-sm">
              <StateIcon info={info} />
              <span className="font-medium text-foreground">{info.label}</span>
              <span>·</span>
              <span>{providerName}</span>
              <span>·</span>
              <span className="tabular-nums">{size}</span>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span>
                    · added{" "}
                    <span className="tabular-nums">{formatRelativeTime(job.createdAt)}</span>
                  </span>
                </TooltipTrigger>
                <TooltipContent>{formatExactTime(job.createdAt)}</TooltipContent>
              </Tooltip>
              {expiry.text !== null ? (
                <>
                  <span>·</span>
                  <span
                    className={cn(
                      "tabular-nums",
                      expiry.level === "amber" && "text-state-warning",
                      (expiry.level === "red" || expiry.level === "expired") && "text-state-failed",
                    )}
                  >
                    {expiry.text}
                  </span>
                </>
              ) : null}
              {job.shared ? (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span className="text-muted-foreground">· ⇄ shared</span>
                  </TooltipTrigger>
                  <TooltipContent>
                    Others also use this content. It stays at the provider until everyone releases
                    it.
                  </TooltipContent>
                </Tooltip>
              ) : null}
              <span>·</span>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className={cn(!job.slotCharged && "opacity-70")}>
                    {job.slotCharged ? "counts toward quota" : "doesn't count toward your quota"}
                  </span>
                </TooltipTrigger>
                <TooltipContent>
                  {job.slotCharged
                    ? "This uses one of your slots."
                    : "doesn't count toward your quota"}
                </TooltipContent>
              </Tooltip>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <CopyAllMenu job={job} />
            <Button variant="outline" size="sm" onClick={onRelease}>
              Release
            </Button>
          </div>
        </div>
      </div>
      {stale ? (
        <div
          role="alert"
          className="bg-state-warning/10 text-state-warning border-y px-6 py-2 text-sm"
        >
          Not up to date: debroxy couldn&apos;t reach {providerName} just now. Showing the last
          known state.
        </div>
      ) : null}
    </div>
  );
}
