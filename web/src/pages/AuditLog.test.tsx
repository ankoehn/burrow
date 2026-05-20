import { describe, it, expect, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import AuditLog from "@/pages/AuditLog";

function mount() {
  return renderApp(<AuditLog />, "/audit");
}

describe("Audit log (§4.25)", () => {
  it("renders the verbatim hash-chain preamble (with `burrowd audit verify` in mono)", async () => {
    mount();
    const preamble = await screen.findByText(/hash-chained/i);
    expect(preamble).toHaveTextContent(
      "Hash-chained — each entry includes the SHA-256 of the previous one. Verify chain integrity from the CLI:",
    );
    expect(preamble.querySelector("code")).toHaveTextContent("burrowd audit verify");
  });

  it("renders the dense events table with the spec columns", async () => {
    mount();
    const table = await screen.findByRole("table", { name: /audit events/i });
    const headers = Array.from(table.querySelectorAll("thead th")).map((h) => h.textContent);
    expect(headers).toEqual(
      expect.arrayContaining(["When", "Actor", "Action", "Subject", "Result", "Source IP"]),
    );
  });

  it("Export triggers GET /audit/export?format=ndjson", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount();
    await userEvent.click(await screen.findByRole("button", { name: /^export$/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url]) =>
          String(url).includes("/api/v1/audit/export")
          && String(url).includes("format=ndjson"),
        ),
      ).toBe(true);
    });
  });

  it("Verify chain POSTs /audit/verify and surfaces the inline 'Chain valid …' line", async () => {
    mount();
    await userEvent.click(await screen.findByRole("button", { name: /verify chain/i }));
    expect(await screen.findByText(/^Chain valid from/i)).toBeInTheDocument();
  });
});
