import createClient, { type Middleware } from "openapi-fetch";
import { ApiError, parseError } from "./errors";
import type { paths as DeliveryPaths } from "./generated/delivery";
import type { paths as IdentityPaths } from "./generated/identity";
import type { paths as JobPaths } from "./generated/job";

type SessionEndedHandler = () => void;

let sessionEndedHandler: SessionEndedHandler | undefined;
let sessionEndedFired = false;
let justLoggedOut = false;

export function setSessionEndedHandler(handler: SessionEndedHandler): void {
  sessionEndedHandler = handler;
}

export function resetSessionEnded(): void {
  sessionEndedFired = false;
  justLoggedOut = false;
}

/** Marks the user as having just logged out, to suppress the §7.4 toast. */
export function markJustLoggedOut(): void {
  justLoggedOut = true;
}

export function wasJustLoggedOut(): boolean {
  return justLoggedOut;
}

function requestPathname(input: RequestInfo | URL): string | null {
  try {
    return input instanceof URL
      ? input.pathname
      : new URL(typeof input === "string" ? input : input.url, "http://localhost").pathname;
  } catch {
    return null;
  }
}

function isPostTo(input: RequestInfo | URL, init: RequestInit | undefined, path: string): boolean {
  const method = (
    init?.method ??
    (typeof Request !== "undefined" && input instanceof Request ? input.method : undefined)
  )?.toUpperCase();
  return method === "POST" && requestPathname(input) === path;
}

export function isLoginRequest(input: RequestInfo | URL, init?: RequestInit): boolean {
  return isPostTo(input, init, "/api/v1/auth/login");
}

/** Logout's 401 is handled by the logout flow, not the session-ended flow (§5.2.1). */
function isLogoutRequest(input: RequestInfo | URL, init?: RequestInit): boolean {
  return isPostTo(input, init, "/api/v1/auth/logout");
}

export async function wrappedFetch(
  input: RequestInfo | URL,
  init?: RequestInit,
): Promise<Response> {
  try {
    const response = await globalThis.fetch(input, init);
    const isOnLoginPage =
      typeof window !== "undefined" && window.location?.pathname?.startsWith("/login");
    if (
      response.status === 401 &&
      !isLoginRequest(input, init) &&
      !isLogoutRequest(input, init) &&
      !sessionEndedFired &&
      !isOnLoginPage
    ) {
      sessionEndedFired = true;
      sessionEndedHandler?.();
    }
    if (response.ok && isLoginRequest(input, init)) {
      resetSessionEnded();
    }
    return response;
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") {
      throw error;
    }
    throw new ApiError({ status: 0, slug: "network-error", message: "" });
  }
}

const errorMiddleware: Middleware = {
  onResponse: async ({ response }) => {
    if (!response.ok) {
      throw await parseError(response);
    }
    return response;
  },
};

export const identityClient = createClient<IdentityPaths>({
  baseUrl: "",
  credentials: "same-origin",
  fetch: wrappedFetch,
});
identityClient.use(errorMiddleware);

export const jobClient = createClient<JobPaths>({
  baseUrl: "",
  credentials: "same-origin",
  fetch: wrappedFetch,
});
jobClient.use(errorMiddleware);

export const deliveryClient = createClient<DeliveryPaths>({
  baseUrl: "",
  credentials: "same-origin",
  fetch: wrappedFetch,
});
deliveryClient.use(errorMiddleware);
