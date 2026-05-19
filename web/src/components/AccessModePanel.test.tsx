import { describe, it, expect } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import { db } from "@/mocks/db";
import { AccessModePanel } from "@/components/AccessModePanel";

describe("AccessModePanel (v0.3.0)", () => {
  it("drops the v0.2.0 disabled gating — all three modes are selectable", async () => {
    renderApp(<AccessModePanel serviceId="svc_web01" serviceName="web" mode="open" />);
    expect(screen.queryByText(/needs http tunnels/i)).toBeNull();
    const radios = screen.getAllByRole("radio");
    expect(radios).toHaveLength(3);
    for (const r of radios) expect(r).not.toHaveAttribute("aria-disabled", "true");
    await userEvent.click(screen.getByRole("radio", { name: /api key/i }));
    expect(screen.getByRole("radio", { name: /api key/i })).toHaveAttribute("aria-checked", "true");
  });

  it("api_key reveals the mono header field and mounts ApiKeysPanel", async () => {
    renderApp(<AccessModePanel serviceId="svc_web01" serviceName="web" mode="open" />);
    await userEvent.click(screen.getByRole("radio", { name: /api key/i }));
    const header = screen.getByLabelText(/api key header/i);
    expect(header).toHaveValue("Authorization: Bearer");
    expect(header.className).toContain("mono");
    expect(
      await screen.findByText("Managing keys needs the services:configure permission."),
    ).toBeInTheDocument();
  });

  it("burrow_login mounts the AccessPolicyEditor", async () => {
    renderApp(<AccessModePanel serviceId="svc_web01" serviceName="web" mode="open" />);
    await userEvent.click(screen.getByRole("radio", { name: /burrow login/i }));
    expect(
      await screen.findByText(/SSO: one Burrow login covers every protected service/i),
    ).toBeInTheDocument();
  });

  it("saving issues PUT /services/:id/access-mode and toasts success", async () => {
    renderApp(<AccessModePanel serviceId="svc_web01" serviceName="web" mode="open" />);
    await userEvent.click(screen.getByRole("radio", { name: /api key/i }));
    await userEvent.click(screen.getByRole("button", { name: /save changes/i }));
    expect((await screen.findAllByText(/access settings saved/i)).length).toBeGreaterThan(0);
  });

  it("a 409 on a tcp service surfaces the contract message verbatim", async () => {
    renderApp(<AccessModePanel serviceId="svc_pg001" serviceName="postgres" mode="open" />);
    await userEvent.click(screen.getByRole("radio", { name: /api key/i }));
    await userEvent.click(screen.getByRole("button", { name: /save changes/i }));
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "api_key and burrow_login require an http service",
      ),
    );
  });

  it("a 403 surfaces a friendly permission message", async () => {
    db.me = { id: "bur_usr_other", email: "x@y.io", role: "user" };
    renderApp(<AccessModePanel serviceId="svc_web01" serviceName="web" mode="open" />);
    await userEvent.click(screen.getByRole("radio", { name: /api key/i }));
    await userEvent.click(screen.getByRole("button", { name: /save changes/i }));
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "You don't have permission to configure this service.",
      ),
    );
  });
});
