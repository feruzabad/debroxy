import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { meFixture, providersFixture } from "../mocks/fixtures";
import { server } from "../mocks/server";
import { renderAppAt } from "./render";

describe("admin guard", () => {
  it("shows the 403 page to a non-admin on an admin route", async () => {
    server.use(
      http.get("/api/v1/me", () => HttpResponse.json(meFixture())),
      http.get("/api/v1/jobs", () => HttpResponse.json({ jobs: [], stale: false })),
      http.get("/api/v1/providers", () => HttpResponse.json(providersFixture())),
    );
    renderAppAt("/admin/users");
    expect(
      await screen.findByRole("heading", { level: 1, name: "Admins only" }),
    ).toBeInTheDocument();
    expect(screen.getByText("You don't have access to this page.")).toBeInTheDocument();
  });
});
