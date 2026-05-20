import { describe, it, expect, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import PromptCache from "@/pages/PromptCache";

function mount() {
  return renderApp(<PromptCache />, "/cache");
}

describe("Prompt cache settings (§4.21)", () => {
  it("renders the shared preamble verbatim", async () => {
    mount();
    expect(
      await screen.findByText(
        (_, el) =>
          el?.tagName === "P" &&
          el.textContent === "The cache lives on this relay. No data is sent anywhere.",
      ),
    ).toBeInTheDocument();
  });

  it("Semantic tab shows the calm banner verbatim and a disabled Switch with the v0.5 tooltip", async () => {
    mount();
    await userEvent.click(await screen.findByRole("tab", { name: /semantic/i }));
    expect(
      await screen.findByText(
        "Semantic caching is off by default. Enable only after reading the docs — vector similarity can return stale or near-miss answers.",
      ),
    ).toBeInTheDocument();
    const sw = screen.getByRole("switch", { name: /enable semantic cache/i });
    expect(sw).toHaveAttribute("aria-checked", "false");
    expect(sw).toHaveAttribute("aria-disabled", "true");
    expect(sw).toHaveAttribute("title", "Available in v0.5");
  });

  it("Exact tab Save issues PUT /cache/settings + toasts 'Cache settings saved.'", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount();
    // ensure Exact is the active tab
    await userEvent.click(await screen.findByRole("tab", { name: /exact/i }));
    const ttl = await screen.findByLabelText(/ttl/i);
    await userEvent.clear(ttl);
    await userEvent.type(ttl, "900");
    await userEvent.click(screen.getByRole("button", { name: /save cache settings/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url, init]) =>
          String(url).endsWith("/api/v1/cache/settings")
          && (init as RequestInit | undefined)?.method === "PUT",
        ),
      ).toBe(true);
    });
    expect((await screen.findAllByText(/cache settings saved/i)).length).toBeGreaterThan(0);
  });

  it("Clear cache prompts a destructive confirm and then DELETEs /cache/entries", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount();
    await userEvent.click(await screen.findByRole("button", { name: /clear cache/i }));
    expect(
      await screen.findByText("Clear all cached responses? This cannot be undone."),
    ).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /^clear$/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url, init]) =>
          String(url).endsWith("/api/v1/cache/entries")
          && (init as RequestInit | undefined)?.method === "DELETE",
        ),
      ).toBe(true);
    });
    expect((await screen.findAllByText(/cache cleared/i)).length).toBeGreaterThan(0);
  });
});
