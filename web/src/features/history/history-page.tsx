import { useQuery } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowUp,
  FileText,
  Globe,
  type LucideIcon,
  Magnet,
  Newspaper,
} from "lucide-react";
import { historyQueryOptions, providersQueryOptions } from "@/api/queries/jobs";
import type { HistoryEntry } from "@/api/schemas";
import { ErrorPanel } from "@/components/app/error-panel";
import { StateBadge } from "@/components/app/state-badge";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatExactTime, formatRelativeTime } from "@/lib/format";
import type { HistorySearch } from "@/lib/search";
import { failureText, stateInfo, stateOrderIndex } from "@/lib/states";

const SKELETON_KEYS = ["sk-1", "sk-2", "sk-3", "sk-4", "sk-5", "sk-6", "sk-7", "sk-8"];

const collator = new Intl.Collator(undefined, { sensitivity: "base", numeric: true });

const KIND_ICONS: Record<string, LucideIcon> = {
  torrent: Magnet,
  web: Globe,
  nzb: Newspaper,
};

function kindIcon(kind: string) {
  return KIND_ICONS[kind] ?? FileText;
}

type HistorySortKey = HistorySearch["sort"];

function sortEntries(
  entries: HistoryEntry[],
  sort: HistorySortKey,
  dir: "asc" | "desc",
  providerNames: ReadonlyMap<string, string>,
): HistoryEntry[] {
  return [...entries].sort((a, b) => {
    let cmp = 0;
    switch (sort) {
      case "name":
        cmp = collator.compare(a.displayName, b.displayName);
        break;
      case "provider":
        cmp = collator.compare(
          providerNames.get(a.provider) ?? a.provider,
          providerNames.get(b.provider) ?? b.provider,
        );
        break;
      case "finalState":
        cmp = stateOrderIndex(a.finalState) - stateOrderIndex(b.finalState);
        break;
      case "claimed":
        cmp = a.claimedAt.localeCompare(b.claimedAt);
        break;
      case "released":
        cmp = a.releasedAt.localeCompare(b.releasedAt);
        break;
    }
    if (cmp === 0) {
      cmp = b.releasedAt.localeCompare(a.releasedAt);
    }
    return dir === "asc" ? cmp : -cmp;
  });
}

function SortHeader({
  label,
  column,
  sort,
  dir,
  onSortChange,
}: {
  label: string;
  column: HistorySortKey;
  sort: HistorySortKey;
  dir: "asc" | "desc";
  onSortChange: (sort: HistorySortKey) => void;
}) {
  const active = sort === column;
  return (
    <TableHead aria-sort={active ? (dir === "asc" ? "ascending" : "descending") : "none"}>
      <button
        type="button"
        className="hover:text-foreground inline-flex items-center gap-1"
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

/** The history page (§5.8): a read-only table with URL search and sort. */
export function HistoryPage({
  search,
  onSearchChange,
}: {
  search: HistorySearch;
  onSearchChange: (next: HistorySearch) => void;
}) {
  const historyQuery = useQuery(historyQueryOptions);
  const providersQuery = useQuery(providersQueryOptions);

  const entries = historyQuery.data?.entries ?? [];
  const providerNames = new Map(
    (providersQuery.data?.providers ?? []).map((provider) => [provider.id, provider.displayName]),
  );

  const needle = search.q?.trim().toLowerCase() ?? "";
  const filtered = entries.filter(
    (entry) => needle === "" || entry.displayName.toLowerCase().includes(needle),
  );
  const sorted = sortEntries(filtered, search.sort, search.dir, providerNames);

  const setSort = (sort: HistorySortKey) => {
    onSearchChange(
      search.sort === sort
        ? { ...search, dir: search.dir === "asc" ? "desc" : "asc" }
        : { ...search, sort, dir: "desc" },
    );
  };

  const setQuery = (q: string) => {
    const next = { ...search };
    if (q !== "") {
      next.q = q;
    } else {
      delete next.q;
    }
    onSearchChange(next);
  };

  if (historyQuery.isPending) {
    return (
      <div className="space-y-1 p-6">
        {SKELETON_KEYS.map((key) => (
          <Skeleton key={key} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  if (historyQuery.isError) {
    return (
      <div className="p-6">
        <ErrorPanel error={historyQuery.error} onRetry={() => void historyQuery.refetch()} />
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between gap-3 px-6 pt-4 pb-2">
        <h1 className="text-base font-semibold">History</h1>
        <Input
          value={search.q ?? ""}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="filter by name"
          aria-label="Filter history by name"
          className="h-7 max-w-xs"
        />
      </div>

      {entries.length === 0 ? (
        <div className="flex flex-col items-center gap-3 p-12 text-center">
          <p className="text-base font-semibold">Nothing released yet.</p>
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <SortHeader
                label="Name"
                column="name"
                sort={search.sort}
                dir={search.dir}
                onSortChange={setSort}
              />
              <SortHeader
                label="Provider"
                column="provider"
                sort={search.sort}
                dir={search.dir}
                onSortChange={setSort}
              />
              <SortHeader
                label="Ended as"
                column="finalState"
                sort={search.sort}
                dir={search.dir}
                onSortChange={setSort}
              />
              <SortHeader
                label="Added"
                column="claimed"
                sort={search.sort}
                dir={search.dir}
                onSortChange={setSort}
              />
              <SortHeader
                label="Released"
                column="released"
                sort={search.sort}
                dir={search.dir}
                onSortChange={setSort}
              />
            </TableRow>
          </TableHeader>
          <TableBody>
            {sorted.map((entry) => {
              const providerName = providerNames.get(entry.provider) ?? entry.provider;
              const KindIcon = kindIcon(entry.kind);
              const failed = entry.finalState === "failed" || entry.finalState === "gone";
              return (
                <TableRow key={entry.jobId}>
                  <TableCell className="max-w-72">
                    <div className="flex items-center gap-2">
                      <KindIcon
                        className="text-muted-foreground size-3.5 shrink-0"
                        aria-hidden="true"
                      />
                      <span className="truncate">{entry.displayName}</span>
                    </div>
                    {entry.name !== entry.displayName ? (
                      <span className="text-muted-foreground block truncate text-xs">
                        {entry.name}
                      </span>
                    ) : null}
                  </TableCell>
                  <TableCell>{providerName}</TableCell>
                  <TableCell>
                    <StateBadge info={stateInfo(entry.finalState, { provider: providerName })} />
                    {failed ? (
                      <span className="text-state-failed block truncate text-xs">
                        {failureText(entry.failureReason ?? null, providerName).text}
                      </span>
                    ) : null}
                  </TableCell>
                  <TableCell className="text-muted-foreground tabular-nums">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span>{formatRelativeTime(entry.claimedAt)}</span>
                      </TooltipTrigger>
                      <TooltipContent>{formatExactTime(entry.claimedAt)}</TooltipContent>
                    </Tooltip>
                  </TableCell>
                  <TableCell className="text-muted-foreground tabular-nums">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <span>{formatRelativeTime(entry.releasedAt)}</span>
                      </TooltipTrigger>
                      <TooltipContent>{formatExactTime(entry.releasedAt)}</TooltipContent>
                    </Tooltip>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
