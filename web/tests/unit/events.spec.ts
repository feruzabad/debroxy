import { describe, expect, it, vi } from "vitest";
import {
  decideToast,
  type EventSourceLike,
  EventStreamController,
  type EventStreamDeps,
  type Job,
  leavesReady,
  type MessageEventLike,
  mergeProviderStale,
  mergeQuota,
  parseEvent,
  prependHistory,
  removeJob,
  stateChanged,
  type Timers,
  upsertJob,
  type VisibilityController,
} from "@/api/events";
import type { Me, ProviderListWithStale } from "@/api/types-rev13";

function jobFixture(overrides: Partial<Job> = {}): Job {
  return {
    id: "job-1",
    displayName: "Sintel",
    provider: "torbox",
    kind: "torrent",
    state: "downloading",
    telemetry: { progress: 0.5, bytesPerSecond: 100, etaSeconds: 60, peers: 3, stalled: false },
    sizeBytes: 1000,
    expiresAt: null,
    shared: false,
    slotCharged: true,
    delivery: { perFile: true, bundle: { format: "zip", rangeSupported: false } },
    files: [{ id: "f1", path: "a.mp4", sizeBytes: 1000 }],
    createdAt: "2026-10-13T00:00:00Z",
    failureReason: null,
    ...overrides,
  };
}

describe("upsertJob", () => {
  it("inserts into an empty list", () => {
    expect(upsertJob(undefined, jobFixture())).toHaveLength(1);
  });

  it("replaces an existing job by id", () => {
    const list = [jobFixture({ id: "a" }), jobFixture({ id: "b" })];
    const next = upsertJob(list, jobFixture({ id: "a", displayName: "renamed" }));
    expect(next).toHaveLength(2);
    expect(next[0]?.displayName).toBe("renamed");
  });

  it("inserts a new job at the newest-createdAt position", () => {
    const old = jobFixture({ id: "old", createdAt: "2026-10-10T00:00:00Z" });
    const newest = jobFixture({ id: "newest", createdAt: "2026-10-13T00:00:00Z" });
    const middle = jobFixture({ id: "middle", createdAt: "2026-10-11T00:00:00Z" });
    const next = upsertJob([old, middle], newest);
    expect(next.map((j) => j.id)).toEqual(["newest", "old", "middle"]);
  });
});

describe("removeJob", () => {
  it("removes a job by id", () => {
    const list = [jobFixture({ id: "a" }), jobFixture({ id: "b" })];
    expect(removeJob(list, "a").map((j) => j.id)).toEqual(["b"]);
  });

  it("returns the same list when absent", () => {
    const list = [jobFixture({ id: "a" })];
    expect(removeJob(list, "zzz")).toBe(list);
  });
});

describe("prependHistory", () => {
  it("prepends a new entry", () => {
    const entry = {
      jobId: "j",
      displayName: "x",
      provider: "torbox",
      kind: "torrent",
      name: "x",
      finalState: "ready",
      failureReason: null,
      claimedAt: "2026-10-10T00:00:00Z",
      releasedAt: "2026-10-13T00:00:00Z",
    };
    expect(prependHistory(undefined, entry)).toEqual([entry]);
    expect(prependHistory([entry], { ...entry, jobId: "newer" })[0]?.jobId).toBe("newer");
  });
});

describe("mergeQuota", () => {
  const me: Me = {
    user: {
      id: "u1",
      username: "alice",
      role: "user",
      status: "active",
      passwordLogin: true,
      oidcLinked: false,
      createdAt: "2026-10-10T00:00:00Z",
    },
    quota: { slotsUsed: 1, maxActiveJobs: 3 },
    session: {
      idleExpiresAt: "2026-10-13T01:00:00Z",
      absoluteExpiresAt: "2026-10-20T01:00:00Z",
    },
    submitRate: {
      uncached: { used: 1, perHour: 10, resetsAt: null },
      cached: { used: 0, perHour: 60, resetsAt: null },
    },
  };

  it("replaces quota and keeps the rest", () => {
    const next = mergeQuota(me, { slotsUsed: 2, maxActiveJobs: 5 });
    expect(next?.quota).toEqual({ slotsUsed: 2, maxActiveJobs: 5 });
    expect(next?.user.username).toBe("alice");
  });

  it("returns undefined for no cached me", () => {
    expect(mergeQuota(undefined, { slotsUsed: 0, maxActiveJobs: 3 })).toBeUndefined();
  });
});

describe("mergeProviderStale", () => {
  const list: ProviderListWithStale = {
    providers: [
      {
        id: "torbox",
        kind: "torbox",
        displayName: "TorBox",
        enabled: true,
        capabilities: [],
      },
      {
        id: "rd",
        kind: "real-debrid",
        displayName: "Real-Debrid",
        enabled: true,
        capabilities: [],
      },
    ],
  };

  it("sets the stale flag on the matching provider", () => {
    const next = mergeProviderStale(list, "torbox", true);
    expect(next?.providers[0]?.stale).toBe(true);
    expect(next?.providers[1]?.stale).toBeUndefined();
  });

  it("clears the stale flag", () => {
    const stale = mergeProviderStale(list, "torbox", true);
    const fresh = mergeProviderStale(stale, "torbox", false);
    expect(fresh?.providers[0]?.stale).toBe(false);
  });

  it("returns undefined for no cached list", () => {
    expect(mergeProviderStale(undefined, "torbox", true)).toBeUndefined();
  });
});

describe("stateChanged / leavesReady", () => {
  it("detects a state change", () => {
    expect(stateChanged(jobFixture({ state: "downloading" }), jobFixture({ state: "ready" }))).toBe(
      true,
    );
    expect(stateChanged(jobFixture({ state: "ready" }), jobFixture({ state: "ready" }))).toBe(
      false,
    );
    expect(stateChanged(undefined, jobFixture({ state: "ready" }))).toBe(false);
  });

  it("detects leaving ready", () => {
    expect(leavesReady(jobFixture({ state: "ready" }), jobFixture({ state: "gone" }))).toBe(true);
    expect(leavesReady(jobFixture({ state: "ready" }), jobFixture({ state: "ready" }))).toBe(false);
    expect(leavesReady(undefined, jobFixture({ state: "gone" }))).toBe(false);
  });
});

describe("decideToast", () => {
  const now = Date.UTC(2026, 9, 13, 12, 0, 0);
  const iso = (ms: number) => new Date(ms).toISOString();

  it("toasts when a cached job becomes ready", () => {
    const previous = jobFixture({ state: "downloading" });
    const next = jobFixture({ state: "ready", createdAt: iso(now - 60_000) });
    expect(decideToast(previous, next, new Set(), now)).toEqual({
      kind: "ready",
      jobId: "job-1",
      displayName: "Sintel",
    });
  });

  it("toasts when a cached job becomes failed", () => {
    const previous = jobFixture({ state: "downloading" });
    const next = jobFixture({
      state: "failed",
      createdAt: iso(now - 60_000),
      failureReason: "provider-rejected",
    });
    expect(decideToast(previous, next, new Set(), now)).toEqual({
      kind: "failed",
      jobId: "job-1",
      displayName: "Sintel",
      failureReason: "provider-rejected",
    });
  });

  it("does not toast for a job not in the cache before", () => {
    const next = jobFixture({ state: "ready" });
    expect(decideToast(undefined, next, new Set(), now)).toBeNull();
  });

  it("does not toast without a state change", () => {
    const previous = jobFixture({ state: "ready" });
    const next = jobFixture({ state: "ready" });
    expect(decideToast(previous, next, new Set(), now)).toBeNull();
  });

  it("does not toast for a non-ready/failed transition", () => {
    const previous = jobFixture({ state: "downloading" });
    const next = jobFixture({ state: "processing" });
    expect(decideToast(previous, next, new Set(), now)).toBeNull();
  });

  it("suppresses a ready toast for a job submitted in the last 3 s", () => {
    const previous = jobFixture({ state: "downloading" });
    const next = jobFixture({ state: "ready", createdAt: iso(now - 500) });
    expect(decideToast(previous, next, new Set(), now)).toBeNull();
  });

  it("de-duplicates by (jobId, state)", () => {
    const previous = jobFixture({ state: "downloading" });
    const next = jobFixture({ state: "ready", createdAt: iso(now - 60_000) });
    const dedup = new Set<string>();
    expect(decideToast(previous, next, dedup, now)).not.toBeNull();
    expect(decideToast(previous, next, dedup, now)).toBeNull();
  });
});

describe("parseEvent", () => {
  it("parses and validates a job.upserted event", () => {
    const result = parseEvent("job.upserted", JSON.stringify(jobFixture()));
    expect(result).toMatchObject({ name: "job.upserted" });
  });

  it("drops invalid JSON", () => {
    expect(parseEvent("job.upserted", "not json")).toBeNull();
  });

  it("drops data that fails the schema", () => {
    expect(parseEvent("job.upserted", JSON.stringify({ id: 1 }))).toBeNull();
  });

  it("drops unknown event names", () => {
    expect(parseEvent("something.else", "{}")).toBeNull();
  });
});

// --- Controller harness ------------------------------------------------------

class FakeEventSource implements EventSourceLike {
  closed = false;
  private listeners = new Map<string, (event: MessageEventLike) => void>();

  constructor(public readonly url: string) {}

  addEventListener(type: string, listener: (event: MessageEventLike) => void): void {
    this.listeners.set(type, listener);
  }

  close(): void {
    this.closed = true;
  }

  emit(type: string, data = ""): void {
    this.listeners.get(type)?.({ data });
  }
}

class FakeTimers implements Timers {
  private timeouts = new Map<number, { handler: () => void; ms: number }>();
  private intervals = new Map<number, { handler: () => void; ms: number }>();
  private next = 1;

  setTimeout(handler: () => void, ms: number): number {
    const id = this.next;
    this.next += 1;
    this.timeouts.set(id, { handler, ms });
    return id;
  }

  clearTimeout(id: number): void {
    this.timeouts.delete(id);
  }

  setInterval(handler: () => void, ms: number): number {
    const id = this.next;
    this.next += 1;
    this.intervals.set(id, { handler, ms });
    return id;
  }

  clearInterval(id: number): void {
    this.intervals.delete(id);
  }

  hasInterval(ms: number): boolean {
    return [...this.intervals.values()].some((item) => item.ms === ms);
  }

  hasTimeout(ms: number): boolean {
    return [...this.timeouts.values()].some((item) => item.ms === ms);
  }

  fireInterval(ms: number): void {
    for (const item of this.intervals.values()) {
      if (item.ms === ms) {
        item.handler();
      }
    }
  }

  fireTimeout(ms: number): void {
    for (const [id, item] of this.timeouts) {
      if (item.ms === ms) {
        this.timeouts.delete(id);
        item.handler();
      }
    }
  }
}

class FakeVisibility implements VisibilityController {
  private hidden = false;
  private listeners: Array<(hidden: boolean) => void> = [];

  isHidden(): boolean {
    return this.hidden;
  }

  subscribe(listener: (hidden: boolean) => void): () => void {
    this.listeners.push(listener);
    return () => {
      this.listeners = this.listeners.filter((l) => l !== listener);
    };
  }

  setHidden(hidden: boolean): void {
    this.hidden = hidden;
    for (const listener of this.listeners) {
      listener(hidden);
    }
  }
}

function makeController() {
  const sources: FakeEventSource[] = [];
  const timers = new FakeTimers();
  const visibility = new FakeVisibility();
  const onStateChange = vi.fn();
  const onEvent = vi.fn();
  const onReady = vi.fn();
  const onPoll = vi.fn();
  const deps: EventStreamDeps = {
    url: "/api/v1/events",
    createEventSource: (url) => {
      const es = new FakeEventSource(url);
      sources.push(es);
      return es;
    },
    timers,
    visibility,
    onStateChange,
    onEvent,
    onReady,
    onPoll,
  };
  const controller = new EventStreamController(deps);
  return { controller, sources, timers, visibility, onStateChange, onEvent, onReady, onPoll };
}

describe("EventStreamController", () => {
  it("opens a stream on start and goes live on ready", () => {
    const { controller, sources, onReady, onStateChange } = makeController();
    controller.start();
    expect(sources).toHaveLength(1);
    expect(sources[0]?.url).toBe("/api/v1/events");
    expect(controller.connectionState).toBe("connecting");

    sources[0]?.emit("ready", JSON.stringify({ scope: "own" }));
    expect(controller.connectionState).toBe("live");
    expect(onReady).toHaveBeenCalledWith("own");
    expect(onStateChange).toHaveBeenCalledWith("live");
  });

  it("falls back to polling after 3 consecutive errors", () => {
    const { controller, sources, timers, onStateChange } = makeController();
    controller.start();

    sources[0]?.emit("error");
    sources[0]?.emit("error");
    expect(controller.connectionState).toBe("connecting");

    sources[0]?.emit("error");
    expect(controller.connectionState).toBe("polling");
    expect(onStateChange).toHaveBeenCalledWith("polling");
    expect(sources[0]?.closed).toBe(true);
    expect(timers.hasInterval(10_000)).toBe(true);
    expect(timers.hasInterval(60_000)).toBe(true);
  });

  it("polls on the 10 s interval while polling", () => {
    const { controller, sources, timers, onPoll } = makeController();
    controller.start();
    sources[0]?.emit("error");
    sources[0]?.emit("error");
    sources[0]?.emit("error");

    timers.fireInterval(10_000);
    expect(onPoll).toHaveBeenCalledTimes(1);
  });

  it("retries the stream every 60 s and recovers to live on ready", () => {
    const { controller, sources, timers, onReady } = makeController();
    controller.start();
    sources[0]?.emit("error");
    sources[0]?.emit("error");
    sources[0]?.emit("error");
    expect(controller.connectionState).toBe("polling");

    timers.fireInterval(60_000);
    expect(sources).toHaveLength(2);

    sources[1]?.emit("ready", JSON.stringify({ scope: "own" }));
    expect(controller.connectionState).toBe("live");
    expect(onReady).toHaveBeenCalledWith("own");
    expect(timers.hasInterval(10_000)).toBe(false);
    expect(timers.hasInterval(60_000)).toBe(false);
  });

  it("treats a recovered stream like the first: it reconnects and falls back again", () => {
    const { controller, sources, timers } = makeController();
    controller.start();
    sources[0]?.emit("error");
    sources[0]?.emit("error");
    sources[0]?.emit("error");
    timers.fireInterval(60_000);
    const recovered = sources[1];
    recovered?.emit("ready", JSON.stringify({ scope: "own" }));
    expect(controller.connectionState).toBe("live");

    recovered?.emit("error");
    expect(controller.connectionState).toBe("reconnecting");
    expect(recovered?.closed).toBe(false);
    recovered?.emit("error");
    recovered?.emit("error");
    expect(controller.connectionState).toBe("polling");
    expect(timers.hasInterval(10_000)).toBe(true);
  });

  it("closes a failed retry stream and keeps polling", () => {
    const { controller, sources, timers } = makeController();
    controller.start();
    sources[0]?.emit("error");
    sources[0]?.emit("error");
    sources[0]?.emit("error");

    timers.fireInterval(60_000);
    const retry = sources[1];
    retry?.emit("error");
    expect(retry?.closed).toBe(true);
    expect(controller.connectionState).toBe("polling");
  });

  it("closes after 5 min hidden and reopens when visible", () => {
    const { controller, sources, timers, visibility } = makeController();
    controller.start();
    sources[0]?.emit("ready", JSON.stringify({ scope: "own" }));

    visibility.setHidden(true);
    expect(timers.hasTimeout(5 * 60_000)).toBe(true);

    timers.fireTimeout(5 * 60_000);
    expect(sources[0]?.closed).toBe(true);

    visibility.setHidden(false);
    expect(sources).toHaveLength(2);
    expect(controller.connectionState).toBe("connecting");
  });

  it("does not close early when hidden for less than 5 min", () => {
    const { controller, sources, visibility } = makeController();
    controller.start();
    visibility.setHidden(true);
    visibility.setHidden(false);
    expect(sources[0]?.closed).toBe(false);
    expect(controller.connectionState).toBe("connecting");
  });

  it("drops invalid event data", () => {
    const { controller, sources, onEvent } = makeController();
    controller.start();
    sources[0]?.emit("job.upserted", "not json");
    sources[0]?.emit("job.upserted", JSON.stringify({ id: 1 }));
    expect(onEvent).not.toHaveBeenCalled();
  });

  it("forwards valid event data to onEvent", () => {
    const { controller, sources, onEvent } = makeController();
    controller.start();
    sources[0]?.emit("job.upserted", JSON.stringify(jobFixture()));
    expect(onEvent).toHaveBeenCalledWith("job.upserted", expect.objectContaining({ id: "job-1" }));
  });
});
