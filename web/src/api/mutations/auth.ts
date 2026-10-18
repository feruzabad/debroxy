import { identityClient, resetSessionEnded } from "@/api/client";

export async function logIn(username: string, password: string): Promise<void> {
  await identityClient.POST("/api/v1/auth/login", { body: { username, password } });
  resetSessionEnded();
}

export async function logOut(): Promise<void> {
  await identityClient.POST("/api/v1/auth/logout");
}
