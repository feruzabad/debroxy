import { Link } from "@tanstack/react-router";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, ArrowUp, Globe, MoreHorizontal } from "lucide-react";
import { Fragment, type ReactNode, useMemo, useRef } from "react";
import type { Job, Provider } from "@/api/queries/jobs";
import { ProgressCell } from "@/components/app/progress-cell";
import { jobStateInfo, StateBadge, StateIcon } from "@/components/app/state-badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatExactTime, formatRelativeTime, formatSize } from "@/lib/format";
import type { JobSortKey, SortDir } from "@/lib/jobs";
import { expiryInfo, sharedSlotInfo } from "@/lib/states";
import { cn } from "@/lib/utils";

interface JobTableProps {
  jobs: Job[];
  providers: Provider[];
  admin?: boolean;
  /** The current admin's username; decides whether an admin job is the admin's own (§5.12). */
  ownUsername?: string | undefined;
  /** The id of the admin job whose inline detail row is expanded. */
  expandedId?: string | null;
  onToggleExpanded?: (job: Job) => void;
  /** Renders the read-only inline detail row for an expanded job (§5.12). */
  renderExpanded?: (job: Job) => ReactNode;
  sort: JobSortKey;
  dir: SortDir;
  onSortChange: (sort: JobSortKey) => void;
  onOpen: (job: Job) => void;
  onCopyAll: (job: Job) => void;
  onRename: (job: Job) => void;
  onRelease: (job: Job) => void;
}

const VIRTUALIZE_THRESHOLD = 200;

function SortHeader({
  label,
  column,
  sort,
  dir,
  onSortChange,
  className,
}: {
  label: string;
  column: JobSortKey;
  sort: JobSortKey;
  dir: SortDir;
  onSortChange: (sort: JobSortKey) => void;
  className?: string;
}) {
  const active = sort === column;
  return (
    <TableHead
      className={className}
      aria-sort={active ? (dir === "asc" ? "ascending" : "descending") : "none"}
    >
      <button
        type="button"
        className="inline-flex items-center gap-1 hover:text-foreground"
        onClick={() => onSortChange(column)}
      >
        {label}
        {active ? (
          dir === "asc" ? (
            <ArrowUp className="size-3" aria-hidden="true" />
          ) : (
            <ArrowDown className="size-3" aria-hidden="true" />
          )
        ) : null}
      </button>
    </TableHead>
  );
}

/** The job list table (§5.3.3), shared by the jobs page and admin jobs. */
export function JobTable({
  jobs,
  providers,
  admin = false,
  ownUsername,
  expandedId = null,
  onToggleExpanded,
  renderExpanded,
  sort,
  dir,
  onSortChange,
  onOpen,
  onCopyAll,
  onRename,
  onRelease,
}: JobTableProps) {
  const providerNames = useMemo(() => {
    const map = new Map<string, string>();
    for (const provider of providers) {
      map.set(provider.id, provider.displayName);
    }
    return map;
  }, [providers]);

  const isOwn = (job: Job) =>
    admin && ownUsername !== undefined && job.ownerUsername === ownUsername;
  const expandable = (job: Job) => admin && !isOwn(job);

  const scrollRef = useRef<HTMLDivElement>(null);
  const virtualize = jobs.length > VIRTUALIZE_THRESHOLD && expandedId === null;
  const virtualizer = useVirtualizer({
    count: virtualize ? jobs.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 40,
    measureElement: (element) => element.getBoundingClientRect().height,
    overscan: 20,
  });

  const cells = (job: Job) => {
    const info = jobStateInfo(job, providerNames.get(job.provider) ?? job.provider);
    const badges = sharedSlotInfo(job.shared, job.slotCharged);
    const expiry = expiryInfo(job.expiresAt);
    const providerName = providerNames.get(job.provider) ?? job.provider;
    return (
      <>
        <TableCell className="w-8 px-2">
          <StateIcon info={info} />
        </TableCell>
        <TableCell className="max-w-72">
          <div className="flex items-center gap-2">
            {job.kind === "web" ? (
              <Globe className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
            ) : null}
            <Tooltip>
              <TooltipTrigger asChild>
                {isOwn(job) ? (
                  <Link
                    to="/jobs/$jobId"
                    params={{ jobId: job.id }}
                    search={{ tab: "files" }}
                    className="truncate text-foreground hover:underline"
                  >
                    {job.displayName}
                  </Link>
                ) : admin ? (
                  <span className="truncate text-foreground">{job.displayName}</span>
                ) : (
                  <Link
                    to="/jobs/$jobId"
                    params={{ jobId: job.id }}
                    search={{ tab: "files" }}
                    className="truncate text-foreground hover:underline"
                  >
                    {job.displayName}
                  </Link>
                )}
              </TooltipTrigger>
              <TooltipContent>{job.displayName}</TooltipContent>
            </Tooltip>
          </div>
          {badges.badges.length > 0 ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="text-muted-foreground block text-xs">
                  {badges.badges.join(" · ")}
                </span>
              </TooltipTrigger>
              {badges.tooltip !== null ? <TooltipContent>{badges.tooltip}</TooltipContent> : null}
            </Tooltip>
          ) : null}
        </TableCell>
        <TableCell>{providerName}</TableCell>
        <TableCell>
          <StateBadge info={info} />
        </TableCell>
        <TableCell>
          <ProgressCell job={job} provider={providerName} />
        </TableCell>
        <TableCell className="text-right tabular-nums">
          {job.sizeBytes === 0 ? "—" : formatSize(job.sizeBytes)}
        </TableCell>
        <TableCell
          className={cn(
            "tabular-nums",
            expiry.level === "amber" && "text-state-warning",
            (expiry.level === "red" || expiry.level === "expired") && "text-state-failed",
            expiry.level === "muted" && "text-muted-foreground",
          )}
        >
          {expiry.text ?? "—"}
        </TableCell>
        <TableCell className="text-muted-foreground tabular-nums">
          <Tooltip>
            <TooltipTrigger asChild>
              <span>{formatRelativeTime(job.createdAt)}</span>
            </TooltipTrigger>
            <TooltipContent>{formatExactTime(job.createdAt)}</TooltipContent>
          </Tooltip>
        </TableCell>
        {admin ? <TableCell>{job.ownerUsername ?? "—"}</TableCell> : null}
        <TableCell className="w-10 px-2 text-right">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon-sm" aria-label={`Actions for ${job.displayName}`}>
                <MoreHorizontal />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {expandable(job) ? null : (
                <DropdownMenuItem onSelect={() => onOpen(job)}>Open</DropdownMenuItem>
              )}
              {admin ? null : (
                <>
                  <DropdownMenuItem
                    onSelect={() => onCopyAll(job)}
                    disabled={job.state !== "ready"}
                  >
                    Copy all links
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => onRename(job)}>Rename…</DropdownMenuItem>
                </>
              )}
              <DropdownMenuItem onSelect={() => onRelease(job)} variant="destructive">
                Release…
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </TableCell>
      </>
    );
  };

  const header = (
    <TableHeader>
      <TableRow>
        <TableHead className="w-8 px-2">
          <span className="sr-only">Status</span>
        </TableHead>
        <SortHeader label="Name" column="name" sort={sort} dir={dir} onSortChange={onSortChange} />
        <SortHeader
          label="Provider"
          column="provider"
          sort={sort}
          dir={dir}
          onSortChange={onSortChange}
        />
        <SortHeader
          label="State"
          column="state"
          sort={sort}
          dir={dir}
          onSortChange={onSortChange}
        />
        <SortHeader
          label="Progress"
          column="progress"
          sort={sort}
          dir={dir}
          onSortChange={onSortChange}
        />
        <SortHeader
          label="Size"
          column="size"
          sort={sort}
          dir={dir}
          onSortChange={onSortChange}
          className="text-right"
        />
        <SortHeader
          label="Expires"
          column="expires"
          sort={sort}
          dir={dir}
          onSortChange={onSortChange}
        />
        <SortHeader
          label="Added"
          column="created"
          sort={sort}
          dir={dir}
          onSortChange={onSortChange}
        />
        {admin ? <TableHead>Owner</TableHead> : null}
        <TableHead className="w-10 px-2">
          <span className="sr-only">Actions</span>
        </TableHead>
      </TableRow>
    </TableHeader>
  );

  const columnCount = admin ? 10 : 9;

  // §5.3.3: rows are keyboard-focusable; Enter opens the job. For admin rows
  // that aren't the admin's own, Enter (or a click) expands the inline drawer.
  const interactionProps = (job: Job) => ({
    tabIndex: 0,
    ...(expandable(job) ? { onClick: () => onToggleExpanded?.(job) } : {}),
    onKeyDown: (event: React.KeyboardEvent<HTMLTableRowElement>) => {
      if (event.key === "Enter" && event.target === event.currentTarget) {
        if (expandable(job)) {
          onToggleExpanded?.(job);
        } else {
          onOpen(job);
        }
      }
    },
  });

  const detailRow = (job: Job) =>
    expandedId === job.id && renderExpanded !== undefined ? (
      <TableRow className="border-b hover:bg-transparent">
        <TableCell colSpan={columnCount} className="p-0">
          {renderExpanded(job)}
        </TableCell>
      </TableRow>
    ) : null;

  return (
    <div ref={scrollRef} className="max-h-[calc(100vh-13rem)] overflow-auto">
      <Table>
        {header}
        {virtualize ? (
          <TableBody className="relative" style={{ height: virtualizer.getTotalSize() }}>
            {virtualizer.getVirtualItems().map((item) => {
              const job = jobs[item.index];
              if (job === undefined) {
                return null;
              }
              return (
                <tr
                  key={job.id}
                  ref={virtualizer.measureElement}
                  data-index={item.index}
                  {...interactionProps(job)}
                  className={cn(
                    "absolute top-0 left-0 w-full border-b transition-colors hover:bg-muted/50 focus-visible:bg-muted/50 focus-visible:outline-none",
                    expandable(job) && "cursor-pointer",
                  )}
                >
                  {cells(job)}
                </tr>
              );
            })}
          </TableBody>
        ) : (
          <TableBody>
            {jobs.map((job) => (
              <Fragment key={job.id}>
                <TableRow
                  {...interactionProps(job)}
                  className={cn(
                    "focus-visible:bg-muted/50 focus-visible:outline-none",
                    expandable(job) && "cursor-pointer",
                  )}
                >
                  {cells(job)}
                </TableRow>
                {detailRow(job)}
              </Fragment>
            ))}
          </TableBody>
        )}
      </Table>
    </div>
  );
}
