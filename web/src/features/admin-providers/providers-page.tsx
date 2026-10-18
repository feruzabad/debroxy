import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { describeError, isApiError } from "@/api/errors";
import { keys } from "@/api/keys";
import { probeProviderCapabilities } from "@/api/mutations/admin";
import { refreshProvider } from "@/api/mutations/jobs";
import type { Provider } from "@/api/queries/jobs";
import { hostersQueryOptions, providersQueryOptions } from "@/api/queries/jobs";
import type { ProviderWithStale } from "@/api/types-rev13";
import { ErrorPanel } from "@/components/app/error-panel";
import { Badge } from "@/components/ui/badge";
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
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { capabilityLabel } from "@/lib/capabilities";
import { cn } from "@/lib/utils";

function stateLabel(state: string | undefined): string {
  switch (state) {
    case "available":
      return "available";
    case "unavailable":
      return "unavailable";
    case "unverified":
      return "not yet tested";
    default:
      return "—";
  }
}

function hasWebCapability(provider: Provider): boolean {
  const capability = provider.capabilities.find((entry) => entry.name === "SubmitWebURL");
  return capability?.enabled === true;
}

/** The admin providers page (§5.13). */
export function ProvidersPage({
  search,
  onSearchChange,
}: {
  search: { provider?: string | undefined };
  onSearchChange: (next: { provider?: string | undefined }) => void;
}) {
  const queryClient = useQueryClient();
  const providersQuery = useQuery(providersQueryOptions);
  const providers: ProviderWithStale[] = providersQuery.data?.providers ?? [];

  const selectedId = search.provider ?? providers[0]?.id ?? "";
  const selected = providers.find((provider) => provider.id === selectedId) ?? null;

  const [refreshing, setRefreshing] = useState(false);
  const [probing, setProbing] = useState(false);

  const hostersQuery = useQuery({
    ...hostersQueryOptions(selectedId),
    enabled: selected !== null && hasWebCapability(selected),
  });

  const setProvider = (providerId: string) => {
    const next = { ...search };
    if (providerId === providers[0]?.id) {
      delete next.provider;
    } else {
      next.provider = providerId;
    }
    onSearchChange(next);
  };

  if (providersQuery.isPending) {
    return (
      <div className="space-y-3 p-6">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (providersQuery.isError) {
    return (
      <div className="p-6">
        <ErrorPanel error={providersQuery.error} onRetry={() => void providersQuery.refetch()} />
      </div>
    );
  }

  const refresh = async () => {
    if (selected === null) {
      return;
    }
    setRefreshing(true);
    try {
      const result = await refreshProvider(selected.id);
      if (result.stale) {
        toast.warning(
          `${selected.displayName} couldn't be reached. Showing the last known state.`,
          {
            duration: 10_000,
          },
        );
      } else {
        toast.success(`${selected.displayName} refreshed.`);
      }
      void queryClient.invalidateQueries({ queryKey: keys.providers });
    } catch (error) {
      if (isApiError(error) && error.slug === "provider-not-found") {
        void queryClient.invalidateQueries({ queryKey: keys.providers });
      }
      toast.error(describeError(error, { provider: selected.displayName }).title, {
        duration: 10_000,
      });
    } finally {
      setRefreshing(false);
    }
  };

  const probe = async () => {
    if (selected === null) {
      return;
    }
    setProbing(true);
    try {
      await probeProviderCapabilities(selected.id);
      toast.success("debroxy will test unavailable capabilities again the next time they're used.");
      void queryClient.invalidateQueries({ queryKey: keys.providers });
    } catch (error) {
      if (isApiError(error) && error.slug === "provider-not-found") {
        void queryClient.invalidateQueries({ queryKey: keys.providers });
      }
      toast.error(describeError(error).title, { duration: 10_000 });
    } finally {
      setProbing(false);
    }
  };

  return (
    <div className="space-y-4 p-6">
      <h1 className="text-base font-semibold">Providers</h1>

      <Tabs value={selectedId} onValueChange={setProvider}>
        <TabsList>
          {providers.map((provider) => (
            <TabsTrigger key={provider.id} value={provider.id}>
              {provider.displayName}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      {selected === null ? (
        <p className="text-muted-foreground text-sm">No providers configured.</p>
      ) : (
        <>
          <div className="flex items-center gap-2 text-sm">
            <span className="font-medium">{selected.displayName}</span>
            <span className="text-muted-foreground">{selected.kind}</span>
            {selected.enabled ? (
              <Badge variant="outline">● enabled</Badge>
            ) : (
              <Badge variant="outline">disabled in the configuration</Badge>
            )}
            {selected.stale === true ? (
              <span className="text-state-warning">⚠ not up to date</span>
            ) : null}
          </div>

          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={refreshing}
              onClick={() => void refresh()}
            >
              ⟳ Refresh now
            </Button>
            <Button variant="outline" size="sm" disabled={probing} onClick={() => void probe()}>
              Re-test unavailable capabilities
            </Button>
          </div>

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Capability</TableHead>
                <TableHead>Provider has it</TableHead>
                <TableHead>debroxy uses it</TableHead>
                <TableHead>Learned state</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {selected.capabilities.map((capability) => (
                <TableRow key={capability.name}>
                  <TableCell>{capabilityLabel(capability.name)}</TableCell>
                  <TableCell>{capability.supported ? "✓" : "✕"}</TableCell>
                  <TableCell>
                    {capability.enabled ? "✓" : capability.supported ? "✕ (not in v1)" : "✕"}
                  </TableCell>
                  <TableCell
                    className={cn(
                      capability.state === "available" && "text-state-ready",
                      capability.state === "unavailable" && "text-state-failed",
                    )}
                  >
                    {stateLabel(capability.state)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>

          {!hasWebCapability(selected) ? (
            <p className="text-muted-foreground text-sm">
              This provider doesn&apos;t take web links.
            </p>
          ) : (
            <HostersTable
              data={hostersQuery.data}
              error={hostersQuery.error}
              pending={hostersQuery.isPending}
              onReload={() => void hostersQuery.refetch()}
            />
          )}
        </>
      )}
    </div>
  );
}

function HostersTable({
  data,
  error,
  pending,
  onReload,
}: {
  data:
    | {
        hosters: {
          name: string;
          domains: string[];
          up: boolean;
          dailyLimit?: number;
          dailyUsed?: number;
        }[];
      }
    | undefined;
  error: unknown;
  pending: boolean;
  onReload: () => void;
}) {
  if (pending) {
    return <Skeleton className="h-40 w-full" />;
  }

  if (error !== null) {
    if (isApiError(error)) {
      if (error.slug === "capability-not-supported") {
        return <p className="text-muted-foreground text-sm">No site list for this provider.</p>;
      }
      if (error.slug === "provider-disabled") {
        return <p className="text-muted-foreground text-sm">Provider disabled.</p>;
      }
      if (error.slug === "provider-unavailable" || error.status >= 500) {
        return (
          <div className="flex items-center gap-2 text-sm">
            <span className="text-muted-foreground">Can&apos;t load the site list right now.</span>
            <Button variant="outline" size="sm" onClick={onReload}>
              Reload
            </Button>
          </div>
        );
      }
    }
    return (
      <div className="flex items-center gap-2 text-sm">
        <span className="text-muted-foreground">Can&apos;t load the site list right now.</span>
        <Button variant="outline" size="sm" onClick={onReload}>
          Reload
        </Button>
      </div>
    );
  }

  const hosters = data?.hosters ?? [];
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold">Supported sites ({hosters.length})</h2>
        <Button variant="outline" size="sm" onClick={onReload}>
          Reload
        </Button>
      </div>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Site</TableHead>
            <TableHead>Domains</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Links today</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {hosters.map((hoster) => (
            <TableRow key={hoster.name}>
              <TableCell>{hoster.name}</TableCell>
              <TableCell className="max-w-md whitespace-normal">
                {hoster.domains.join(", ")}
              </TableCell>
              <TableCell className={cn(hoster.up ? "text-state-ready" : "text-state-failed")}>
                {hoster.up ? "● up" : "○ down"}
              </TableCell>
              <TableCell className="tabular-nums">
                {hoster.dailyUsed !== undefined && hoster.dailyLimit !== undefined
                  ? `${hoster.dailyUsed} / ${hoster.dailyLimit} links today`
                  : "—"}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
