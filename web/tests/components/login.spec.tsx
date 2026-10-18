import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { configFixture, meFixture, providersFixture } from "../mocks/fixtures";
import { server } from "../mocks/server";
import { renderAppAt } from "./render";

function userRouteHandlers() {
  server.use(
    http.get("/api/v1/me", () => HttpResponse.json(meFixture())),
    http.get("/api/v1/jobs", () => HttpResponse.json({ jobs: [], stale: false })),
    http.get("/api/v1/providers", () => HttpResponse.json(providersFixture())),
  );
}

describe("login", () => {
  it("logs in and navigates to the jobs page", async () => {
    const user = userEvent.setup();
    let requestBody: unknown;
    server.use(
      http.post("/api/v1/auth/login", async ({ request }) => {
        requestBody = await request.json();
        return HttpResponse.json({ token: "t", expiresAt: "2099-01-01T00:00:00Z" });
      }),
    );
    userRouteHandlers();
    renderAppAt("/login");
    await user.type(await screen.findByLabelText("Username"), "alice");
    await user.type(screen.getByLabelText("Password"), "secret");
    await user.click(screen.getByRole("button", { name: "Log in" }));
    await waitFor(() => expect(requestBody).toEqual({ username: "alice", password: "secret" }));
    await screen.findByRole("heading", { level: 1, name: "Jobs" });
  });

  it("shows the wrong-password error and clears the password", async () => {
    const user = userEvent.setup();
    server.use(
      http.post("/api/v1/auth/login", () =>
        HttpResponse.json({ slug: "invalid-credentials", message: "" }, { status: 401 }),
      ),
    );
    renderAppAt("/login");
    const password = await screen.findByLabelText("Password");
    await user.type(screen.getByLabelText("Username"), "alice");
    await user.type(password, "wrong");
    await user.click(screen.getByRole("button", { name: "Log in" }));
    expect(await screen.findByText("Wrong username or password.")).toBeInTheDocument();
    await waitFor(() => expect(password).toHaveValue(""));
  });

  it("shows a throttled countdown and disables the button", async () => {
    const user = userEvent.setup();
    server.use(
      http.post("/api/v1/auth/login", () =>
        HttpResponse.json(
          { slug: "login-throttled", message: "", retryAfter: 42 },
          { status: 429 },
        ),
      ),
    );
    renderAppAt("/login");
    await user.type(await screen.findByLabelText("Username"), "alice");
    await user.type(screen.getByLabelText("Password"), "secret");
    await user.click(screen.getByRole("button", { name: "Log in" }));
    expect(await screen.findByText("Too many attempts.")).toBeInTheDocument();
    expect(screen.getByText(/Try again in \d+:\d+/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Log in" })).toBeDisabled();
  });

  it("shows the single sign-on failure message", async () => {
    userRouteHandlers();
    renderAppAt("/login?error=oidc");
    expect(await screen.findByText(/Single sign-on didn't work/)).toBeInTheDocument();
  });

  it("shows the single sign-on button when OIDC is enabled", async () => {
    server.use(
      http.get("/api/v1/config", () => HttpResponse.json({ ...configFixture, oidcEnabled: true })),
    );
    renderAppAt("/login");
    expect(
      await screen.findByRole("button", { name: "Log in with single sign-on" }),
    ).toBeInTheDocument();
  });
});
