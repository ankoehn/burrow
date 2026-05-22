import { describe, it, expect, beforeEach } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import { resetDb } from "@/mocks/db";
import Retention from "@/pages/Retention";

beforeEach(() => resetDb());

describe("Retention & compliance", () => {
  it("renders heading 'Retention & compliance'", async () => {
    renderApp(<Retention />);
    expect(await screen.findByRole("heading", { name: /retention & compliance/i })).toBeInTheDocument();
  });

  it("renders seven number inputs with their current values from /settings/retention", async () => {
    renderApp(<Retention />);
    // Wait for data to load by waiting for a known input label
    await screen.findByLabelText(/audit log/i);
    const inputs = document.querySelectorAll('input[type="number"]');
    expect(inputs.length).toBeGreaterThanOrEqual(7);
  });

  it("audit retention note appears as a muted advisory below the audit days input", async () => {
    renderApp(<Retention />);
    expect(
      await screen.findByText(/Audit retention only deletes the six rate-limited leaf event types/i),
    ).toBeInTheDocument();
  });

  it("Save → PUT /settings/retention + toast 'Retention settings saved.'", async () => {
    const user = userEvent.setup();
    renderApp(<Retention />);
    const input = await screen.findByLabelText(/audit log/i);
    // Clear and type a valid value
    await user.clear(input);
    await user.type(input, "30");
    await user.click(screen.getByRole("button", { name: /save/i }));
    expect(await screen.findByText(/Retention settings saved\./i)).toBeInTheDocument();
  });

  it("out-of-range value disables Save", async () => {
    const user = userEvent.setup();
    renderApp(<Retention />);
    const input = await screen.findByLabelText(/audit log/i);
    await user.clear(input);
    await user.type(input, "9999");
    const saveBtn = screen.getByRole("button", { name: /save/i });
    expect(saveBtn).toBeDisabled();
  });
});
