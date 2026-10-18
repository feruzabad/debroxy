import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { meFixture, providersFixture } from "../mocks/fixtures";
import { server } from "../mocks/server";
import { renderAppAt } from "./render";

function accountHandlers() {
  server.use(
    http.get("/api/v1/me", () => HttpResponse.json(meFixture())),
    http.get("/api/v1/jobs", () => HttpResponse.json({ jobs: [], stale: false })),
    http.get("/api/v1/providers", () => HttpResponse.json(providersFixture())),
  );
}

describe("account", () => {
  it("validates a too-short new password", async () => {
    const user = userEvent.setup();
    accountHandlers();
    renderAppAt("/account");
    await screen.findByRole("heading", { level: 1, name: "Account" });
    await user.type(screen.getByLabelText("Current password"), "oldpass");
    await user.type(screen.getByLabelText(/New password/), "short");
    await user.type(screen.getByLabelText("Repeat new"), "short");
    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByText("Use at least 12 characters.")).toBeInTheDocument();
  });

  it("shows an inline error for a wrong current password", async () => {
    const user = userEvent.setup();
    accountHandlers();
    server.use(
      http.put("/api/v1/me/password", () =>
        HttpResponse.json({ slug: "wrong-current-password", message: "wrong" }, { status: 400 }),
      ),
    );
    renderAppAt("/account");
    await screen.findByRole("heading", { level: 1, name: "Account" });
    await user.type(screen.getByLabelText("Current password"), "wrongpass");
    await user.type(screen.getByLabelText(/New password/), "correct-horse-battery");
    await user.type(screen.getByLabelText("Repeat new"), "correct-horse-battery");
    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByText("That isn't your current password.")).toBeInTheDocument();
  });

  it("hides the password form for single sign-on only users", async () => {
    accountHandlers();
    server.use(
      http.get("/api/v1/me", () =>
        HttpResponse.json(meFixture({ user: { ...meFixture().user, passwordLogin: false } })),
      ),
    );
    renderAppAt("/account");
    expect(
      await screen.findByText("You sign in with single sign-on. You have no password here."),
    ).toBeInTheDocument();
  });

  it("shows the account facts", async () => {
    accountHandlers();
    renderAppAt("/account");
    await screen.findByRole("heading", { level: 1, name: "Account" });
    expect(screen.getByText(/alice · User · active · member since/)).toBeInTheDocument();
    expect(screen.getByText(/Sign-in methods: Password ✓/)).toBeInTheDocument();
    expect(screen.getByText(/Session: idle logout/)).toBeInTheDocument();
    expect(
      screen.getByText(
        (_, element) =>
          element?.tagName === "P" &&
          element.textContent === "Slots: 2 of 3 · Submits: 3 of 10 this hour · cached 0 of 60",
      ),
    ).toBeInTheDocument();
  });

  it("changes the password", async () => {
    const user = userEvent.setup();
    let body: unknown;
    accountHandlers();
    server.use(
      http.put("/api/v1/me/password", async ({ request }) => {
        body = await request.json();
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderAppAt("/account");
    await screen.findByRole("heading", { level: 1, name: "Account" });
    await user.type(screen.getByLabelText("Current password"), "old-password-1");
    await user.type(screen.getByLabelText(/New password/), "correct-horse-battery");
    await user.type(screen.getByLabelText("Repeat new"), "correct-horse-battery");
    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(
      await screen.findByText("Password changed. Other sessions were logged out."),
    ).toBeInTheDocument();
    expect(body).toEqual({
      currentPassword: "old-password-1",
      newPassword: "correct-horse-battery",
    });
  });

  it("rejects a repeat that doesn't match", async () => {
    const user = userEvent.setup();
    accountHandlers();
    renderAppAt("/account");
    await screen.findByRole("heading", { level: 1, name: "Account" });
    await user.type(screen.getByLabelText("Current password"), "old-password-1");
    await user.type(screen.getByLabelText(/New password/), "correct-horse-battery");
    await user.type(screen.getByLabelText("Repeat new"), "correct-horse-batterz");
    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByText("The passwords don't match.")).toBeInTheDocument();
  });

  it("rejects a password over 1024 bytes", async () => {
    const user = userEvent.setup();
    accountHandlers();
    renderAppAt("/account");
    await screen.findByRole("heading", { level: 1, name: "Account" });
    const long = "é".repeat(513); // 513 characters, 1026 bytes
    await user.type(screen.getByLabelText("Current password"), "old-password-1");
    await user.click(screen.getByLabelText(/New password/));
    await user.paste(long);
    await user.click(screen.getByLabelText("Repeat new"));
    await user.paste(long);
    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByText("That's too long.")).toBeInTheDocument();
  });

  it("shows invalid-password inline with the server message", async () => {
    const user = userEvent.setup();
    accountHandlers();
    server.use(
      http.put("/api/v1/me/password", () =>
        HttpResponse.json(
          { slug: "invalid-password", message: "The password is too common." },
          { status: 400 },
        ),
      ),
    );
    renderAppAt("/account");
    await screen.findByRole("heading", { level: 1, name: "Account" });
    await user.type(screen.getByLabelText("Current password"), "old-password-1");
    await user.type(screen.getByLabelText(/New password/), "correct-horse-battery");
    await user.type(screen.getByLabelText("Repeat new"), "correct-horse-battery");
    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(await screen.findByText("The password is too common.")).toBeInTheDocument();
  });
});
