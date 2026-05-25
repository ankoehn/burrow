import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import Account from "@/pages/Account";
import { parseUserAgent } from "@/lib/userAgent";

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

  it("sessions table renders browser · os, not raw UA (C5)", async () => {
    renderApp(<Account />);
    await waitFor(() => {
      expect(screen.getByText(/Chrome 148 · Windows/)).toBeInTheDocument();
      expect(screen.queryByText(/AppleWebKit/)).toBeNull();
    });
  });

  it("parseUserAgent unit: Chrome on Windows", () => {
    const out = parseUserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36");
    expect(out).toEqual({ browser: "Chrome 148", os: "Windows" });
  });

  it("renders only one 'Add a passkey' CTA when the list is empty (C3 follow-up)", async () => {
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
    renderApp(<Account />);
    await waitFor(() => screen.getByText(/no passkeys yet/i));
    const ctas = screen.getAllByRole("button", { name: /add a passkey/i });
    expect(ctas).toHaveLength(1);
  });
});
