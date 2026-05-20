import { describe, it, expect, vi } from "vitest";
import { screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import AutomationTokens from "@/pages/AutomationTokens";

function mount() {
  return renderApp(<AutomationTokens />, "/account/automation");
}

describe("Automation tokens", () => {
  it("renders a row per token with mono bua_ prefix", async () => {
    mount();
    const table = await screen.findByRole("table", { name: /automation tokens/i });
    expect(within(table).getByText(/bua_/)).toBeInTheDocument();
  });

  it("Mint reveals the plaintext once with the verbatim save-now warning", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount();
    await userEvent.click(await screen.findByRole("button", { name: /mint token/i }));
    const dlg = await screen.findByRole("dialog");
    await userEvent.type(within(dlg).getByLabelText(/^name$/i), "ci-bot");
    await userEvent.click(within(dlg).getByRole("button", { name: /^create$/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url, init]) =>
          String(url).endsWith("/api/v1/automation/tokens")
          && (init as RequestInit | undefined)?.method === "POST",
        ),
      ).toBe(true);
    });
    expect(
      await screen.findByText("Save this token now — you won't see it again."),
    ).toBeInTheDocument();
  });
});
