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
    await userEvent.type(screen.getByPlaceholderText(/token name/i), "laptop");
    await userEvent.click(screen.getByRole("button", { name: /create/i }));
    expect(await screen.findByText("bur_SECRET123")).toBeInTheDocument();
  });
});
