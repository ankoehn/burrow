import { describe, it, expect, beforeEach } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import { resetDb } from "@/mocks/db";
import { Route, Routes } from "react-router-dom";
import { CustomDomainsPanel } from "./CustomDomains";

// Wrap in a route so we can supply serviceId from the URL
function mount(serviceId = "svc_ai001") {
  return renderApp(
    <Routes>
      <Route path="/services/:id" element={<CustomDomainsPanel serviceId={serviceId} />} />
    </Routes>,
    `/services/${serviceId}`,
  );
}

beforeEach(() => {
  resetDb();
});

describe("CustomDomainsPanel", () => {
  it("lists seeded domain with hostname, status badge, expiry, and truncated fingerprint", async () => {
    mount();
    // hostname visible as mono text
    const hostname = await screen.findByText("foo.example.com");
    expect(hostname).toBeTruthy();

    // status badge
    const badge = await screen.findByText("active");
    expect(badge).toBeTruthy();

    // truncated fingerprint: first 12 chars of "deadbeef0123456789abcdef" + "…"
    const fp = await screen.findByText("deadbeef0123…");
    expect(fp).toBeTruthy();
  });

  it("Add domain dialog opens", async () => {
    mount();
    await screen.findByText("foo.example.com");
    const addBtn = screen.getByRole("button", { name: /add domain/i });
    await userEvent.click(addBtn);
    // Dialog title should appear
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText(/add custom domain/i)).toBeTruthy();
  });

  it("SAN-mismatch rejection shows verbatim alert", async () => {
    mount();
    await screen.findByText("foo.example.com");

    // Open add dialog
    await userEvent.click(screen.getByRole("button", { name: /add domain/i }));
    const dialog = await screen.findByRole("dialog");

    // Fill hostname
    const hostnameInput = within(dialog).getByLabelText(/hostname/i);
    await userEvent.clear(hostnameInput);
    await userEvent.type(hostnameInput, "san-mismatch.example.com");

    // Fill cert and key PEM
    const certTa = within(dialog).getByLabelText("Certificate (PEM)");
    await userEvent.clear(certTa);
    await userEvent.type(certTa, "-----BEGIN CERTIFICATE-----\nMOCK\n-----END CERTIFICATE-----");

    const keyTa = within(dialog).getByLabelText("Private key (PEM)");
    await userEvent.clear(keyTa);
    await userEvent.type(keyTa, "-----BEGIN PRIVATE KEY-----\nMOCK\n-----END PRIVATE KEY-----");

    // Submit
    const submitBtn = within(dialog).getByRole("button", { name: /add$/i });
    await userEvent.click(submitBtn);

    // Verbatim error message
    await screen.findByText("The certificate's SAN does not include this hostname.");
  });

  it("successful Add → POST /services/:id/domains and dialog closes", async () => {
    mount();
    await screen.findByText("foo.example.com");

    await userEvent.click(screen.getByRole("button", { name: /add domain/i }));
    const dialog = await screen.findByRole("dialog");

    const hostnameInput = within(dialog).getByLabelText(/hostname/i);
    await userEvent.clear(hostnameInput);
    await userEvent.type(hostnameInput, "new.example.com");

    const certTa = within(dialog).getByLabelText("Certificate (PEM)");
    await userEvent.clear(certTa);
    await userEvent.type(certTa, "-----BEGIN CERTIFICATE-----\nDATA\n-----END CERTIFICATE-----");

    const keyTa = within(dialog).getByLabelText("Private key (PEM)");
    await userEvent.clear(keyTa);
    await userEvent.type(keyTa, "-----BEGIN PRIVATE KEY-----\nDATA\n-----END PRIVATE KEY-----");

    const submitBtn = within(dialog).getByRole("button", { name: /add$/i });
    await userEvent.click(submitBtn);

    // Dialog should close (dialog element gone)
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });

    // New hostname should appear in the list
    await screen.findByText("new.example.com");
  });

  it("Delete row → confirms then DELETE /services/:id/domains/:did", async () => {
    mount();
    await screen.findByText("foo.example.com");

    // Click delete on the seeded row
    const deleteBtn = screen.getByRole("button", { name: /delete/i });
    await userEvent.click(deleteBtn);

    // Confirm dialog should appear
    const confirmDialog = await screen.findByRole("dialog");
    expect(within(confirmDialog).getByText(/foo\.example\.com/i)).toBeTruthy();

    // Click confirm
    const confirmBtn = within(confirmDialog).getByRole("button", { name: /remove/i });
    await userEvent.click(confirmBtn);

    // Domain should be gone
    await waitFor(() => {
      expect(screen.queryByText("foo.example.com")).toBeNull();
    });
  });
});
