import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { unmanagedItemsQueryOptions } from "@/api/queries/admin";
import { providersQueryOptions } from "@/api/queries/jobs";
import { ErrorPanel } from "@/components/app/error-panel";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
import { copyText } from "@/lib/browser";
import { formatExactTime, formatRelativeTime, formatSize } from "@/lib/format";

const SKELETON_KEYS = ["sk-1", "sk-2", "sk-3", "sk-4", "sk-5", "sk-6", "sk-7", "sk-8"];

/** The admin unmanaged-items page (§5.14). */
export function UnmanagedPage({
  search,
  onSearchChange,
}: {
  search: { provider?: string | undefined };
  onSearchChange: (next: { provider?: string | undefined }) => void;
}) {
  const itemsQuery = useQuery(unmanagedItemsQueryOptions);
  const providersQuery = useQuery(providersQueryOptions);

  const items = itemsQuery.data?.items ?? [];
  const providerNames = new Map(
    (providersQuery.data?.providers ?? []).map((provider) => [provider.id, provider.displayName]),
  );
  const providerIds = [...new Set(items.map((item) => item.provider))];

  const filtered = search.provider
    ? items.filter((item) => item.provider === search.provider)
    : items;

  const setProvider = (providerId: string | null) => {
    const next = { ...search };
    if (providerId === null) {
      delete next.provider;
    } else {
      next.provider = providerId;
    }
    onSearchChange(next);
  };

  if (itemsQuery.isPending) {
    return (
      <div className="space-y-1 p-6">
        {SKELETON_KEYS.map((key) => (
          <Skeleton key={key} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  if (itemsQuery.isError) {
    return (
      <div className="p-6">
        <ErrorPanel error={itemsQuery.error} onRetry={() => void itemsQuery.refetch()} />
      </div>
    );
  }

  return (
    <div className="space-y-4 p-6">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-base font-semibold">Unmanaged items</h1>
        <div className="flex items-center gap-2">
          <Select
            value={search.provider ?? "all"}
            onValueChange={(value) => setProvider(value === "all" ? null : value)}
          >
            <SelectTrigger size="sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All providers</SelectItem>
              {providerIds.map((providerId) => (
                <SelectItem key={providerId} value={providerId}>
                  {providerNames.get(providerId) ?? providerId}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button variant="outline" size="sm" onClick={() => void itemsQuery.refetch()}>
            Reload
          </Button>
        </div>
      </div>

      <p className="text-muted-foreground text-sm">
        These items are on the provider accounts but weren&apos;t added through debroxy. debroxy
        never deletes them. If someone uses the account directly, they show up here. Remove them in
        the provider&apos;s own website if you don&apos;t need them.
      </p>

      {items.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          Nothing unmanaged. The provider accounts contain only what debroxy added.
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Provider</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Kind</TableHead>
              <TableHead>Size</TableHead>
              <TableHead>Provider state</TableHead>
              <TableHead>Provider item ID</TableHead>
              <TableHead>First seen</TableHead>
              <TableHead>Last seen</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.map((item) => (
              <TableRow key={`${item.provider}:${item.providerItemId}`}>
                <TableCell>{providerNames.get(item.provider) ?? item.provider}</TableCell>
                <TableCell className="max-w-56 truncate">{item.name}</TableCell>
                <TableCell>{item.kind}</TableCell>
                <TableCell className="tabular-nums">{formatSize(item.sizeBytes)}</TableCell>
                <TableCell className="font-mono">{item.state}</TableCell>
                <TableCell className="font-mono">
                  <span className="mr-1">{item.providerItemId}</span>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label="Copy provider item ID"
                    onClick={() => {
                      void copyText(item.providerItemId);
                      toast.success("Copied.");
                    }}
                  >
                    ⧉
                  </Button>
                </TableCell>
                <TableCell className="text-muted-foreground tabular-nums">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span>{formatRelativeTime(item.firstSeenAt)}</span>
                    </TooltipTrigger>
                    <TooltipContent>{formatExactTime(item.firstSeenAt)}</TooltipContent>
                  </Tooltip>
                </TableCell>
                <TableCell className="text-muted-foreground tabular-nums">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span>{formatRelativeTime(item.lastSeenAt)}</span>
                    </TooltipTrigger>
                    <TooltipContent>{formatExactTime(item.lastSeenAt)}</TooltipContent>
                  </Tooltip>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
