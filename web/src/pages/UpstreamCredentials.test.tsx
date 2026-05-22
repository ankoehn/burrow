import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import { db, resetDb } from "@/mocks/db";
import { UpstreamCredentialsPanel } from "@/pages/UpstreamCredentials";

function mount(serviceId = "svc_ai001") {
  return renderApp(<UpstreamCredentialsPanel serviceId={serviceId} />, "/");
}

describe("UpstreamCredentials panel", () => {
  beforeEach(() => resetDb());
  afterEach(() => resetDb());

  it("renders the no-binding empty state when slot_present is false and no binding", async () => {
    // svc_web01 has no binding seeded
    mount("svc_web01");
    expect(
      await screen.findByText(
        "No upstream key bound. Visitor requests proxy through unchanged.",
      ),
    ).toBeInTheDocument();
  });

  it("shows env-var-missing notice when slot_present is false and a slot is bound", async () => {
    // Simulate OPENAI slot absent and svc_ai001 bound to OPENAI
    db.absentSlots.add("OPENAI");
    mount("svc_ai001");

    // Wait for the notice to appear
    const notice = await screen.findByRole("alert");
    const strong = notice.querySelector("strong");
    const code = notice.querySelector("code");
    expect(strong?.textContent).toBe("OPENAI");
    expect(code?.textContent).toBe("BURROW_UPSTREAM_KEY_OPENAI");
    // full collapsed text
    expect(notice.textContent).toBe(
      "Slot OPENAI is bound but the environment variable is not set on this server. Requests will fail until the operator sets BURROW_UPSTREAM_KEY_OPENAI.",
    );
  });

  it("shows invalid header_format alert and disables Save when {key} is missing", async () => {
    mount("svc_ai001");
    // Wait for the form to load
    const formatInput = await screen.findByLabelText(/header format/i);
    await userEvent.clear(formatInput);
    await userEvent.type(formatInput, "Bearer xyz");

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe(
      "Header format must include {key} where the upstream key should appear.",
    );
    const saveBtn = screen.getByRole("button", { name: /save/i });
    expect(saveBtn).toBeDisabled();
  });

  it("saves binding via PUT /services/:id/upstream-credential and toasts 'Upstream key binding saved.'", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount("svc_ai001");
    // Wait for form to hydrate
    await screen.findByLabelText(/header name/i);
    // Change header_name
    const headerNameInput = screen.getByLabelText(/header name/i);
    await userEvent.clear(headerNameInput);
    await userEvent.type(headerNameInput, "X-Api-Key");
    // Click Save
    const saveBtn = screen.getByRole("button", { name: /save/i });
    await userEvent.click(saveBtn);
    await waitFor(() => {
      const putCalls = fetchSpy.mock.calls.filter(
        ([url, init]) =>
          String(url).endsWith("/api/v1/services/svc_ai001/upstream-credential") &&
          (init as RequestInit | undefined)?.method === "PUT",
      );
      expect(putCalls.length).toBeGreaterThanOrEqual(1);
      const b = JSON.parse(String((putCalls.at(-1)![1] as RequestInit).body));
      expect(b.slot).toBeTruthy();
      expect(b.header_name).toBe("X-Api-Key");
    });
    expect(
      (await screen.findAllByText("Upstream key binding saved.")).length,
    ).toBeGreaterThan(0);
  });

  it("Unbind confirm dialog calls DELETE /services/:id/upstream-credential", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount("svc_ai001");
    // Wait for the Unbind button to appear (only when binding exists)
    const unbindBtn = await screen.findByRole("button", { name: /unbind/i });
    await userEvent.click(unbindBtn);
    // Confirmation dialog should appear
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toBeInTheDocument();
    // Click Confirm in the dialog
    const confirmBtn = screen.getByRole("button", { name: /confirm/i });
    await userEvent.click(confirmBtn);
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(
          ([url, init]) =>
            String(url).endsWith("/api/v1/services/svc_ai001/upstream-credential") &&
            (init as RequestInit | undefined)?.method === "DELETE",
        ),
      ).toBe(true);
    });
  });

  it("renders the honesty disclosure line verbatim", async () => {
    mount("svc_ai001");
    expect(
      await screen.findByText(
        "Slot values live in environment variables on this server, never in the database.",
      ),
    ).toBeInTheDocument();
  });
});
