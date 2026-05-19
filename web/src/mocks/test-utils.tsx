import type { ReactElement } from "react";
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { ThemeProvider } from "@/components/theme-provider";
import { db } from "@/mocks/db";

// Mirror the production query defaults but disable retry for fast failing tests.
function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

export function setCsrfCookie(): void {
  document.cookie = `burrow_csrf=${db.csrf}; path=/`;
}

export function renderApp(ui: ReactElement, route = "/") {
  setCsrfCookie();
  const qc = makeClient();
  return {
    qc,
    ...render(
      <ThemeProvider>
        <QueryClientProvider client={qc}>
          <MemoryRouter initialEntries={[route]}>{ui}</MemoryRouter>
        </QueryClientProvider>
      </ThemeProvider>,
    ),
  };
}
