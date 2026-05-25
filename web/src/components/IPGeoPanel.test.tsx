import { describe, it, expect, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { renderApp } from "@/mocks/test-utils";
import { server } from "@/mocks/server";
import { IPGeoPanel } from "@/components/IPGeoPanel";

describe("IPGeoPanel", () => {
  it("renders the verbatim 'Allow everywhere.' empty state when both lists empty", async () => {
    renderApp(<IPGeoPanel serviceId="svc_ai001" />);
    expect(await screen.findByText("Allow everywhere.")).toBeInTheDocument();
  });

  it("Add CIDR validates basic shape and PUTs /services/:id/ipgeo on save", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    renderApp(<IPGeoPanel serviceId="svc_ai001" />);
    await screen.findByText("Allow everywhere.");
    await userEvent.click(screen.getByRole("button", { name: /add cidr/i }));
    // The dialog opens with an input + list selector (allow / block).
    const input = await screen.findByLabelText(/^cidr$/i);
    // Bad shape: no slash. The form's submit should keep the dialog open.
    await userEvent.type(input, "garbage");
    await userEvent.click(screen.getByRole("button", { name: /^add$/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/invalid cidr/i);
    // Replace with a valid shape.
    await userEvent.clear(input);
    await userEvent.type(input, "10.0.0.0/8");
    await userEvent.click(screen.getByRole("button", { name: /^add$/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url, init]) =>
          String(url).endsWith("/api/v1/services/svc_ai001/ipgeo")
          && (init as RequestInit | undefined)?.method === "PUT",
        ),
      ).toBe(true);
    });
  });

  // P1-4 — copy softened: no developer language ("compiled", "build flag").
  it("surfaces the user-facing banner when /geo/status reports enabled:false (P1-4)", async () => {
    server.use(
      http.get("/api/v1/geo/status", () =>
        HttpResponse.json({ enabled: false, db_path: "", db_age_seconds: 0 })),
    );
    renderApp(<IPGeoPanel serviceId="svc_ai001" />);
    expect(
      await screen.findByText("Geo restrictions aren't available on this relay."),
    ).toBeInTheDocument();
  });
});
