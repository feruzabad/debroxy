// Job-list filtering, counting and sorting (docs/frontend/revision2.md §5.3.2,
// §5.3.3). Pure functions shared by the jobs page and, later, admin jobs.

import type { Job } from "@/api/queries/jobs";
import { type StateGroup, stateInfo, stateOrderIndex } from "@/lib/states";

export type JobSortKey =
  | "name"
  | "provider"
  | "state"
  | "progress"
  | "size"
  | "expires"
  | "created";
export type SortDir = "asc" | "desc";

export interface JobFilter {
  q: string;
  group: "all" | StateGroup;
  provider: string | null;
}

const nameCollator = new Intl.Collator(undefined, { sensitivity: "base", numeric: true });

function compareExpires(a: string | null | undefined, b: string | null | undefined): number {
  // Nulls always sort last, regardless of direction (§5.3.3).
  const aTime = a == null ? Number.POSITIVE_INFINITY : new Date(a).getTime();
  const bTime = b == null ? Number.POSITIVE_INFINITY : new Date(b).getTime();
  return aTime - bTime;
}

function compareJobs(
  a: Job,
  b: Job,
  sort: JobSortKey,
  providerNames: ReadonlyMap<string, string>,
): number {
  switch (sort) {
    case "name":
      return nameCollator.compare(a.displayName, b.displayName);
    case "provider": {
      const aName = providerNames.get(a.provider) ?? a.provider;
      const bName = providerNames.get(b.provider) ?? b.provider;
      return nameCollator.compare(aName, bName);
    }
    case "state":
      return stateOrderIndex(a.state) - stateOrderIndex(b.state);
    case "progress":
      return a.telemetry.progress - b.telemetry.progress;
    case "size":
      return a.sizeBytes - b.sizeBytes;
    case "expires":
      return compareExpires(a.expiresAt, b.expiresAt);
    case "created":
      return a.createdAt.localeCompare(b.createdAt);
  }
}

/** Sorts a copy of the jobs by the given key and direction. */
export function sortJobs(
  jobs: readonly Job[],
  sort: JobSortKey,
  dir: SortDir,
  providerNames: ReadonlyMap<string, string>,
): Job[] {
  return [...jobs].sort((a, b) => {
    let cmp = compareJobs(a, b, sort, providerNames);
    if (cmp === 0) {
      cmp = b.createdAt.localeCompare(a.createdAt);
    }
    return dir === "asc" ? cmp : -cmp;
  });
}

/** Filters jobs by name, state group and provider (§5.3.2). */
export function filterJobs(jobs: readonly Job[], filter: JobFilter): Job[] {
  const needle = filter.q.trim().toLowerCase();
  return jobs.filter((job) => {
    if (filter.group !== "all" && stateInfo(job.state).group !== filter.group) {
      return false;
    }
    if (filter.provider !== null && job.provider !== filter.provider) {
      return false;
    }
    if (needle !== "" && !job.displayName.toLowerCase().includes(needle)) {
      return false;
    }
    return true;
  });
}

/** Live counts per state group for the filter chips (§5.3.1). */
export function groupCounts(jobs: readonly Job[]): {
  all: number;
  active: number;
  ready: number;
  problems: number;
} {
  const counts = { all: jobs.length, active: 0, ready: 0, problems: 0 };
  for (const job of jobs) {
    const group = stateInfo(job.state).group;
    if (group === "active") {
      counts.active += 1;
    } else if (group === "ready") {
      counts.ready += 1;
    } else {
      counts.problems += 1;
    }
  }
  return counts;
}

/** The provider ids appearing in the listed jobs, in first-seen order. */
export function distinctProviders(jobs: readonly Job[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const job of jobs) {
    if (!seen.has(job.provider)) {
      seen.add(job.provider);
      out.push(job.provider);
    }
  }
  return out;
}
