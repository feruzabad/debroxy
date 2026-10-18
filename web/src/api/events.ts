// Event Stream cache reducer and connection controller (docs/frontend/revision2.md §8).

import type { ZodType } from "zod";
import type { components } from "./generated/job";
import {
  jobReleasedSchema,
  jobSchema,
  providerStaleSchema,
  quotaUsageSchema,
  readySchema,
} from "./schemas";
import type {
  EventScope,
  JobReleasedEvent,
  Me,
  ProviderListWithStale,
  ProviderStaleEvent,
  ReadyEvent,
} from "./types-rev13";

export type Job = components["schemas"]["Job"];
export type HistoryEntry = components["schemas"]["HistoryEntry"];
export type QuotaUsage = components["schemas"]["QuotaUsage"];

// --- Pure cache-update functions (§8.3) ---------------------------------------

/** Upserts a job by `id`, inserting new jobs at the newest-`createdAt`-first position. */
export function upsertJob(jobs: Job[] | undefined, job: Job): Job[] {
  const list = jobs ?? [];
  const existing = list.findIndex((entry) => entry.id === job.id);
  if (existing >= 0) {
    const next = list.slice();
    next[existing] = job;
    return next;
  }
  let insertAt = list.length;
  for (let i = 0; i < list.length; i += 1) {
    const entry = list[i];
    if (entry !== undefined && entry.createdAt < job.createdAt) {
      insertAt = i;
      break;
    }
  }
  const next = list.slice();
  next.splice(insertAt, 0, job);
  return next;
}

/** Removes a job by `id`, leaving the list unchanged when it is absent. */
export function removeJob(jobs: Job[] | undefined, jobId: string): Job[] {
  if (jobs === undefined) {
    return [];
  }
  const index = jobs.findIndex((entry) => entry.id === jobId);
  if (index < 0) {
    return jobs;
  }
  return [...jobs.slice(0, index), ...jobs.slice(index + 1)];
}

/** Prepends a history entry, for `job.released`. */
export function prependHistory(
  entries: HistoryEntry[] | undefined,
  entry: HistoryEntry,
): HistoryEntry[] {
  return [entry, ...(entries ?? [])];
}

/** Replaces `me.quota`, for `quota.updated`. */
export function mergeQuota(me: Me | undefined, quota: QuotaUsage): Me | undefined {
  if (me === undefined) {
    return undefined;
  }
  return { ...me, quota };
}

/** Merges the client-only `stale` flag into one provider, for `provider.stale`. */
export function mergeProviderStale(
  list: ProviderListWithStale | undefined,
  providerId: string,
  stale: boolean,
): ProviderListWithStale | undefined {
  if (list === undefined) {
    return undefined;
  }
  let changed = false;
  const providers = list.providers.map((provider) => {
    if (provider.id !== providerId) {
      return provider;
    }
    if (provider.stale === stale) {
      return provider;
    }
    changed = true;
    return { ...provider, stale };
  });
  return changed ? { ...list, providers } : list;
}

/** True when an upsert changes the job's state (used to decide toasts). */
export function stateChanged(previous: Job | undefined, next: Job): boolean {
  return previous !== undefined && previous.state !== next.state;
}

/** True when the job leaves `ready`, so `['links', id, …]` must be dropped. */
export function leavesReady(previous: Job | undefined, next: Job): boolean {
  return previous !== undefined && previous.state === "ready" && next.state !== "ready";
}

// --- Toast triggers (§8.7) ----------------------------------------------------

export type ToastEvent =
  | { kind: "ready"; jobId: string; displayName: string }
  | { kind: "failed"; jobId: string; displayName: string; failureReason: string | null };

const SUBMIT_TOAST_WINDOW_MS = 3_000;

/**
 * Decides whether an own-scope `job.upserted` should toast, and de-duplicates by
 * `(jobId, state)` in the supplied set. `previous` is the cached job before the
 * event (undefined when the job was not in the cache).
 */
export function decideToast(
  previous: Job | undefined,
  next: Job,
  dedup: Set<string>,
  now: number = Date.now(),
): ToastEvent | null {
  if (previous === undefined) {
    return null;
  }
  if (previous.state === next.state) {
    return null;
  }
  if (next.state !== "ready" && next.state !== "failed") {
    return null;
  }
  if (next.state === "ready" && now - Date.parse(next.createdAt) < SUBMIT_TOAST_WINDOW_MS) {
    return null;
  }
  const key = `${next.id}:${next.state}`;
  if (dedup.has(key)) {
    return null;
  }
  dedup.add(key);
  if (next.state === "ready") {
    return { kind: "ready", jobId: next.id, displayName: next.displayName };
  }
  return {
    kind: "failed",
    jobId: next.id,
    displayName: next.displayName,
    failureReason: next.failureReason ?? null,
  };
}

// --- Event parsing and validation ---------------------------------------------

const EVENT_SCHEMAS: Record<string, ZodType> = {
  "job.upserted": jobSchema,
  "job.released": jobReleasedSchema,
  "quota.updated": quotaUsageSchema,
  "provider.stale": providerStaleSchema,
  ready: readySchema,
};

export type ParsedEvent =
  | { name: "job.upserted"; data: Job }
  | { name: "job.released"; data: JobReleasedEvent }
  | { name: "quota.updated"; data: QuotaUsage }
  | { name: "provider.stale"; data: ProviderStaleEvent }
  | { name: "ready"; data: ReadyEvent };

/** Parses and validates one SSE event; invalid or unknown names are dropped (null). */
export function parseEvent(name: string, raw: string): ParsedEvent | null {
  const schema = EVENT_SCHEMAS[name];
  if (schema === undefined) {
    return null;
  }
  let json: unknown;
  try {
    json = JSON.parse(raw);
  } catch {
    return null;
  }
  const result = schema.safeParse(json);
  if (!result.success) {
    return null;
  }
  return { name: name as ParsedEvent["name"], data: result.data } as ParsedEvent;
}

// --- Event Stream controller (§8.1–§8.5) --------------------------------------

export type ConnectionState = "connecting" | "live" | "reconnecting" | "polling";

export interface MessageEventLike {
  data: string;
}

export interface EventSourceLike {
  addEventListener(type: string, listener: (event: MessageEventLike) => void): void;
  close(): void;
}

export interface Timers {
  setTimeout(handler: () => void, ms: number): number;
  clearTimeout(id: number): void;
  setInterval(handler: () => void, ms: number): number;
  clearInterval(id: number): void;
}

export interface VisibilityController {
  isHidden(): boolean;
  subscribe(listener: (hidden: boolean) => void): () => void;
}

export interface EventStreamDeps {
  url: string;
  createEventSource: (url: string) => EventSourceLike;
  timers: Timers;
  visibility: VisibilityController;
  onStateChange: (state: ConnectionState) => void;
  onEvent: (name: string, data: unknown) => void;
  onReady: (scope: EventScope) => void;
  onPoll: () => void;
}

const FAILURE_LIMIT = 3;
const POLL_INTERVAL_MS = 10_000;
const STREAM_RETRY_MS = 60_000;
const HIDDEN_CLOSE_MS = 5 * 60_000;

const EVENT_NAMES = [
  "ready",
  "job.upserted",
  "job.released",
  "quota.updated",
  "provider.stale",
] as const;

export class EventStreamController {
  private es: EventSourceLike | null = null;
  private state: ConnectionState = "connecting";
  private failureCount = 0;
  private wasLive = false;
  private pollIntervalId: number | null = null;
  private retryIntervalId: number | null = null;
  private hiddenTimeoutId: number | null = null;
  private hiddenDue = false;
  // A trial stream opened while polling becomes primary once it is ready (§8.5).
  private primary = true;
  private started = false;
  private closed = false;
  private unsubscribeVisibility: (() => void) | null;

  constructor(private readonly deps: EventStreamDeps) {
    this.unsubscribeVisibility = deps.visibility.subscribe((hidden) =>
      this.handleVisibility(hidden),
    );
  }

  get connectionState(): ConnectionState {
    return this.state;
  }

  start(): void {
    if (this.started) {
      return;
    }
    this.started = true;
    this.openStream();
  }

  close(): void {
    this.closed = true;
    this.closeStream();
    this.stopPolling();
    this.clearHiddenTimeout();
    this.unsubscribeVisibility?.();
    this.unsubscribeVisibility = null;
  }

  private setState(next: ConnectionState): void {
    if (this.state === next) {
      return;
    }
    this.state = next;
    this.deps.onStateChange(next);
  }

  private openStream(): void {
    if (this.closed) {
      return;
    }
    this.closeStream();
    this.failureCount = 0;
    const es = this.deps.createEventSource(this.deps.url);
    this.es = es;
    this.primary = true;
    this.attachHandlers(es);
  }

  private attachHandlers(es: EventSourceLike): void {
    es.addEventListener("error", () => this.handleError(es));
    for (const name of EVENT_NAMES) {
      es.addEventListener(name, (event) => this.handleMessage(name, event.data));
    }
  }

  private handleError(es: EventSourceLike): void {
    if (this.closed || this.es !== es) {
      return;
    }
    if (!this.primary) {
      this.closeStream();
      return;
    }
    this.failureCount += 1;
    if (this.state !== "polling") {
      this.setState(this.wasLive ? "reconnecting" : "connecting");
    }
    if (this.failureCount >= FAILURE_LIMIT) {
      this.switchToPolling();
    }
  }

  private handleMessage(name: string, raw: string): void {
    if (this.closed) {
      return;
    }
    const parsed = parseEvent(name, raw);
    if (parsed === null) {
      return;
    }
    if (parsed.name === "ready") {
      this.handleReady(parsed.data.scope);
      return;
    }
    this.deps.onEvent(parsed.name, parsed.data);
  }

  private handleReady(scope: EventScope): void {
    this.primary = true;
    this.failureCount = 0;
    this.wasLive = true;
    this.stopPolling();
    this.setState("live");
    this.deps.onReady(scope);
  }

  private switchToPolling(): void {
    this.closeStream();
    this.setState("polling");
    if (this.pollIntervalId === null) {
      this.pollIntervalId = this.deps.timers.setInterval(
        () => this.deps.onPoll(),
        POLL_INTERVAL_MS,
      );
    }
    if (this.retryIntervalId === null) {
      this.retryIntervalId = this.deps.timers.setInterval(
        () => this.retryStream(),
        STREAM_RETRY_MS,
      );
    }
  }

  private retryStream(): void {
    if (this.closed || this.state !== "polling" || this.es !== null) {
      return;
    }
    const es = this.deps.createEventSource(this.deps.url);
    this.es = es;
    this.primary = false;
    this.attachHandlers(es);
  }

  private stopPolling(): void {
    if (this.pollIntervalId !== null) {
      this.deps.timers.clearInterval(this.pollIntervalId);
      this.pollIntervalId = null;
    }
    if (this.retryIntervalId !== null) {
      this.deps.timers.clearInterval(this.retryIntervalId);
      this.retryIntervalId = null;
    }
  }

  private closeStream(): void {
    if (this.es !== null) {
      this.es.close();
      this.es = null;
    }
  }

  private handleVisibility(hidden: boolean): void {
    this.clearHiddenTimeout();
    if (hidden) {
      this.hiddenTimeoutId = this.deps.timers.setTimeout(() => this.hideDue(), HIDDEN_CLOSE_MS);
      return;
    }
    if (this.hiddenDue) {
      this.hiddenDue = false;
      this.setState("connecting");
      this.openStream();
    }
  }

  private hideDue(): void {
    this.hiddenDue = true;
    this.closeStream();
    this.stopPolling();
  }

  private clearHiddenTimeout(): void {
    if (this.hiddenTimeoutId !== null) {
      this.deps.timers.clearTimeout(this.hiddenTimeoutId);
      this.hiddenTimeoutId = null;
    }
  }
}
