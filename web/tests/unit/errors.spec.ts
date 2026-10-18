import { describe, expect, it } from "vitest";
import { ApiError, ERROR_SPECS, errorSpec, parseError } from "@/api/errors";

const SERVER_SLUGS = [
  "invalid-request",
  "invalid-source",
  "upload-too-large",
  "unsupported-hoster",
  "provider-rejected",
  "invalid-username",
  "invalid-password",
  "invalid-role",
  "invalid-status",
  "invalid-oidc-identity",
  "wrong-current-password",
  "no-login-method",
  "invalid-display-name",
  "invalid-slot-quota",
  "webhook-too-large",
  "invalid-credentials",
  "authentication-required",
  "session-expired",
  "forbidden",
  "invalid-signed-link",
  "signed-link-expired",
  "job-not-found",
  "provider-not-found",
  "user-not-found",
  "not-found",
  "job-already-exists",
  "job-id-conflict",
  "remote-item-being-deleted",
  "remote-item-unavailable",
  "remote-item-not-ready",
  "target-not-available",
  "last-admin",
  "username-taken",
  "oidc-identity-taken",
  "provider-disabled",
  "provider-account-limit",
  "capability-unavailable-for-plan",
  "bootstrap-not-allowed",
  "slot-quota-exceeded",
  "submit-rate-exceeded",
  "refresh-rate-exceeded",
  "login-throttled",
  "capability-not-supported",
  "capability-not-enabled",
  "oidc-not-enabled",
  "oidc-provider-unavailable",
  "provider-unavailable",
  "too-many-streams",
  "internal-error",
  "submission-pending",
  "provider-state-stale",
];

describe("errorSpec", () => {
  it("maps every server slug to an entry", () => {
    for (const slug of SERVER_SLUGS) {
      expect(ERROR_SPECS[slug], `missing entry for ${slug}`).toBeDefined();
    }
  });

  it("maps the client network-error slug", () => {
    expect(ERROR_SPECS["network-error"]).toEqual({
      title: "Can't reach debroxy.",
      text: "Check your connection and try again.",
      behaviour: "toast",
    });
  });

  it.each([
    ["slot-quota-exceeded", "No free slots.", "alert"],
    ["submit-rate-exceeded", "Submit limit reached.", "countdown"],
    ["provider-rejected", "Refused by {provider}.", "alert"],
    ["job-not-found", "Job not found.", "page"],
    ["internal-error", "Something went wrong.", "toast"],
  ])("%s → %s / %s", (slug, title, behaviour) => {
    expect(errorSpec(slug).title).toBe(title);
    expect(errorSpec(slug).behaviour).toBe(behaviour);
  });

  it("falls back to a generic title for an unknown slug", () => {
    expect(errorSpec("totally-unknown")).toEqual({
      title: "Error (totally-unknown).",
      text: "{message}",
      behaviour: "toast",
    });
  });

  it("formats http-{status} slugs", () => {
    expect(errorSpec("http-503")).toEqual({
      title: "Unexpected response (503).",
      text: "",
      behaviour: "toast",
    });
  });
});

describe("parseError", () => {
  it("reads slug, message and retryAfter from the JSON body", async () => {
    const error = await parseError(
      new Response(
        JSON.stringify({ slug: "submit-rate-exceeded", message: "later", retryAfter: 30 }),
        {
          status: 429,
        },
      ),
    );
    expect(error).toBeInstanceOf(ApiError);
    expect(error.status).toBe(429);
    expect(error.slug).toBe("submit-rate-exceeded");
    expect(error.message).toBe("later");
    expect(error.retryAfter).toBe(30);
  });

  it("prefers the body retryAfter over the header", async () => {
    const error = await parseError(
      new Response(JSON.stringify({ slug: "x", message: "y", retryAfter: 30 }), {
        status: 429,
        headers: { "retry-after": "7" },
      }),
    );
    expect(error.retryAfter).toBe(30);
  });

  it("falls back to the Retry-After header when the body has none", async () => {
    const error = await parseError(
      new Response(JSON.stringify({ slug: "provider-unavailable", message: "down" }), {
        status: 502,
        headers: { "retry-after": "7" },
      }),
    );
    expect(error.retryAfter).toBe(7);
  });

  it("uses http-{status} for a non-JSON body", async () => {
    const error = await parseError(new Response("boom", { status: 500 }));
    expect(error.slug).toBe("http-500");
    expect(error.status).toBe(500);
    expect(error.message).toBe("");
  });

  it("reads only the Retry-After header for a non-JSON body", async () => {
    const error = await parseError(
      new Response("boom", { status: 503, headers: { "retry-after": "5" } }),
    );
    expect(error.slug).toBe("http-503");
    expect(error.retryAfter).toBe(5);
  });

  it("parses an HTTP-date Retry-After header", async () => {
    const date = new Date(Date.now() + 30_000).toUTCString();
    const error = await parseError(
      new Response(JSON.stringify({ slug: "provider-unavailable", message: "down" }), {
        status: 502,
        headers: { "retry-after": date },
      }),
    );
    expect(error.retryAfter).toBeGreaterThanOrEqual(29);
    expect(error.retryAfter).toBeLessThanOrEqual(30);
  });

  it("clamps a past HTTP-date Retry-After header to 1", async () => {
    const date = new Date(Date.now() - 60_000).toUTCString();
    const error = await parseError(
      new Response(JSON.stringify({ slug: "provider-unavailable", message: "down" }), {
        status: 502,
        headers: { "retry-after": date },
      }),
    );
    expect(error.retryAfter).toBe(1);
  });

  it("ignores a non-numeric body retryAfter", async () => {
    const error = await parseError(
      new Response(
        JSON.stringify({ slug: "submit-rate-exceeded", message: "later", retryAfter: "30" }),
        {
          status: 429,
        },
      ),
    );
    expect(error.retryAfter).toBeUndefined();
  });

  it("falls back to the header when the body retryAfter is non-numeric", async () => {
    const error = await parseError(
      new Response(
        JSON.stringify({ slug: "submit-rate-exceeded", message: "later", retryAfter: "30" }),
        {
          status: 429,
          headers: { "retry-after": "7" },
        },
      ),
    );
    expect(error.retryAfter).toBe(7);
  });
});
