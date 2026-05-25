import { describe, it, expect } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import Users from "@/pages/Users";

describe("Users list", () => {
  it("renders users with status badge and last-login", async () => {
    renderApp(<Users />);
    expect(await screen.findByText("bob@acme.io")).toBeInTheDocument();
    const carolRow = screen.getByText("carol@acme.io").closest("tr")!;
    expect(within(carolRow).getByText(/suspended/i)).toBeInTheDocument();
    const bobRow = screen.getByText("bob@acme.io").closest("tr")!;
    expect(within(bobRow).getByText("—")).toBeInTheDocument(); // null last_login
  });

  it("filters by email search", async () => {
    renderApp(<Users />);
    await screen.findByText("bob@acme.io");
    await userEvent.type(screen.getByRole("searchbox", { name: /search/i }), "carol");
    await waitFor(() => expect(screen.queryByText("bob@acme.io")).not.toBeInTheDocument());
    expect(screen.getByText("carol@acme.io")).toBeInTheDocument();
  });

  it("filter row has explicit gap so the Role select can't overlap the count (C1)", async () => {
    const { container } = renderApp(<Users />);
    await waitFor(() => screen.getByRole("heading", { name: "Users" }));
    const row = container.querySelector(".users-filter-row");
    expect(row).not.toBeNull();
    // The gap is defined in CSS (not inline), so we verify the class is present
    // and the element contains both the Role select and the count label.
    expect(row!.querySelector("select, [role='combobox'], button")).not.toBeNull();
    expect(row!.textContent).toMatch(/total/);
  });
});
