import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClientProvider, QueryClient } from "@tanstack/react-query";
import Tokens from "./Tokens";

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={qc}><Tokens /></QueryClientProvider>);
}

describe("Tokens", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("shows the plaintext token once after create", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url: any, opts: any) => {
      if (String(url).endsWith("/tokens") && opts?.method === "POST")
        return new Response(JSON.stringify({ name: "laptop", token: "bur_SECRET123" }), { status: 201 }) as any;
      return new Response("[]", { status: 200 }) as any;
    });
    setup();
    // Input is associated with a visible label — query by label text
    await userEvent.type(screen.getByLabelText(/token name/i), "laptop");
    await userEvent.click(screen.getByRole("button", { name: /create/i }));
    expect(await screen.findByText("bur_SECRET123")).toBeInTheDocument();
  });

  it("renders formatted timestamps (not raw RFC3339) for token rows", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify([
        { id: "t1", name: "laptop", created_at: "2024-01-15T10:00:00Z", last_used: "2024-06-01T09:30:00Z" },
      ]), { status: 200 }) as any
    );
    setup();
    // Raw RFC3339 strings must NOT appear as-is in the DOM
    await screen.findByText("laptop");
    expect(screen.queryByText("2024-01-15T10:00:00Z")).not.toBeInTheDocument();
    expect(screen.queryByText("2024-06-01T09:30:00Z")).not.toBeInTheDocument();
  });

  it("renders 'never' when last_used is null", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify([
        { id: "t1", name: "server", created_at: "2024-01-15T10:00:00Z", last_used: null },
      ]), { status: 200 }) as any
    );
    setup();
    await screen.findByText("server");
    expect(screen.getByText("never")).toBeInTheDocument();
  });

  it("renders the token table using the design-system table.data class", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify([
        { id: "t1", name: "laptop", created_at: "2024-01-15T10:00:00Z", last_used: null },
      ]), { status: 200 }) as any
    );
    setup();
    await screen.findByText("laptop");
    expect(screen.getByRole("table").className).toContain("data");
  });

  it("Revoke button has a distinguishing aria-label", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify([
        { id: "t1", name: "mymachine", created_at: "2024-01-15T10:00:00Z", last_used: null },
      ]), { status: 200 }) as any
    );
    setup();
    await screen.findByText("mymachine");
    expect(screen.getByRole("button", { name: "Revoke token mymachine" })).toBeInTheDocument();
  });
});
