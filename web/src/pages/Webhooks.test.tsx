import { describe, it, expect, vi } from "vitest";
import { screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import Webhooks from "@/pages/Webhooks";

function mount() {
  return renderApp(<Webhooks />, "/webhooks");
}

describe("Webhooks (§4.26)", () => {
  it("renders the verbatim HMAC preamble", async () => {
    mount();
    expect(
      await screen.findByText(
        /Burrow signs every webhook with an HMAC-SHA256 signature in the/,
      ),
    ).toBeInTheDocument();
  });

  it("renders a row per webhook with a mono URL + Copy affordance", async () => {
    mount();
    const table = await screen.findByRole("table", { name: /webhooks/i });
    expect(within(table).getByText(/example\.com\/hook/)).toBeInTheDocument();
    expect(within(table).getByRole("button", { name: /copy webhook url/i })).toBeInTheDocument();
  });

  it("Add webhook rejects non-HTTPS URLs and reveals the signing secret on success", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount();
    await userEvent.click(await screen.findByRole("button", { name: /add webhook/i }));
    const name = await screen.findByLabelText(/^name$/i);
    const url = screen.getByLabelText(/^url$/i);
    await userEvent.type(name, "ops");
    await userEvent.type(url, "http://example.com/x");
    await userEvent.click(screen.getByRole("button", { name: /^create$/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/https/i);
    await userEvent.clear(url);
    await userEvent.type(url, "https://example.com/ops");
    await userEvent.click(screen.getByRole("button", { name: /^create$/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([u, init]) =>
          String(u).endsWith("/api/v1/webhooks")
          && (init as RequestInit | undefined)?.method === "POST",
        ),
      ).toBe(true);
    });
    expect(
      await screen.findByText("Save this signing secret now — you won't see it again."),
    ).toBeInTheDocument();
  });
});
