import { jobClient } from "@/api/client";
import { ApiError } from "@/api/errors";
import type { components } from "@/api/generated/job";

export type SubmitJobRequest = components["schemas"]["SubmitJobRequest"];
export type Job = components["schemas"]["Job"];
export type HistoryEntry = components["schemas"]["HistoryEntry"];
export type SubmissionPending = components["schemas"]["SubmissionPending"];

export type SubmitOutcome =
  | { kind: "created"; job: Job }
  | { kind: "pending"; job: Job }
  | { kind: "released-before"; entry: HistoryEntry };

/** submitJob with its three success shapes (§5.5.5). */
export async function submitJob(request: SubmitJobRequest): Promise<SubmitOutcome> {
  const { data, response } = await jobClient.POST("/api/v1/jobs", { body: request });
  if (data === undefined) {
    throw new ApiError({ status: 0, slug: "network-error", message: "" });
  }
  switch (response.status) {
    case 201:
      return { kind: "created", job: data as Job };
    case 202:
      return { kind: "pending", job: (data as SubmissionPending).job };
    default:
      return { kind: "released-before", entry: data as HistoryEntry };
  }
}

export async function renameJob(jobId: string, displayName: string): Promise<Job> {
  const { data } = await jobClient.PATCH("/api/v1/jobs/{jobId}", {
    params: { path: { jobId } },
    body: { displayName },
  });
  if (data === undefined) {
    throw new ApiError({ status: 0, slug: "network-error", message: "" });
  }
  return data;
}

export async function releaseJob(jobId: string, admin = false): Promise<void> {
  if (admin) {
    await jobClient.DELETE("/api/v1/admin/jobs/{jobId}", { params: { path: { jobId } } });
    return;
  }
  await jobClient.DELETE("/api/v1/jobs/{jobId}", { params: { path: { jobId } } });
}

export async function refreshProvider(providerId: string): Promise<{ stale: boolean }> {
  const { data } = await jobClient.POST("/api/v1/providers/{providerId}/refresh", {
    params: { path: { providerId } },
  });
  if (data === undefined) {
    throw new ApiError({ status: 0, slug: "network-error", message: "" });
  }
  return data;
}
