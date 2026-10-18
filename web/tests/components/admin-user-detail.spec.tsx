import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import {
  type AdminUser,
  adminMeFixture,
  adminUserFixture,
  configFixture,
  providersFixture,
  slotQuotaFixture,
} from "../mocks/fixtures";
import { server } from "../mocks/server";
import { renderAppAt } from "./render";

const bob = (overrides: Partial<AdminUser> = {}) =>
  adminUserFixture({ id: "u-2", username: "bob", role: "user", ...overrides });

function detailHandlers(user: AdminUser = bob(), quota = slotQuotaFixture()) {
  server.use(
    http.get("/api/v1/me", () => HttpResponse.json(adminMeFixture())),
    http.get("/api/v1/jobs", () => HttpResponse.json({ jobs: [], stale: false })),
    http.get("/api/v1/providers", () => HttpResponse.json(providersFixture())),
    http.get(`/api/v1/admin/users/${user.id}`, () => HttpResponse.json(user)),
    http.get(`/api/v1/admin/users/${user.id}/slot-quota`, () => HttpResponse.json(quota)),
  );
}

function oidcOn() {
  server.use(
    http.get("/api/v1/config", () => HttpResponse.json({ ...configFixture, oidcEnabled: true })),
  );
}

async function openBob() {
  renderAppAt("/admin/users/u-2");
  await screen.findByRole("heading", { level: 1, name: /bob · created/ });
}

describe("admin user detail", () => {
  it("shows the not-found state", async () => {
    detailHandlers();
    server.use(
      http.get("/api/v1/admin/users/u-9", () =>
        HttpResponse.json({ slug: "user-not-found", message: "" }, { status: 404 }),
      ),
      http.get("/api/v1/admin/users/u-9/slot-quota", () =>
        HttpResponse.json({ slug: "user-not-found", message: "" }, { status: 404 }),
      ),
    );
    renderAppAt("/admin/users/u-9");
    expect(await screen.findByText("This user doesn't exist.")).toBeInTheDocument();
  });

  it("confirms before disabling and sends only the status", async () => {
    const user = userEvent.setup();
    let body: unknown;
    detailHandlers();
    server.use(
      http.patch("/api/v1/admin/users/u-2", async ({ request }) => {
        body = await request.json();
        return HttpResponse.json(bob({ status: "disabled" }));
      }),
    );
    await openBob();
    await user.click(screen.getByRole("radio", { name: "Disabled" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(
      within(dialog).getByText(
        "They're logged out at once and their download links stop working. Their jobs stay.",
      ),
    ).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Disable" }));
    expect(await screen.findByText("Saved.")).toBeInTheDocument();
    expect(body).toEqual({ status: "disabled" });
  });

  it("warns when the admin edits their own account", async () => {
    detailHandlers(adminUserFixture({ id: "u-1", username: "alice", role: "admin" }));
    renderAppAt("/admin/users/u-1");
    expect(await screen.findByText("You're changing your own account.")).toBeInTheDocument();
  });

  it("resets the password with a generated one", async () => {
    const user = userEvent.setup();
    let body: { password?: string } = {};
    detailHandlers();
    server.use(
      http.put("/api/v1/admin/users/u-2/password", async ({ request }) => {
        body = (await request.json()) as { password: string };
        return new HttpResponse(null, { status: 204 });
      }),
    );
    await openBob();
    await user.click(screen.getByRole("button", { name: "Reset password…" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Generate" }));
    await user.click(within(dialog).getByRole("button", { name: "Reset" }));
    expect(
      await screen.findByText("Password reset. bob was logged out everywhere."),
    ).toBeInTheDocument();
    expect(body.password).toHaveLength(20);
  });

  it("rejects a repeat that doesn't match", async () => {
    const user = userEvent.setup();
    detailHandlers();
    await openBob();
    await user.click(screen.getByRole("button", { name: "Reset password…" }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText("Password"), "correct-horse-battery");
    await user.type(within(dialog).getByLabelText("Repeat"), "correct-horse-batterz");
    await user.click(within(dialog).getByRole("button", { name: "Reset" }));
    expect(await within(dialog).findByText("The passwords don't match.")).toBeInTheDocument();
  });

  it("disables the single sign-on buttons when the server has it off", async () => {
    detailHandlers(
      bob({ oidcLinked: true, oidcIdentity: { issuer: "https://id.example", subject: "s-1" } }),
    );
    await openBob();
    expect(screen.getByText("Single sign-on is off on this server.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Unlink" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Change…" })).toBeDisabled();
  });

  it("shows the identity conflict when linking", async () => {
    const user = userEvent.setup();
    oidcOn();
    detailHandlers();
    server.use(
      http.put("/api/v1/admin/users/u-2/oidc-identity", () =>
        HttpResponse.json({ slug: "oidc-identity-taken", message: "" }, { status: 409 }),
      ),
    );
    await openBob();
    expect(screen.getByText("not linked")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Link…" }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText("Issuer"), "https://id.example");
    await user.type(within(dialog).getByLabelText("Subject"), "s-1");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(
      await within(dialog).findByText("Another user already has this identity."),
    ).toBeInTheDocument();
  });

  it("links an identity and shows it", async () => {
    const user = userEvent.setup();
    oidcOn();
    detailHandlers();
    server.use(
      http.put("/api/v1/admin/users/u-2/oidc-identity", async ({ request }) =>
        HttpResponse.json(
          bob({
            oidcLinked: true,
            oidcIdentity: (await request.json()) as { issuer: string; subject: string },
          }),
        ),
      ),
    );
    await openBob();
    await user.click(screen.getByRole("button", { name: "Link…" }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText("Issuer"), "https://id.example");
    await user.type(within(dialog).getByLabelText("Subject"), "s-1");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await screen.findByText("s-1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Change…" })).toBeEnabled();
  });

  it("explains why unlinking the last sign-in method fails", async () => {
    const user = userEvent.setup();
    oidcOn();
    detailHandlers(
      bob({
        passwordLogin: false,
        oidcLinked: true,
        oidcIdentity: { issuer: "https://id.example", subject: "s-1" },
      }),
    );
    server.use(
      http.delete("/api/v1/admin/users/u-2/oidc-identity", () =>
        HttpResponse.json({ slug: "no-login-method", message: "" }, { status: 400 }),
      ),
    );
    await openBob();
    expect(screen.getByText("none")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Unlink" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Unlink" }));
    expect(
      await screen.findByText("bob would have no way to sign in. Set a password first."),
    ).toBeInTheDocument();
  });

  it("confirms a quota below the slots in use, then sets it", async () => {
    const user = userEvent.setup();
    let body: unknown;
    detailHandlers(bob(), slotQuotaFixture({ maxActiveJobs: 3, isDefault: true, slotsUsed: 2 }));
    server.use(
      http.put("/api/v1/admin/users/u-2/slot-quota", async ({ request }) => {
        body = await request.json();
        return new HttpResponse(null, { status: 204 });
      }),
    );
    await openBob();
    const field = await screen.findByLabelText("Slot quota");
    await waitFor(() => expect(field).toHaveValue("3"));
    await user.clear(field);
    await user.type(field, "1");
    await user.click(screen.getByRole("button", { name: "Set" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(
      within(dialog).getByText(
        "bob already has 2 active jobs. They keep them, but can't add more until they're below 1.",
      ),
    ).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Set" }));
    expect(await screen.findByText("Quota set to 1.")).toBeInTheDocument();
    expect(body).toEqual({ maxActiveJobs: 1 });
  });

  it("rejects a negative quota inline without calling the server", async () => {
    const user = userEvent.setup();
    let called = false;
    detailHandlers();
    server.use(
      http.put("/api/v1/admin/users/u-2/slot-quota", () => {
        called = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    await openBob();
    const field = await screen.findByLabelText("Slot quota");
    await user.clear(field);
    await user.type(field, "-1");
    await user.click(screen.getByRole("button", { name: "Set" }));
    expect(await screen.findByText("Enter a whole number, 0 or more.")).toBeInTheDocument();
    expect(called).toBe(false);
  });

  it("shows invalid-slot-quota from the server inline", async () => {
    const user = userEvent.setup();
    detailHandlers(bob(), slotQuotaFixture({ slotsUsed: 0 }));
    server.use(
      http.put("/api/v1/admin/users/u-2/slot-quota", () =>
        HttpResponse.json({ slug: "invalid-slot-quota", message: "" }, { status: 400 }),
      ),
    );
    await openBob();
    const field = await screen.findByLabelText("Slot quota");
    await user.clear(field);
    await user.type(field, "100000");
    await user.click(screen.getByRole("button", { name: "Set" }));
    expect(await screen.findByText("Enter a whole number, 0 or more.")).toBeInTheDocument();
  });
});
