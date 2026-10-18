import { queryOptions } from "@tanstack/react-query";
import { identityClient, jobClient } from "@/api/client";
import { ApiError } from "@/api/errors";
import type { components as identityComponents } from "@/api/generated/identity";
import type { components as jobComponents } from "@/api/generated/job";
import { keys } from "@/api/keys";

export type AdminUser = identityComponents["schemas"]["User"];
export type AdminUserList = identityComponents["schemas"]["UserList"];
export type SlotQuotaView = jobComponents["schemas"]["SlotQuota"];
export type UnmanagedItemList = jobComponents["schemas"]["UnmanagedItemList"];
export type OutboxMessageList = jobComponents["schemas"]["OutboxMessageList"];

function required<T>(data: T | undefined): T {
  if (data === undefined) {
    throw new ApiError({ status: 0, slug: "network-error", message: "" });
  }
  return data;
}

export const adminUsersQueryOptions = queryOptions({
  queryKey: keys.admin.users,
  queryFn: async () => required((await identityClient.GET("/api/v1/admin/users")).data),
  staleTime: 30_000,
});

export function adminUserQueryOptions(userId: string) {
  return queryOptions({
    queryKey: keys.admin.user(userId),
    queryFn: async () =>
      required(
        (
          await identityClient.GET("/api/v1/admin/users/{userId}", {
            params: { path: { userId } },
          })
        ).data,
      ),
    staleTime: 30_000,
  });
}

export function slotQuotaQueryOptions(userId: string) {
  return queryOptions({
    queryKey: keys.admin.quota(userId),
    queryFn: async () =>
      required(
        (
          await jobClient.GET("/api/v1/admin/users/{userId}/slot-quota", {
            params: { path: { userId } },
          })
        ).data,
      ),
    staleTime: 30_000,
  });
}

export const unmanagedItemsQueryOptions = queryOptions({
  queryKey: keys.admin.unmanaged,
  queryFn: async () => required((await jobClient.GET("/api/v1/admin/unmanaged-items")).data),
  staleTime: 60_000,
});

export const outboxMessagesQueryOptions = queryOptions({
  queryKey: keys.admin.outbox,
  queryFn: async () => required((await jobClient.GET("/api/v1/admin/outbox")).data),
  staleTime: 60_000,
});
