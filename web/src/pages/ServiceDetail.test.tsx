import { describe, it, expect } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import { Route, Routes } from "react-router-dom";
import ServiceDetail from "@/pages/ServiceDetail";

function mount() {
  return renderApp(
    <Routes>
      <Route path="/services/:id" element={<ServiceDetail />} />
    </Routes>,
    "/services/svc_ai001",
  );
}

describe("ServiceDetail page", () => {
  it("renders service name in the heading", async () => {
    mount();
    const heading = await screen.findByRole("heading", { name: /ollama/i });
    expect(heading.textContent).toMatch(/ollama/i);
  });

  it("shows four tabs: Access, API keys, Upstream key, Custom domains", async () => {
    mount();
    await screen.findByRole("heading", { name: /ollama/i });
    const tablist = screen.getByRole("tablist");
    const tabs = within(tablist).getAllByRole("tab");
    const labels = tabs.map((t) => t.textContent?.trim());
    expect(labels).toContain("Access");
    expect(labels).toContain("API keys");
    expect(labels).toContain("Upstream key");
    expect(labels).toContain("Custom domains");
  });

  it("Upstream key tab renders the binding fields when a binding exists", async () => {
    mount();
    await screen.findByRole("heading", { name: /ollama/i });
    // Switch to Upstream key tab
    const upstreamTab = screen.getByRole("tab", { name: /upstream key/i });
    await userEvent.click(upstreamTab);
    // The binding for svc_ai001 has header_name=Authorization and header_format=Bearer {key}
    const headerNameInput = await screen.findByLabelText(/header name/i);
    expect((headerNameInput as HTMLInputElement).value).toBe("Authorization");
    const headerFormatInput = screen.getByLabelText(/header format/i);
    expect((headerFormatInput as HTMLInputElement).value).toBe("Bearer {key}");
  });
});
