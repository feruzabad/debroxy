export type LinkTargetKey = `file:${string}` | "bundle" | "all";

export const keys = {
  config: ["config"],
  me: ["me"],
  providers: ["providers"],
  hosters: (providerId: string) => ["hosters", providerId],
  jobs: ["jobs"],
  job: (jobId: string) => ["job", jobId],
  history: ["history"],
  cached: (providerId: string, fingerprint: string) => ["cached", providerId, fingerprint],
  links: (jobId: string, targetKey: LinkTargetKey) => ["links", jobId, targetKey],
  admin: {
    users: ["admin", "users"],
    user: (userId: string) => ["admin", "user", userId],
    quota: (userId: string) => ["admin", "quota", userId],
    jobs: ["admin", "jobs"],
    unmanaged: ["admin", "unmanaged"],
    outbox: ["admin", "outbox"],
  },
} as const;
