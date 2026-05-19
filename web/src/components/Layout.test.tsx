import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, act, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { ThemeProvider } from "./theme-provider";
import { Layout } from "./Layout";

function renderLayout(meRole?: "admin" | "user" | null) {
  // Mock fetch: /api/v1/me returns a user with the given role, or 401 if null.
  vi.spyOn(globalThis, "fetch").mockImplementation(async (url: unknown) => {
    if (String(url).includes("/api/v1/me")) {
      if (meRole == null) {
        return new Response(JSON.stringify({ error: "unauthorized" }), { status: 401 }) as Response;
      }
      return new Response(
        JSON.stringify({ id: "u1", email: "alice@example.com", role: meRole }),
        { status: 200 }
      ) as Response;
    }
    return new Response("{}", { status: 200 }) as Response;
  });

  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <ThemeProvider>
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/tunnels"]}>
          <Routes>
            <Route element={<Layout />}>
              <Route path="/tunnels" element={<div>TUNNELS</div>} />
            </Route>
            <Route path="/login" element={<div>LOGIN</div>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    </ThemeProvider>
  );
}

describe("Layout theme toggle", () => {
  beforeEach(() => {
    document.documentElement.classList.remove("dark");
    localStorage.clear();
    vi.mocked(window.matchMedia).mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }));
  });

  it("renders the toggle button with correct aria-label in light mode", () => {
    renderLayout();
    const btn = screen.getByRole("button", { name: "Switch to dark theme" });
    expect(btn).toBeInTheDocument();
  });

  it("clicking the toggle switches to dark and updates aria-label", async () => {
    renderLayout();
    const btn = screen.getByRole("button", { name: "Switch to dark theme" });
    await act(async () => { btn.click(); });
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(screen.getByRole("button", { name: "Switch to light theme" })).toBeInTheDocument();
  });

  it("clicking toggle twice returns to light and correct aria-label", async () => {
    renderLayout();
    const btn = screen.getByRole("button", { name: "Switch to dark theme" });
    await act(async () => { btn.click(); });
    const darkBtn = screen.getByRole("button", { name: "Switch to light theme" });
    await act(async () => { darkBtn.click(); });
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(screen.getByRole("button", { name: "Switch to dark theme" })).toBeInTheDocument();
  });

  it("Log out button is still present alongside toggle", () => {
    renderLayout();
    expect(screen.getByRole("button", { name: /log out/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /switch to/i })).toBeInTheDocument();
  });
});

describe("Layout nav role-gating", () => {
  beforeEach(() => {
    document.documentElement.classList.remove("dark");
    localStorage.clear();
    vi.mocked(window.matchMedia).mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }));
  });

  it("shows Users nav link when role is admin", async () => {
    renderLayout("admin");
    // findByRole waits for the link to appear (after useAuth resolves)
    expect(await screen.findByRole("link", { name: /^users$/i })).toBeInTheDocument();
  });

  it("hides Users nav link when role is user", async () => {
    renderLayout("user");
    // Wait for auth to settle: the Account link must appear (role-neutral)
    await screen.findByRole("link", { name: /^account$/i });
    // Users link must NOT be present for non-admin
    await waitFor(() => {
      expect(screen.queryByRole("link", { name: /^users$/i })).not.toBeInTheDocument();
    });
  });

  it("Tunnels, Tokens, and Account links are present for both roles", async () => {
    renderLayout("user");
    await screen.findByRole("link", { name: /^account$/i });
    expect(screen.getByRole("link", { name: /^tunnels$/i })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /^tokens$/i })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /^account$/i })).toBeInTheDocument();
  });

  it("Tunnels, Tokens, Account, and Users links are present for admin", async () => {
    renderLayout("admin");
    // Wait for the Users link which only appears after useAuth resolves with admin role
    expect(await screen.findByRole("link", { name: /^users$/i })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /^tunnels$/i })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /^tokens$/i })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /^account$/i })).toBeInTheDocument();
  });

  it("nav links use the design-system .nav-item class", () => {
    renderLayout();
    expect(screen.getByRole("link", { name: /^tunnels$/i }).className).toContain("nav-item");
  });
});
