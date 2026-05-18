import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { ThemeProvider } from "./theme-provider";
import { Layout } from "./Layout";

function renderLayout() {
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
