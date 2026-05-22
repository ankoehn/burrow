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

describe("v0.5.0 routes", () => {
  it.each([
    ["/connection-logs",            /^Connection logs$/i],
    ["/settings/retention",         /^Retention & compliance$/i],
    ["/settings/database",          /^Database backend$/i],
    ["/services/svc_ai001/domains", /^Custom domains$/i],
  ])("%s resolves to its page heading", async (path, heading) => {
    renderAt(path);
    expect(await screen.findByRole("heading", { name: heading })).toBeInTheDocument();
  });
});
