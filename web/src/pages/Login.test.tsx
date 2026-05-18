import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClientProvider, QueryClient } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import Login from "./Login";

function setup() {
  const qc = new QueryClient();
  render(<QueryClientProvider client={qc}><MemoryRouter><Login /></MemoryRouter></QueryClientProvider>);
}

describe("Login", () => {
  it("shows an error on bad credentials", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ error: "invalid credentials" }), { status: 401 }) as any);
    setup();
    await userEvent.type(screen.getByPlaceholderText("Email"), "a@x");
    await userEvent.type(screen.getByPlaceholderText("Password"), "bad");
    await userEvent.click(screen.getByRole("button", { name: /sign in/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/invalid/i);
  });
  it("calls the login endpoint on submit", async () => {
    const f = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("{}", { status: 200 }) as any);
    setup();
    await userEvent.type(screen.getByPlaceholderText("Email"), "a@x");
    await userEvent.type(screen.getByPlaceholderText("Password"), "pw");
    await userEvent.click(screen.getByRole("button", { name: /sign in/i }));
    expect(f).toHaveBeenCalledWith("/api/v1/auth/login", expect.objectContaining({ method: "POST" }));
  });
});
