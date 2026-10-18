import { queryOptions } from "@tanstack/react-query";
import { jobClient } from "@/api/client";
import { ApiError } from "@/api/errors";
import type { components } from "@/api/generated/job";
import { keys } from "@/api/keys";

export type Job = components["schemas"]["Job"];
export type JobList = components["schemas"]["JobList"];
export type HistoryList = components["schemas"]["HistoryList"];
export type ProviderList = components["schemas"]["ProviderList"];
export type Provider = components["schemas"]["Provider"];
export type HosterList = components["schemas"]["HosterList"];

function required<T>(data: T | undefined): T {
  if (data === undefined) {
    throw new ApiError({ status: 0, slug: "network-error", message: "" });
  }
  return data;
}

/** Live queries are fresh forever while the stream is Live, else for 10 s (§8.6). */
export function liveStaleTime(live: boolean): number {
  return live ? Number.POSITIVE_INFINITY : 10_000;
}

export const jobsQueryOptions = queryOptions({
  queryKey: keys.jobs,
  queryFn: async () => required((await jobClient.GET("/api/v1/jobs")).data),
});

export const allJobsQueryOptions = queryOptions({
  queryKey: keys.admin.jobs,
  queryFn: async () => required((await jobClient.GET("/api/v1/admin/jobs")).data),
});

export function jobQueryOptions(jobId: string) {
  return queryOptions({
    queryKey: keys.job(jobId),
    queryFn: async () =>
      required((await jobClient.GET("/api/v1/jobs/{jobId}", { params: { path: { jobId } } })).data),
  });
}

export const providersQueryOptions = queryOptions({
  queryKey: keys.providers,
  queryFn: async () => required((await jobClient.GET("/api/v1/providers")).data),
  staleTime: 60_000,
});

export const historyQueryOptions = queryOptions({
  queryKey: keys.history,
  queryFn: async () => required((await jobClient.GET("/api/v1/jobs/history")).data),
  staleTime: 60_000,
});

export function hostersQueryOptions(providerId: string) {
  return queryOptions({
    queryKey: keys.hosters(providerId),
    queryFn: async () =>
      required(
        (
          await jobClient.GET("/api/v1/providers/{providerId}/hosters", {
            params: { path: { providerId } },
          })
        ).data,
      ),
    staleTime: 10 * 60_000,
    retry: false,
  });
}

export function cachedQueryOptions(providerId: string, fingerprint: string) {
  return queryOptions({
    queryKey: keys.cached(providerId, fingerprint),
    queryFn: async () => {
      const data = required(
        (
          await jobClient.POST("/api/v1/providers/{providerId}/cached-availability", {
            params: { path: { providerId } },
            body: { fingerprints: [fingerprint] },
          })
        ).data,
      );
      return data.cached[fingerprint] === true;
    },
    staleTime: 5 * 60_000,
    retry: false,
  });
}
