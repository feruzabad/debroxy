import { z } from "zod";
import type { components } from "./generated/job";
import type { JobReleasedEvent, ProviderStaleEvent, ReadyEvent } from "./types-rev13";

export type Job = components["schemas"]["Job"];
export type HistoryEntry = components["schemas"]["HistoryEntry"];
export type QuotaUsage = components["schemas"]["QuotaUsage"];

// zod's `.optional()` types a missing field as `T | undefined`, which
// `exactOptionalPropertyTypes` rejects against the generated `field?: T`.
// The optional, non-nullable members of the generated `Job` (`ownerUsername`,
// `stale`, `delivery.bundle`) are widened here and only here; nullable-but-
// optional members use `.default(null)` and need no widening.
type JobSchemaType = Omit<Job, "ownerUsername" | "stale" | "delivery"> & {
  ownerUsername?: string | undefined;
  stale?: boolean | undefined;
  delivery: {
    perFile: boolean;
    bundle?: { format: "zip" | "rar"; rangeSupported: boolean } | undefined;
  };
};

const telemetrySchema = z.object({
  progress: z.number(),
  bytesPerSecond: z.number(),
  etaSeconds: z.number().nullable().default(null),
  peers: z.number().nullable().default(null),
  stalled: z.boolean(),
});

const bundleSchema = z.object({
  format: z.enum(["zip", "rar"]),
  rangeSupported: z.boolean(),
});

const deliverySchema = z.object({
  perFile: z.boolean(),
  bundle: bundleSchema.optional(),
});

const fileSchema = z.object({
  id: z.string(),
  path: z.string(),
  sizeBytes: z.number(),
});

export const jobSchema = z.object({
  id: z.string(),
  displayName: z.string(),
  provider: z.string(),
  kind: z.enum(["torrent", "nzb", "web"]),
  state: z.enum([
    "submitting",
    "queued",
    "resolving_metadata",
    "awaiting_file_selection",
    "downloading",
    "processing",
    "ready",
    "failed",
    "gone",
    "deleting",
    "deleted",
  ]),
  telemetry: telemetrySchema,
  sizeBytes: z.number(),
  expiresAt: z.string().nullable().default(null),
  shared: z.boolean(),
  slotCharged: z.boolean(),
  delivery: deliverySchema,
  files: z.array(fileSchema),
  createdAt: z.string(),
  failureReason: z.string().nullable().default(null),
  ownerUsername: z.string().optional(),
  stale: z.boolean().optional(),
}) satisfies z.ZodType<JobSchemaType>;

export const historyEntrySchema = z.object({
  jobId: z.string(),
  displayName: z.string(),
  provider: z.string(),
  kind: z.string(),
  name: z.string(),
  finalState: z.string(),
  failureReason: z.string().nullable().default(null),
  claimedAt: z.string(),
  releasedAt: z.string(),
}) satisfies z.ZodType<HistoryEntry>;

export const quotaUsageSchema = z.object({
  slotsUsed: z.number(),
  maxActiveJobs: z.number(),
}) satisfies z.ZodType<QuotaUsage>;

export const jobReleasedSchema = z.object({
  jobId: z.string(),
  historyEntry: historyEntrySchema,
}) satisfies z.ZodType<JobReleasedEvent>;

export const providerStaleSchema = z.object({
  providerId: z.string(),
  stale: z.boolean(),
}) satisfies z.ZodType<ProviderStaleEvent>;

export const readySchema = z.object({
  scope: z.enum(["own", "all"]),
}) satisfies z.ZodType<ReadyEvent>;
