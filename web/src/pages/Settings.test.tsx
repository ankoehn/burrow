import { describe, it, expect } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import Settings from "@/pages/Settings";

describe("Settings / SMTP", () => {
  it("shows the unconfigured notice initially", async () => {
    renderApp(<Settings />);
    expect(await screen.findByText(/Email isn't set up yet/i)).toBeInTheDocument();
  });

  it("saves whitelisted SMTP settings", async () => {
    renderApp(<Settings />);
    await screen.findByLabelText(/SMTP server/i);
    await userEvent.type(screen.getByLabelText(/SMTP server/i), "mx.acme.io");
    await userEvent.type(screen.getByLabelText(/^Port$/i), "587");
    await userEvent.click(screen.getByRole("button", { name: /save settings/i }));
    expect(await screen.findByText(/settings saved/i)).toBeInTheDocument();
  });

  it("surfaces the 409 when testing email while unconfigured", async () => {
    renderApp(<Settings />);
    await screen.findByLabelText(/SMTP server/i);
    await userEvent.click(screen.getByRole("button", { name: /send test email/i }));
    await userEvent.type(screen.getByLabelText(/test recipient/i), "ops@acme.io");
    await userEvent.click(screen.getByRole("button", { name: /^test now$/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/not configured/i);
  });
});

describe("Settings / nav cards", () => {
  it("renders the five new nav cards (Retention, Database, Backup, OpenAPI viewer, Custom domains)", async () => {
    renderApp(<Settings />);
    // Wait for the page to be ready
    await screen.findByLabelText(/SMTP server/i);
    expect(screen.getByText("Retention & compliance")).toBeInTheDocument();
    expect(screen.getByText("Database backend")).toBeInTheDocument();
    expect(screen.getByText("Backup & restore")).toBeInTheDocument();
    expect(screen.getByText("OpenAPI viewer")).toBeInTheDocument();
    expect(screen.getByText("Custom domains")).toBeInTheDocument();
    // OpenAPI viewer must be an <a> with target="_blank"
    const openApiLink = screen.getByRole("link", { name: /openapi viewer/i });
    expect(openApiLink).toHaveAttribute("target", "_blank");
  });

  it("existing SMTP form is still rendered below the nav cards", async () => {
    renderApp(<Settings />);
    expect(await screen.findByLabelText(/SMTP server/i)).toBeInTheDocument();
  });
});
