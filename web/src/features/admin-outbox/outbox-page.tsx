import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { outboxMessagesQueryOptions } from "@/api/queries/admin";
import { ErrorPanel } from "@/components/app/error-panel";
import { Button } from "@/components/ui/button";
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
import { formatExactTime, formatRelativeTime } from "@/lib/format";

const SKELETON_KEYS = ["sk-1", "sk-2", "sk-3", "sk-4", "sk-5", "sk-6", "sk-7", "sk-8"];

/** The admin stuck-deletions page (§5.15). */
export function OutboxPage() {
  const messagesQuery = useQuery(outboxMessagesQueryOptions);
  const messages = messagesQuery.data?.messages ?? [];

  if (messagesQuery.isPending) {
    return (
      <div className="space-y-1 p-6">
        {SKELETON_KEYS.map((key) => (
          <Skeleton key={key} className="h-10 w-full" />
        ))}
      </div>
    );
  }

  if (messagesQuery.isError) {
    return (
      <div className="p-6">
        <ErrorPanel error={messagesQuery.error} onRetry={() => void messagesQuery.refetch()} />
      </div>
    );
  }

  return (
    <div className="space-y-4 p-6">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-base font-semibold">Stuck deletions</h1>
        <Button variant="outline" size="sm" onClick={() => void messagesQuery.refetch()}>
          Reload
        </Button>
      </div>

      <p className="text-muted-foreground text-sm">
        debroxy tried to delete these items at the provider and gave up. They still exist there and
        count against the account&apos;s limits. Delete them in the provider&apos;s website.
      </p>

      {messages.length === 0 ? (
        <p className="text-muted-foreground text-sm">No stuck deletions.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Kind</TableHead>
              <TableHead>Remote item</TableHead>
              <TableHead>Attempts</TableHead>
              <TableHead>Last error</TableHead>
              <TableHead>Created</TableHead>
              <TableHead>Gave up</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {messages.map((message) => (
              <TableRow key={message.id}>
                <TableCell className="tabular-nums">{message.id}</TableCell>
                <TableCell className="font-mono">{message.kind}</TableCell>
                <TableCell className="font-mono">
                  <span className="mr-1">{message.remoteItemId}</span>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label="Copy remote item ID"
                    onClick={() => {
                      void copyText(message.remoteItemId);
                      toast.success("Copied.");
                    }}
                  >
                    ⧉
                  </Button>
                </TableCell>
                <TableCell className="tabular-nums">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span>{message.attempts}</span>
                    </TooltipTrigger>
                    <TooltipContent>
                      debroxy gave up deleting this item after {message.attempts} attempts. It still
                      exists at the provider.
                    </TooltipContent>
                  </Tooltip>
                </TableCell>
                <TableCell className="max-w-md font-mono text-xs whitespace-normal">
                  {message.lastError}
                </TableCell>
                <TableCell className="text-muted-foreground tabular-nums">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span>{formatRelativeTime(message.createdAt)}</span>
                    </TooltipTrigger>
                    <TooltipContent>{formatExactTime(message.createdAt)}</TooltipContent>
                  </Tooltip>
                </TableCell>
                <TableCell className="text-muted-foreground tabular-nums">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span>{formatRelativeTime(message.parkedAt)}</span>
                    </TooltipTrigger>
                    <TooltipContent>{formatExactTime(message.parkedAt)}</TooltipContent>
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
