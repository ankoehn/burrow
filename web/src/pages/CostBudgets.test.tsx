import { describe, it, expect, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import CostBudgets from "@/pages/CostBudgets";

function mount() {
  return renderApp(<CostBudgets />, "/cost");
}

describe("Cost & budgets (§4.24)", () => {
  it("renders the verbatim pricing disclosure", async () => {
    mount();
    expect(
      await screen.findByText(
        "Estimates from the pricing table shipped with Burrow v0.4. Operators can edit this table in Settings.",
      ),
    ).toBeInTheDocument();
  });

  it("renders four spend tiles (today/week/month/year)", async () => {
    mount();
    const strip = await screen.findByRole("list", { name: /spend by window/i });
    expect(strip).toBeInTheDocument();
    const tiles = screen.getAllByRole("listitem", { hidden: false });
    const spendTiles = tiles.filter((tile) => tile.querySelector(".label"));
    expect(spendTiles.length).toBeGreaterThanOrEqual(4);
  });

  it("Add budget validates daily_usd > 0 and posts /budgets", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount();
    await userEvent.click(await screen.findByRole("button", { name: /add budget/i }));
    const daily = await screen.findByLabelText(/daily usd/i);
    await userEvent.clear(daily);
    await userEvent.type(daily, "0");
    await userEvent.click(screen.getByRole("button", { name: /^create$/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/must be greater than zero/i);
    await userEvent.clear(daily);
    await userEvent.type(daily, "25");
    await userEvent.click(screen.getByRole("button", { name: /^create$/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url, init]) =>
          String(url).endsWith("/api/v1/budgets")
          && (init as RequestInit | undefined)?.method === "POST",
        ),
      ).toBe(true);
    });
  });

  it("Export cost report triggers GET /cost/export?format=ndjson", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount();
    await userEvent.click(await screen.findByRole("button", { name: /export cost report/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url]) =>
          String(url).includes("/api/v1/cost/export?")
          && String(url).includes("format=ndjson"),
        ),
      ).toBe(true);
    });
  });
});
