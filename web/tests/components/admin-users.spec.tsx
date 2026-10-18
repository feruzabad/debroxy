import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import {
  adminMeFixture,
  adminUserFixture,
  providersFixture,
  slotQuotaFixture,
} from "../mocks/fixtures";
import { server } from "../mocks/server";
import { renderAppAt } from "./render";

function shellHandlers() {
  server.use(
    http.get("/api/v1/me", () => HttpResponse.json(adminMeFixture())),
    http.get("/api/v1/jobs", () => HttpResponse.json({ jobs: [], stale: false })),
    http.get("/api/v1/providers", () => HttpResponse.json(providersFixture())),
  );
}

describe("admin users", () => {
  it("lists users and marks the current admin", async () => {
    shellHandlers();
    server.use(
      http.get("/api/v1/admin/users", () =>
        HttpResponse.json({
          users: [
            adminUserFixture({ id: "u-1", username: "alice", role: "admin" }),
            adminUserFixture({ id: "u-2", username: "bob", role: "user" }),
          ],
        }),
      ),
    );
    renderAppAt("/admin/users");
    expect(await screen.findByRole("heading", { level: 1, name: "Users" })).toBeInTheDocument();
    expect(await screen.findByText("bob")).toBeInTheDocument();
    expect(screen.getByText("alice (you)")).toBeInTheDocument();
  });

  it("validates a too-short username on create", async () => {
    const user = userEvent.setup();
    shellHandlers();
    server.use(
      http.get("/api/v1/admin/users", () => HttpResponse.json({ users: [adminUserFixture()] })),
    );
    renderAppAt("/admin/users/new");
    await screen.findByText("New user");
    await user.type(screen.getByLabelText("Username"), "ab");
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(
      await screen.findByText("Use 3–32 characters: letters, digits, dot, dash, underscore."),
    ).toBeInTheDocument();
  });

  it("shows an inline error for a taken username", async () => {
    const user = userEvent.setup();
    shellHandlers();
    server.use(
      http.get("/api/v1/admin/users", () => HttpResponse.json({ users: [adminUserFixture()] })),
      http.post("/api/v1/admin/users", () =>
        HttpResponse.json({ slug: "username-taken", message: "" }, { status: 409 }),
      ),
    );
    renderAppAt("/admin/users/new");
    await screen.findByText("New user");
    await user.type(screen.getByLabelText("Username"), "bob");
    await user.type(
      screen.getByLabelText("Password", { selector: "input" }),
      "correct-horse-battery",
    );
    await user.type(screen.getByLabelText("Repeat"), "correct-horse-battery");
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByText("Pick another username.")).toBeInTheDocument();
  });

  it("shows the last-admin alert when demoting the last admin", async () => {
    const user = userEvent.setup();
    shellHandlers();
    server.use(
      http.get("/api/v1/admin/users/u-2", () =>
        HttpResponse.json(adminUserFixture({ id: "u-2", username: "bob", role: "admin" })),
      ),
      http.get("/api/v1/admin/users/u-2/slot-quota", () => HttpResponse.json(slotQuotaFixture())),
      http.patch("/api/v1/admin/users/u-2", () =>
        HttpResponse.json({ slug: "last-admin", message: "" }, { status: 409 }),
      ),
    );
    renderAppAt("/admin/users/u-2");
    await screen.findByRole("heading", { level: 1, name: /bob · created/ });
    await user.click(screen.getByRole("radio", { name: "User" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(
      await screen.findByText("bob is the last active admin. Make someone else an admin first."),
    ).toBeInTheDocument();
  });

  it("shows the slot quota view", async () => {
    shellHandlers();
    server.use(
      http.get("/api/v1/admin/users/u-2", () =>
        HttpResponse.json(adminUserFixture({ id: "u-2", username: "bob", role: "user" })),
      ),
      http.get("/api/v1/admin/users/u-2/slot-quota", () =>
        HttpResponse.json(slotQuotaFixture({ maxActiveJobs: 3, isDefault: true, slotsUsed: 1 })),
      ),
    );
    renderAppAt("/admin/users/u-2");
    expect(await screen.findByText("active jobs (default) · 1 in use")).toBeInTheDocument();
  });
});
