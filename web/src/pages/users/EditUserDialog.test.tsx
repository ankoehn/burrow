import { describe, it, expect } from "vitest";
import { screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import Users from "@/pages/Users";

describe("Edit user dialog", () => {
  it("suspends an active user via the status switch", async () => {
    renderApp(<Users />);
    const bobRow = (await screen.findByText("bob@acme.io")).closest("tr")!;
    await userEvent.click(within(bobRow).getByRole("button", { name: /^edit$/i }));
    const dialog = await screen.findByRole("dialog", { name: /edit user/i });
    await userEvent.click(within(dialog).getByRole("switch", { name: /user status/i }));
    await userEvent.click(within(dialog).getByRole("button", { name: /save changes/i }));
    await waitFor(() => {
      const row = screen.getByText("bob@acme.io").closest("tr")!;
      expect(within(row).getByText(/suspended/i)).toBeInTheDocument();
    });
  });

  it("disables status switch and delete for the current user (self)", async () => {
    renderApp(<Users />);
    const meRow = (await screen.findByText("alice@acme.io")).closest("tr")!;
    await userEvent.click(within(meRow).getByRole("button", { name: /^edit$/i }));
    const dialog = await screen.findByRole("dialog", { name: /edit user/i });
    expect(within(dialog).getByRole("switch", { name: /user status/i })).toHaveAttribute("aria-disabled", "true");
    expect(within(dialog).getByRole("button", { name: /delete user/i })).toBeDisabled();
  });
});
