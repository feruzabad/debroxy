import { afterEach, describe, expect, it, vi } from "vitest";
import {
  isLoginRequest,
  resetSessionEnded,
  setSessionEndedHandler,
  wrappedFetch,
} from "@/api/client";

describe("isLoginRequest", () => {
  describe("string inputs", () => {
    it("returns true for relative string pathname with POST method", () => {
      expect(isLoginRequest("/api/v1/auth/login", { method: "POST" })).toBe(true);
    });

    it("returns true for case-insensitive method", () => {
      expect(isLoginRequest("/api/v1/auth/login", { method: "post" })).toBe(true);
    });

    it("returns true for absolute URL string with POST method", () => {
      expect(isLoginRequest("http://localhost:8080/api/v1/auth/login", { method: "POST" })).toBe(
        true,
      );
      expect(isLoginRequest("https://example.com/api/v1/auth/login", { method: "POST" })).toBe(
        true,
      );
    });

    it("returns true with query parameters or hash in string", () => {
      expect(isLoginRequest("/api/v1/auth/login?redirect=%2F", { method: "POST" })).toBe(true);
      expect(isLoginRequest("/api/v1/auth/login#section", { method: "POST" })).toBe(true);
    });

    it("returns false for non-POST method or missing method", () => {
      expect(isLoginRequest("/api/v1/auth/login", { method: "GET" })).toBe(false);
      expect(isLoginRequest("/api/v1/auth/login")).toBe(false);
    });

    it("returns false for different pathname", () => {
      expect(isLoginRequest("/api/v1/me", { method: "POST" })).toBe(false);
      expect(isLoginRequest("/api/v1/auth/logout", { method: "POST" })).toBe(false);
    });
  });

  describe("URL inputs", () => {
    it("returns true for URL instance with pathname /api/v1/auth/login and POST method", () => {
      const url = new URL("http://localhost:8080/api/v1/auth/login");
      expect(isLoginRequest(url, { method: "POST" })).toBe(true);
    });

    it("returns true for URL with query params", () => {
      const url = new URL("http://localhost:8080/api/v1/auth/login?redirect=/");
      expect(isLoginRequest(url, { method: "POST" })).toBe(true);
    });

    it("returns false for URL with non-POST method or missing init", () => {
      const url = new URL("http://localhost:8080/api/v1/auth/login");
      expect(isLoginRequest(url, { method: "GET" })).toBe(false);
      expect(isLoginRequest(url)).toBe(false);
    });

    it("returns false for URL with different pathname", () => {
      const url = new URL("http://localhost:8080/api/v1/me");
      expect(isLoginRequest(url, { method: "POST" })).toBe(false);
    });
  });

  describe("Request inputs", () => {
    it("returns true for Request object with POST method", () => {
      const req = new Request("http://localhost:8080/api/v1/auth/login", { method: "POST" });
      expect(isLoginRequest(req)).toBe(true);
    });

    it("returns true when init.method overrides Request method", () => {
      const req = new Request("http://localhost:8080/api/v1/auth/login", { method: "GET" });
      expect(isLoginRequest(req, { method: "POST" })).toBe(true);
    });

    it("returns false for Request object with GET or default method", () => {
      const reqGet = new Request("http://localhost:8080/api/v1/auth/login", { method: "GET" });
      expect(isLoginRequest(reqGet)).toBe(false);
      const reqDefault = new Request("http://localhost:8080/api/v1/auth/login");
      expect(isLoginRequest(reqDefault)).toBe(false);
    });

    it("returns false for Request object with different pathname", () => {
      const req = new Request("http://localhost:8080/api/v1/me", { method: "POST" });
      expect(isLoginRequest(req)).toBe(false);
    });
  });
});

describe("session ended guard", () => {
  afterEach(() => {
    resetSessionEnded();
  });

  it("does not trigger sessionEndedHandler on 401 for login requests", async () => {
    const handler = vi.fn();
    setSessionEndedHandler(handler);

    const req = new Request("http://localhost:8080/api/v1/auth/login", { method: "POST" });
    const fakeFetch = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response(null, { status: 401 }));

    await wrappedFetch(req);
    expect(handler).not.toHaveBeenCalled();

    fakeFetch.mockRestore();
  });

  it("resets session ended guard on successful login", async () => {
    const handler = vi.fn();
    setSessionEndedHandler(handler);

    // Trigger session ended first via a non-login 401
    const fake401 = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response(null, { status: 401 }));
    await wrappedFetch("http://localhost:8080/api/v1/me", { method: "GET" });
    expect(handler).toHaveBeenCalledTimes(1);

    // Another 401 should not trigger handler again while guard is tripped
    await wrappedFetch("http://localhost:8080/api/v1/me", { method: "GET" });
    expect(handler).toHaveBeenCalledTimes(1);

    fake401.mockRestore();

    // Successful login resets guard
    const fake200 = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response("{}", { status: 200 }));
    await wrappedFetch(new Request("http://localhost:8080/api/v1/auth/login", { method: "POST" }));
    fake200.mockRestore();

    // Next 401 on protected endpoint should fire handler again
    const fake401Again = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response(null, { status: 401 }));
    await wrappedFetch("http://localhost:8080/api/v1/me", { method: "GET" });
    expect(handler).toHaveBeenCalledTimes(2);
    fake401Again.mockRestore();
  });
});
