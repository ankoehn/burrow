import { describe, it, expect } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import { AccessModePanel } from "@/components/AccessModePanel";

describe("AccessModePanel", () => {
  it("renders three radios; only Open is enabled", async () => {
    renderApp(<AccessModePanel serviceId="tnl_web01" serviceName="web-staging" mode="open" clientId="sess_4f7a9c0b2e81" />);
    const group = screen.getByRole("radiogroup", { name: /access mode/i });
    const radios = screen.getAllByRole("radio");
    expect(radios).toHaveLength(3);
    expect(screen.getByRole("radio", { name: /open/i })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("radio", { name: /api key/i })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByRole("radio", { name: /burrow login/i })).toHaveAttribute("aria-disabled", "true");
    expect(group).toBeInTheDocument();
  });

  it("disabled modes do not change the selection", async () => {
    renderApp(<AccessModePanel serviceId="tnl_web01" serviceName="web-staging" mode="open" clientId="sess_4f7a9c0b2e81" />);
    await userEvent.click(screen.getByRole("radio", { name: /api key/i }));
    expect(screen.getByRole("radio", { name: /open/i })).toHaveAttribute("aria-checked", "true");
  });

  it("saving Open issues PUT and toasts success", async () => {
    renderApp(<AccessModePanel serviceId="tnl_web01" serviceName="web-staging" mode="open" clientId="sess_4f7a9c0b2e81" />);
    await userEvent.click(screen.getByRole("button", { name: /save changes/i }));
    expect(await screen.findByText(/access settings saved/i)).toBeInTheDocument();
  });
});
