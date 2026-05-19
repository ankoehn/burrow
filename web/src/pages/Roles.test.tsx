import { describe, it, expect } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import Roles from "@/pages/Roles";

describe("Roles", () => {
  it("lists built-in roles", async () => {
    renderApp(<Roles />);
    expect(await screen.findByText("admin")).toBeInTheDocument();
    expect(screen.getByText("user")).toBeInTheDocument();
    expect(screen.getByText(/Use own tunnels and tokens/i)).toBeInTheDocument();
  });

  it("opens role detail and shows the API-provided permissions", async () => {
    renderApp(<Roles />);
    const userRow = (await screen.findByText("user")).closest("tr")!;
    await userEvent.click(within(userRow).getByRole("button", { name: /view/i }));
    const dialog = await screen.findByRole("dialog", { name: /user/i });
    expect(within(dialog).getByText("tunnels:read:own")).toBeInTheDocument();
    expect(within(dialog).getByText(/Read-only — built-in role/i)).toBeInTheDocument();
  });
});
