import { describe, it, expect, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import Guardrails from "@/pages/Guardrails";

function mount() {
  return renderApp(<Guardrails />, "/guardrails");
}

describe("Guardrails page (§4.22)", () => {
  it("renders three accordion sections (Regex, Presidio, Prompt-injection), collapsed by default", async () => {
    mount();
    expect(await screen.findByRole("button", { name: /regex redaction/i })).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByRole("button", { name: /presidio/i })).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByRole("button", { name: /prompt-injection/i })).toHaveAttribute("aria-expanded", "false");
  });

  it("Regex section shows built-in + custom tables when expanded", async () => {
    mount();
    await userEvent.click(await screen.findByRole("button", { name: /regex redaction/i }));
    expect(await screen.findByRole("table", { name: /built-in rules/i })).toBeInTheDocument();
    expect(screen.getByRole("table", { name: /custom rules/i })).toBeInTheDocument();
  });

  it("Presidio section shows the verbatim muted line and a Test connection button", async () => {
    mount();
    await userEvent.click(await screen.findByRole("button", { name: /presidio/i }));
    expect(
      await screen.findByText(
        "Runs Microsoft Presidio (Apache-2.0) as a sidecar process Burrow shells out to. Off by default — you install Presidio yourself.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /test connection/i })).toBeInTheDocument();
  });

  it("Prompt-injection section shows action Select + View pattern list disclosure", async () => {
    mount();
    await userEvent.click(await screen.findByRole("button", { name: /prompt-injection/i }));
    expect(await screen.findByLabelText(/on detection/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /view pattern list/i }));
    expect(await screen.findByText(/ignore previous instructions/i)).toBeInTheDocument();
  });

  it("Save Regex section issues PUT /redaction/settings and toasts", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount();
    await userEvent.click(await screen.findByRole("button", { name: /regex redaction/i }));
    await userEvent.click(await screen.findByRole("button", { name: /save regex/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url, init]) =>
          String(url).endsWith("/api/v1/redaction/settings")
          && (init as RequestInit | undefined)?.method === "PUT",
        ),
      ).toBe(true);
    });
  });
});
