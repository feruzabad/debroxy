// The client's only persisted preference besides the theme (docs/frontend/revision2.md §3.5).
// Every access is wrapped: a blocked storage falls back to the default.

const LAST_PROVIDER_KEY = "debroxy.lastProvider";

export function readLastProvider(): string | null {
  try {
    return window.localStorage.getItem(LAST_PROVIDER_KEY);
  } catch {
    return null;
  }
}

export function storeLastProvider(providerId: string): void {
  try {
    window.localStorage.setItem(LAST_PROVIDER_KEY, providerId);
  } catch {
    // ignored: the preference is a convenience
  }
}
