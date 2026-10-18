import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterAll, afterEach, beforeAll } from "vitest";
import { server } from "./mocks/server";

const NodeRequest = globalThis.Request;

// openapi-fetch builds a Request from the same-origin relative path (§3.3);
// Node's Request rejects relative URLs, so tests resolve them against the jsdom origin.
class AbsoluteUrlRequest extends NodeRequest {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(
      typeof input === "string" && input.startsWith("/")
        ? new URL(input, window.location.origin).toString()
        : input,
      init,
    );
  }
}
globalThis.Request = AbsoluteUrlRequest;

// jsdom lacks matchMedia; the theme sync (§3.6) and Radix components rely on it.
window.matchMedia =
  window.matchMedia ??
  ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }));

// jsdom has no EventSource; the shell's own-scope stream (§8.1) needs one to
// connect without throwing. It never emits, so the indicator stays "Connecting".
class MockEventSource {
  static instances: MockEventSource[] = [];
  url: string;
  private listeners = new Map<string, Array<(event: { data: string }) => void>>();

  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: (event: { data: string }) => void): void {
    const list = this.listeners.get(type) ?? [];
    list.push(listener);
    this.listeners.set(type, list);
  }

  close(): void {
    this.listeners.clear();
  }
}

globalThis.EventSource = MockEventSource as unknown as typeof EventSource;

beforeAll(() => server.listen({ onUnhandledFrame: "error" }));

afterEach(() => {
  cleanup();
  server.resetHandlers();
  MockEventSource.instances = [];
});

afterAll(() => server.close());
