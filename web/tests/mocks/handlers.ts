import { HttpResponse, http } from "msw";
import type { components } from "@/api/generated/job";
import { configFixture, meFixture, providersFixture } from "./fixtures";

export const unauthorizedError: components["schemas"]["Error"] = {
  slug: "authentication-required",
  message: "Log in first.",
};

export const handlers = [
  http.get("/api/v1/config", () => HttpResponse.json(configFixture)),
  http.get("/api/v1/me", () => HttpResponse.json(unauthorizedError, { status: 401 })),
];

export const meHandler = http.get("/api/v1/me", () => HttpResponse.json(meFixture()));

export const providersHandler = http.get("/api/v1/providers", () =>
  HttpResponse.json(providersFixture()),
);
