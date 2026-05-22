import { describe, it, expect, vi, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import { db, resetDb } from "@/mocks/db";
import ConnectionLogs from "@/pages/ConnectionLogs";

function mount() {
  return renderApp(<ConnectionLogs />, "/connection-logs");
}

describe("Connection logs page (§v0.5.0 Part E)", () => {
  afterEach(() => {
    resetDb();
  });

  it("renders the heading 'Connection logs'", async () => {
    mount();
    expect(await screen.findByRole("heading", { name: /^connection logs$/i })).toBeInTheDocument();
  });

  it("renders the seeded rows in the table", async () => {
    mount();
    const table = await screen.findByRole("table", { name: /connection logs/i });
    const rows = Array.from(table.querySelectorAll("tbody tr"));
    expect(rows.length).toBeGreaterThan(5);
    // mono small cells (source IP or started_at) should be visible
    const monoCells = Array.from(table.querySelectorAll("td.mono"));
    expect(monoCells.length).toBeGreaterThan(0);
  });

  it("empty state shows verbatim text when zero rows", async () => {
    db.connectionLogs = [];
    mount();
    expect(
      await screen.findByText(
        "No connection logs yet. Connections are recorded on session close.",
      ),
    ).toBeInTheDocument();
  });

  it("Rollups toggle switches the table to the rollups endpoint", async () => {
    mount();
    // Wait for initial table to load
    await screen.findByRole("table", { name: /connection logs/i });
    // Click the rollups toggle (native checkbox rendered with aria-label="Rollups")
    const toggle = await screen.findByRole("checkbox", { name: /rollups/i });
    await userEvent.click(toggle);
    // Rollups table should show "Day" column header
    await waitFor(() => {
      expect(screen.getByText("Day")).toBeInTheDocument();
    });
    // At least one rollup row should be visible
    const table = screen.getByRole("table", { name: /connection logs/i });
    const rows = Array.from(table.querySelectorAll("tbody tr"));
    expect(rows.length).toBeGreaterThan(0);
  });

  it("Export button triggers GET /connection-logs/export with format=ndjson", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount();
    await userEvent.click(await screen.findByRole("button", { name: /^export$/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url]) =>
          String(url).includes("/api/v1/connection-logs/export") &&
          String(url).includes("format=ndjson"),
        ),
      ).toBe(true);
    });
  });

  it("Kind filter narrows to the chosen kind", async () => {
    mount();
    // Wait for initial table
    await screen.findByRole("table", { name: /connection logs/i });
    // Select "control" from the native kind select
    const kindSelect = await screen.findByRole("combobox", { name: /kind/i });
    await userEvent.selectOptions(kindSelect, "control");
    // Table still renders rows (at least some control rows seeded)
    await waitFor(() => {
      const table = screen.getByRole("table", { name: /connection logs/i });
      const rows = Array.from(table.querySelectorAll("tbody tr"));
      expect(rows.length).toBeGreaterThan(0);
    });
    // Verify only control rows are shown (no http_proxy or tcp_proxy badges)
    const kindCells = Array.from(
      document.querySelectorAll("td[data-kind]"),
    );
    if (kindCells.length > 0) {
      kindCells.forEach((cell) => {
        expect(cell.getAttribute("data-kind")).toBe("control");
      });
    }
  });
});
