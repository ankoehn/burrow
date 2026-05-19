import { describe, it, expect } from "vitest";
import { screen, within } from "@testing-library/react";
import { Routes, Route } from "react-router-dom";
import { renderApp } from "@/mocks/test-utils";
import ClientDetail from "@/pages/ClientDetail";

function mount() {
  return renderApp(
    <Routes><Route path="/clients/:id" element={<ClientDetail />} /></Routes>,
    "/clients/sess_4f7a9c0b2e81",
  );
}

describe("Client detail", () => {
  it("shows client metadata and its services", async () => {
    mount();
    expect(await screen.findByRole("heading", { name: /office-box-1/i })).toBeInTheDocument();
    const svcTable = screen.getByRole("table", { name: /services/i });
    expect(within(svcTable).getByText("web-staging")).toBeInTheDocument();
    expect(within(svcTable).getByText(":9000")).toBeInTheDocument();
  });

  it("shows 'client not found' for an unknown id", async () => {
    renderApp(
      <Routes><Route path="/clients/:id" element={<ClientDetail />} /></Routes>,
      "/clients/nope",
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(/not found/i);
  });
});
