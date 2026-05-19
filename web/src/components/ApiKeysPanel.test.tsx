import { describe, it, expect } from "vitest";
import { screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import { ApiKeysPanel } from "@/components/ApiKeysPanel";

const SVC = "svc_ai001"; // seeded http service in api_key mode with 2 keys

describe("ApiKeysPanel", () => {
  it("lists existing keys with created/last-used and a Revoke action", async () => {
    renderApp(<ApiKeysPanel serviceId={SVC} />);
    const table = await screen.findByRole("table", { name: /api keys/i });
    const ci = (await within(table).findByText("ci")).closest("tr")!;
    // 'ci' has last_used null → "Never"
    expect(within(ci).getByText("Never")).toBeInTheDocument();
    expect(within(ci).getByRole("button", { name: /revoke key ci/i })).toBeInTheDocument();
    expect(within(table).getByText("prod")).toBeInTheDocument();
  });

  it("shows the services:configure permission hint", async () => {
    renderApp(<ApiKeysPanel serviceId={SVC} />);
    expect(
      await screen.findByText("Managing keys needs the services:configure permission."),
    ).toBeInTheDocument();
  });

  it("creates a key and reveals the plaintext exactly once", async () => {
    renderApp(<ApiKeysPanel serviceId={SVC} />);
    await screen.findByRole("table", { name: /api keys/i });
    await userEvent.click(screen.getByRole("button", { name: /create key/i }));
    await userEvent.type(screen.getByLabelText(/key name/i), "deploy");
    await userEvent.click(screen.getByRole("button", { name: /^create$/i }));

    const reveal = await screen.findByText(/store it now — you won't see it again\./i);
    expect(reveal).toBeInTheDocument();
    const keyText = await screen.findByText(/^buk_mock_/);
    expect(keyText).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /copy api key/i })).toBeInTheDocument();

    // Dismiss the reveal — the plaintext must no longer be retrievable.
    await userEvent.click(screen.getByRole("button", { name: /done/i }));
    await waitFor(() => expect(screen.queryByText(/^buk_mock_/)).toBeNull());
  });

  it("revokes a key after confirmation", async () => {
    renderApp(<ApiKeysPanel serviceId={SVC} />);
    const table = await screen.findByRole("table", { name: /api keys/i });
    await userEvent.click(await within(table).findByRole("button", { name: /revoke key prod/i }));
    await userEvent.click(await screen.findByRole("button", { name: /^revoke$/i }));
    await waitFor(() =>
      expect(within(screen.getByRole("table", { name: /api keys/i })).queryByText("prod")).toBeNull(),
    );
  });
});
