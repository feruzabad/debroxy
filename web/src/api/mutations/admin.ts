import { identityClient, jobClient } from "@/api/client";
import { ApiError } from "@/api/errors";
import type { components as identityComponents } from "@/api/generated/identity";

export type CreateUserRequest = identityComponents["schemas"]["CreateUserRequest"];
export type UpdateUserRequest = identityComponents["schemas"]["UpdateUserRequest"];
export type OIDCIdentity = identityComponents["schemas"]["OIDCIdentity"];
export type AdminUser = identityComponents["schemas"]["User"];

function required<T>(data: T | undefined): T {
  if (data === undefined) {
    throw new ApiError({ status: 0, slug: "network-error", message: "" });
  }
  return data;
}

export async function createUser(request: CreateUserRequest): Promise<AdminUser> {
  return required((await identityClient.POST("/api/v1/admin/users", { body: request })).data);
}

export async function updateUser(userId: string, request: UpdateUserRequest): Promise<AdminUser> {
  return required(
    (
      await identityClient.PATCH("/api/v1/admin/users/{userId}", {
        params: { path: { userId } },
        body: request,
      })
    ).data,
  );
}

export async function resetUserPassword(userId: string, password: string): Promise<void> {
  await identityClient.PUT("/api/v1/admin/users/{userId}/password", {
    params: { path: { userId } },
    body: { password },
  });
}

export async function linkOIDCIdentity(userId: string, identity: OIDCIdentity): Promise<AdminUser> {
  return required(
    (
      await identityClient.PUT("/api/v1/admin/users/{userId}/oidc-identity", {
        params: { path: { userId } },
        body: identity,
      })
    ).data,
  );
}

export async function unlinkOIDCIdentity(userId: string): Promise<AdminUser> {
  return required(
    (
      await identityClient.DELETE("/api/v1/admin/users/{userId}/oidc-identity", {
        params: { path: { userId } },
      })
    ).data,
  );
}

export async function setSlotQuota(userId: string, maxActiveJobs: number): Promise<void> {
  await jobClient.PUT("/api/v1/admin/users/{userId}/slot-quota", {
    params: { path: { userId } },
    body: { maxActiveJobs },
  });
}

export async function probeProviderCapabilities(providerId: string): Promise<void> {
  await jobClient.POST("/api/v1/admin/providers/{providerId}/probe-capabilities", {
    params: { path: { providerId } },
  });
}
