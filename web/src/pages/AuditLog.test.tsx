import { describe, it, expect, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import AuditLog from "@/pages/AuditLog";

function mount() {
  return renderApp(<AuditLog />, "/audit");
}

describe("Audit log (§4.25)", () => {
  // P2-5: the preamble no longer points at the CLI; it directs operators
  // at the in-UI Verify chain button (no `burrowd audit verify` reference).
  it("renders the user-facing hash-chain preamble pointing at the Verify chain button (P2-5)", async () => {
    mount();
    const preamble = await screen.findByText(/hash-chained/i);
    expect(preamble).toHaveTextContent(
      "Hash-chained — each entry includes the SHA-256 of the previous one. Click Verify chain to confirm integrity.",
    );
    expect(preamble.querySelector("code")).toBeNull();
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

  it("renders formatted timestamps (not raw RFC3339) for audit rows (B5)", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (url: unknown) => {
      if (String(url).includes("/audit/events")) {
        return new Response(
          JSON.stringify([
            {
              id: "1",
              ts: "2026-05-25T07:42:51.83442115Z",
              actor_id: "",
              actor_email: "",
              action: "session.create",
              subject_id: "",
              subject_label: "",
              result: "ok",
              source_ip: "127.0.0.1",
              user_agent: "",
              request_id: "",
              payload: {},
              prev_hash: "",
              hash: "",
            },
          ]),
          { status: 200 },
        ) as Response;
      }
      return new Response("{}", { status: 200 }) as Response;
    });
    mount();
    await waitFor(() => {
      expect(screen.queryByText(/2026-05-25T07:42:51\.83442115Z/)).toBeNull();
      expect(screen.getByText(/25 May 2026/)).toBeInTheDocument();
    });
  });
});
