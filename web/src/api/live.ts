// Event Stream wiring: connects EventStreamController to the Query cache,
// the connection indicator and the transition toasts (docs/frontend/revision2.md §8).

import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useSyncExternalStore } from "react";
import { toast } from "sonner";
import {
  type ConnectionState,
  decideToast,
  type EventSourceLike,
  EventStreamController,
  type HistoryEntry,
  type Job,
  leavesReady,
  mergeProviderStale,
  mergeQuota,
  prependHistory,
  removeJob,
  type Timers,
  upsertJob,
  type VisibilityController,
} from "@/api/events";
import { keys } from "@/api/keys";
import type { JobList } from "@/api/queries/jobs";
import type {
  JobReleasedEvent,
  Me,
  ProviderListWithStale,
  ProviderStaleEvent,
} from "@/api/types-rev13";
import { failureText } from "@/lib/states";

// --- connection state store (§8.4) -------------------------------------------

let ownState: ConnectionState = "connecting";
let adminState: ConnectionState = "connecting";
const listeners = new Set<() => void>();

function notify(): void {
  for (const listener of listeners) {
    listener();
  }
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** The own-scope stream's state, for the header indicator and live stale times. */
export function useConnectionState(): ConnectionState {
  return useSyncExternalStore(subscribe, () => ownState);
}

export function useAdminConnectionState(): ConnectionState {
  return useSyncExternalStore(subscribe, () => adminState);
}

// --- released jobs (§5.6.2) ---------------------------------------------------

const releasedEntries = new Map<string, HistoryEntry>();

/**
 * The released entry for a job whose detail page is open, set by `job.released`
 * so the page can show the §5.6.2 "released" state instead of a 404.
 */
export function useReleasedEntry(jobId: string): HistoryEntry | undefined {
  return useSyncExternalStore(subscribe, () => releasedEntries.get(jobId));
}

// --- browser adapters ----------------------------------------------------------

const browserTimers: Timers = {
  setTimeout: (handler, ms) => window.setTimeout(handler, ms),
  clearTimeout: (id) => window.clearTimeout(id),
  setInterval: (handler, ms) => window.setInterval(handler, ms),
  clearInterval: (id) => window.clearInterval(id),
};

const browserVisibility: VisibilityController = {
  isHidden: () => document.visibilityState === "hidden",
  subscribe: (listener) => {
    const handler = () => listener(document.visibilityState === "hidden");
    document.addEventListener("visibilitychange", handler);
    return () => document.removeEventListener("visibilitychange", handler);
  },
};

function createEventSource(url: string): EventSourceLike {
  return new EventSource(url) as unknown as EventSourceLike;
}

const running = new Set<EventStreamController>();

/** Closes every Event Stream (logout and the session-ended flow, §7.4). */
export function closeAllStreams(): void {
  for (const controller of running) {
    controller.close();
  }
  running.clear();
}

// --- own scope -----------------------------------------------------------------

const toastSeen = new Set<string>();

function applyOwnEvent(
  queryClient: QueryClient,
  name: string,
  data: unknown,
  openJob: (id: string) => void,
) {
  switch (name) {
    case "job.upserted": {
      const job = data as Job;
      const previous =
        queryClient.getQueryData<JobList>(keys.jobs)?.jobs.find((entry) => entry.id === job.id) ??
        queryClient.getQueryData<Job>(keys.job(job.id));
      const decision = decideToast(previous, job, toastSeen);
      queryClient.setQueryData<JobList>(keys.jobs, (list) =>
        list === undefined ? list : { ...list, jobs: upsertJob(list.jobs, job) },
      );
      queryClient.setQueryData(keys.job(job.id), job);
      if (leavesReady(previous, job)) {
        queryClient.removeQueries({ queryKey: ["links", job.id] });
      }
      if (decision?.kind === "ready") {
        toast.success(`${decision.displayName} is ready.`, {
          action: { label: "Open", onClick: () => openJob(decision.jobId) },
        });
      } else if (decision?.kind === "failed") {
        const reason = failureText(decision.failureReason, job.provider).text;
        toast.error(`${decision.displayName} failed: ${reason}`, {
          duration: 10_000,
          action: { label: "Open", onClick: () => openJob(decision.jobId) },
        });
      }
      return;
    }
    case "job.released": {
      const { jobId, historyEntry } = data as JobReleasedEvent;
      releasedEntries.set(jobId, historyEntry);
      notify();
      queryClient.setQueryData<JobList>(keys.jobs, (list) =>
        list === undefined ? list : { ...list, jobs: removeJob(list.jobs, jobId) },
      );
      queryClient.removeQueries({ queryKey: keys.job(jobId) });
      queryClient.removeQueries({ queryKey: ["links", jobId] });
      queryClient.setQueryData<{ entries: HistoryEntry[] }>(keys.history, (history) =>
        history === undefined
          ? history
          : { ...history, entries: prependHistory(history.entries, historyEntry) },
      );
      return;
    }
    case "quota.updated":
      queryClient.setQueryData<Me>(keys.me, (me) => mergeQuota(me, data as Me["quota"]));
      return;
    case "provider.stale": {
      const { providerId, stale } = data as ProviderStaleEvent;
      queryClient.setQueryData<ProviderListWithStale>(keys.providers, (list) =>
        mergeProviderStale(list, providerId, stale),
      );
      return;
    }
  }
}

/** Keeps the own-scope stream open while the shell is mounted (§8.1). */
export function useOwnEventStream(): void {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  useEffect(() => {
    const openJob = (jobId: string) =>
      void navigate({ to: "/jobs/$jobId", params: { jobId }, search: { tab: "files" } });
    const controller = new EventStreamController({
      url: "/api/v1/events",
      createEventSource,
      timers: browserTimers,
      visibility: browserVisibility,
      onStateChange: (state) => {
        ownState = state;
        notify();
      },
      onEvent: (name, data) => applyOwnEvent(queryClient, name, data, openJob),
      onReady: () => {
        void queryClient.invalidateQueries({ queryKey: keys.jobs });
        void queryClient.invalidateQueries({ queryKey: keys.me });
      },
      onPoll: () => void queryClient.refetchQueries({ queryKey: keys.jobs, type: "active" }),
    });
    running.add(controller);
    controller.start();
    return () => {
      controller.close();
      running.delete(controller);
      ownState = "connecting";
      notify();
    };
  }, [queryClient, navigate]);
}

// --- admin scope (§8.1, /admin/jobs only) ----------------------------------------

function applyAdminEvent(queryClient: QueryClient, name: string, data: unknown) {
  switch (name) {
    case "job.upserted":
      queryClient.setQueryData<JobList>(keys.admin.jobs, (list) =>
        list === undefined ? list : { ...list, jobs: upsertJob(list.jobs, data as Job) },
      );
      return;
    case "job.released":
      queryClient.setQueryData<JobList>(keys.admin.jobs, (list) =>
        list === undefined
          ? list
          : { ...list, jobs: removeJob(list.jobs, (data as JobReleasedEvent).jobId) },
      );
      return;
    case "provider.stale": {
      const { providerId, stale } = data as ProviderStaleEvent;
      queryClient.setQueryData<ProviderListWithStale>(keys.providers, (list) =>
        mergeProviderStale(list, providerId, stale),
      );
      return;
    }
  }
}

export function useAdminEventStream(): void {
  const queryClient = useQueryClient();
  useEffect(() => {
    const controller = new EventStreamController({
      url: "/api/v1/events?scope=all",
      createEventSource,
      timers: browserTimers,
      visibility: browserVisibility,
      onStateChange: (state) => {
        adminState = state;
        notify();
      },
      onEvent: (name, data) => applyAdminEvent(queryClient, name, data),
      onReady: () => void queryClient.invalidateQueries({ queryKey: keys.admin.jobs }),
      onPoll: () => void queryClient.refetchQueries({ queryKey: keys.admin.jobs, type: "active" }),
    });
    running.add(controller);
    controller.start();
    return () => {
      controller.close();
      running.delete(controller);
      adminState = "connecting";
      notify();
    };
  }, [queryClient]);
}
