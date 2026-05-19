import { describe, it, expect } from "vitest";
import { screen, within } from "@testing-library/react";
import { renderApp } from "@/mocks/test-utils";
import { Routes, Route } from "react-router-dom";
import Clients from "@/pages/Clients";

describe("Clients overview", () => {
  it("lists connected clients with platform and traffic", async () => {
    renderApp(
      <Routes><Route path="/clients" element={<Clients />} /></Routes>,
      "/clients",
    );
    expect(await screen.findByText("office-box-1")).toBeInTheDocument();
    const row = screen.getByText("office-box-1").closest("tr")!;
    expect(within(row).getByText(/linux/i)).toBeInTheDocument();
  });

  it("has a link to client detail", async () => {
    renderApp(
      <Routes><Route path="/clients" element={<Clients />} /></Routes>,
      "/clients",
    );
    const row = (await screen.findByText("office-box-1")).closest("tr")!;
    expect(within(row).getByRole("link", { name: /view/i })).toHaveAttribute("href", "/clients/sess_4f7a9c0b2e81");
  });
});
