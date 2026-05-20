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

  it("opens role detail and shows the API-provided permissions for built-in", async () => {
    renderApp(<Roles />);
    const userRow = (await screen.findByText("user")).closest("tr")!;
    await userEvent.click(within(userRow).getByRole("button", { name: /view/i }));
    const dialog = await screen.findByRole("dialog", { name: /user/i });
    expect(within(dialog).getByText("tunnels:read:own")).toBeInTheDocument();
    expect(within(dialog).getByText(/Read-only — built-in role/i)).toBeInTheDocument();
  });

  it("custom roles show an Edit button that opens the CustomRoleEditor", async () => {
    renderApp(<Roles />);
    const analystRow = (await screen.findByText("analyst")).closest("tr")!;
    expect(within(analystRow).getByText("Custom")).toBeInTheDocument();
    await userEvent.click(within(analystRow).getByRole("button", { name: /edit/i }));
    expect(await screen.findByRole("dialog", { name: /edit role · analyst/i })).toBeInTheDocument();
  });

  it("New role opens the editor in create mode", async () => {
    renderApp(<Roles />);
    await userEvent.click(await screen.findByRole("button", { name: /new role/i }));
    expect(await screen.findByRole("dialog", { name: /^new role$/i })).toBeInTheDocument();
  });
});
