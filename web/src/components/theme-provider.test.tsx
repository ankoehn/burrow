import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, act, screen } from "@testing-library/react";
import { ThemeProvider, useTheme } from "./theme-provider";

// Helper component to expose theme context values in tests.
function Consumer() {
  const { theme, toggleTheme, setTheme } = useTheme();
  return (
    <div>
      <span data-testid="theme">{theme}</span>
      <button onClick={toggleTheme}>toggle</button>
      <button onClick={() => setTheme("dark")}>set-dark</button>
      <button onClick={() => setTheme("light")}>set-light</button>
    </div>
  );
}

function renderWithProvider() {
  return render(
    <ThemeProvider>
      <Consumer />
    </ThemeProvider>
  );
}

describe("ThemeProvider", () => {
  beforeEach(() => {
    // Clean up class and storage between tests.
    document.documentElement.classList.remove("dark");
    localStorage.clear();
    // Reset matchMedia mock to return light (matches: false) by default.
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

  it("defaults to light when no stored value and matchMedia returns false", () => {
    renderWithProvider();
    expect(screen.getByTestId("theme").textContent).toBe("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });

  it("falls back to dark when matchMedia prefers dark and no localStorage value", () => {
    vi.mocked(window.matchMedia).mockImplementation((query: string) => ({
      matches: query === "(prefers-color-scheme: dark)",
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }));
    renderWithProvider();
    expect(screen.getByTestId("theme").textContent).toBe("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("reads initial theme from localStorage", () => {
    localStorage.setItem("burrow-theme", "dark");
    renderWithProvider();
    expect(screen.getByTestId("theme").textContent).toBe("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("light stored value overrides dark matchMedia", () => {
    localStorage.setItem("burrow-theme", "light");
    vi.mocked(window.matchMedia).mockImplementation((query: string) => ({
      matches: query === "(prefers-color-scheme: dark)",
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }));
    renderWithProvider();
    expect(screen.getByTestId("theme").textContent).toBe("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });

  it("toggleTheme switches from light to dark, adds class, persists", async () => {
    renderWithProvider();
    expect(screen.getByTestId("theme").textContent).toBe("light");
    await act(async () => {
      screen.getByRole("button", { name: "toggle" }).click();
    });
    expect(screen.getByTestId("theme").textContent).toBe("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(localStorage.getItem("burrow-theme")).toBe("dark");
  });

  it("toggleTheme switches from dark to light, removes class, persists", async () => {
    localStorage.setItem("burrow-theme", "dark");
    renderWithProvider();
    expect(screen.getByTestId("theme").textContent).toBe("dark");
    await act(async () => {
      screen.getByRole("button", { name: "toggle" }).click();
    });
    expect(screen.getByTestId("theme").textContent).toBe("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(localStorage.getItem("burrow-theme")).toBe("light");
  });

  it("setTheme('dark') adds class and writes localStorage", async () => {
    renderWithProvider();
    await act(async () => {
      screen.getByRole("button", { name: "set-dark" }).click();
    });
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(localStorage.getItem("burrow-theme")).toBe("dark");
  });

  it("setTheme('light') removes class and writes localStorage", async () => {
    localStorage.setItem("burrow-theme", "dark");
    renderWithProvider();
    await act(async () => {
      screen.getByRole("button", { name: "set-light" }).click();
    });
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(localStorage.getItem("burrow-theme")).toBe("light");
  });
});
