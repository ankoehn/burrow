import { describe, it, expect } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { render } from "@testing-library/react";
import { ThemeProvider } from "@/components/theme-provider";
import App from "@/App";
import { setCsrfCookie } from "@/mocks/test-utils";

function renderAt(route: string) {
  setCsrfCookie();
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <ThemeProvider><QueryClientProvider client={qc}><MemoryRouter initialEntries={[route]}><App /></MemoryRouter></QueryClientProvider></ThemeProvider>,
  );
}

describe("App routes", () => {
  it("renders Roles at /roles", async () => {
    renderAt("/roles");
    expect(await screen.findByRole("heading", { name: /^Roles$/i })).toBeInTheDocument();
  });
  it("renders Settings at /settings", async () => {
    renderAt("/settings");
    expect(await screen.findByRole("heading", { name: /^Settings$/i })).toBeInTheDocument();
  });
  it("renders Clients at /clients", async () => {
    renderAt("/clients");
    expect(await screen.findByRole("heading", { name: /^Clients$/i })).toBeInTheDocument();
  });
  it("renders Connect at /clients/connect", async () => {
    renderAt("/clients/connect");
    expect(await screen.findByRole("heading", { name: /connect a client/i })).toBeInTheDocument();
  });
  it("shows admin nav links for an admin", async () => {
    renderAt("/account");
    expect(await screen.findByRole("link", { name: /^Roles$/i })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /^Settings$/i })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /^Clients$/i })).toBeInTheDocument();
  });
});
