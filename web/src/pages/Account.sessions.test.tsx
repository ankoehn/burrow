import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import Account from "@/pages/Account";

describe("Account active sessions", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("renders a real EmptyState (not a flat tr) when there are no passkeys (C3)", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url: unknown) => {
      const u = String(url);
      if (u.includes("/auth/webauthn/credentials")) {
        return new Response(JSON.stringify([]), { status: 200 }) as Response;
      }
      if (u.includes("/api/v1/me")) {
        return new Response(JSON.stringify({ id: "u1", email: "alice@example.com", role: "user" }), { status: 200 }) as Response;
      }
      if (u.includes("/sessions")) {
        return new Response(JSON.stringify([]), { status: 200 }) as Response;
      }
      return new Response(JSON.stringify([]), { status: 200 }) as Response;
    });
    const { container } = renderApp(<Account />);
    await waitFor(() => {
      expect(container.querySelector(".state-card")).not.toBeNull();
      expect(container.querySelector(".state-card .icon-bubble")).not.toBeNull();
    });
  });

  it("lists sessions and marks the current one", async () => {
    renderApp(<Account />);
    const table = await screen.findByRole("table", { name: /active sessions/i });
    expect(within(table).getByText(/this device/i)).toBeInTheDocument();
    expect(within(table).getByText("198.51.100.4")).toBeInTheDocument();
  });

  it("revokes a non-current session", async () => {
    renderApp(<Account />);
    const table = await screen.findByRole("table", { name: /active sessions/i });
    const oldRow = within(table).getByText("198.51.100.4").closest("tr")!;
    await userEvent.click(within(oldRow).getByRole("button", { name: /revoke/i }));
    await waitFor(() => expect(screen.queryByText("198.51.100.4")).not.toBeInTheDocument());
  });

  it("signs out everywhere", async () => {
    renderApp(<Account />);
    await screen.findByRole("table", { name: /active sessions/i });
    await userEvent.click(screen.getByRole("button", { name: /sign out everywhere/i }));
    await waitFor(() => expect(screen.queryByText("198.51.100.4")).not.toBeInTheDocument());
  });
});
