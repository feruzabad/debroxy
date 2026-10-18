import { z } from "zod";

// Shared route search schemas and defaults (§4.1). They live in lib/ so both
// features and app components can import them without crossing feature
// boundaries (§3.4).

export const JobsSearch = z.object({
  q: z.string().trim().max(200).optional().catch(undefined),
  group: z.enum(["all", "active", "ready", "problems"]).default("all").catch("all"),
  provider: z.string().optional().catch(undefined),
  sort: z
    .enum(["name", "provider", "state", "progress", "size", "expires", "created"])
    .default("created")
    .catch("created"),
  dir: z.enum(["asc", "desc"]).default("desc").catch("desc"),
});

export type JobsSearch = z.output<typeof JobsSearch>;

export const defaultJobsSearch: JobsSearch = JobsSearch.parse({});

/** Parses a raw URL query string into a validated `JobsSearch` (§4.1). */
export function parseJobsSearch(searchStr: string): JobsSearch {
  const params = new URLSearchParams(searchStr);
  const raw: Record<string, string> = {};
  for (const key of ["q", "group", "provider", "sort", "dir"] as const) {
    const value = params.get(key);
    if (value !== null) {
      raw[key] = value;
    }
  }
  return JobsSearch.parse(raw);
}

export const HistorySearch = z.object({
  q: z.string().optional().catch(undefined),
  sort: z
    .enum(["name", "provider", "finalState", "claimed", "released"])
    .default("released")
    .catch("released"),
  dir: z.enum(["asc", "desc"]).default("desc").catch("desc"),
});

export type HistorySearch = z.output<typeof HistorySearch>;

export const defaultHistorySearch: HistorySearch = HistorySearch.parse({});
