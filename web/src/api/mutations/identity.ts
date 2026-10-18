import { identityClient } from "@/api/client";

/** Change the own password; the server revokes every other session (§5.9). */
export async function changeOwnPassword(
  currentPassword: string,
  newPassword: string,
): Promise<void> {
  await identityClient.PUT("/api/v1/me/password", {
    body: { currentPassword, newPassword },
  });
}
