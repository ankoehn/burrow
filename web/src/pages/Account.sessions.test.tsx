import { describe, it, expect } from "vitest";
import { screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import Account from "@/pages/Account";

describe("Account active sessions", () => {
  it("lists sessions and marks the current one", async () => {
    renderApp(<Account />);
    const table = await screen.findByRole("table", { name: /active sessions/i });
    expect(within(table).getByText(/this device/i)).toBeInTheDocument();
    expect(within(table).getByText("198.51.100.4")).toBeInTheDocument();
  });

  it("revokes a non-current session", async () => {
    renderApp(<Account />);
    const table = await screen.findByRole("table", { name: /active sessions/i });
    const oldRow = within(table).getByText("198.51.100.4").closest("tr")!;
    await userEvent.click(within(oldRow).getByRole("button", { name: /revoke/i }));
    await waitFor(() => expect(screen.queryByText("198.51.100.4")).not.toBeInTheDocument());
  });

  it("signs out everywhere", async () => {
    renderApp(<Account />);
    await screen.findByRole("table", { name: /active sessions/i });
    await userEvent.click(screen.getByRole("button", { name: /sign out everywhere/i }));
    await waitFor(() => expect(screen.queryByText("198.51.100.4")).not.toBeInTheDocument());
  });
});
