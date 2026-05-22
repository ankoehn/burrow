import { describe, it, expect, beforeEach } from "vitest";
import { screen } from "@testing-library/react";
import { renderApp } from "@/mocks/test-utils";
import { db, resetDb } from "@/mocks/db";
import DatabaseBackend from "@/pages/DatabaseBackend";

beforeEach(() => resetDb());

describe("Database backend", () => {
  it("renders heading 'Database backend' and the driver chip", async () => {
    renderApp(<DatabaseBackend />);
    expect(await screen.findByRole("heading", { name: /database backend/i })).toBeInTheDocument();
    // Driver chip: seeded as "sqlite"
    expect(await screen.findByText("sqlite")).toBeInTheDocument();
  });

  it("when postgres_alpha is true, renders the amber banner verbatim", async () => {
    // Override the seed before mounting
    db.databaseStatus = {
      driver: "postgres",
      postgres_alpha: true,
      url_redacted: "postgres://burrow:****@host/db",
    };
    renderApp(<DatabaseBackend />);
    expect(
      await screen.findByText(/Postgres backend is alpha — see release notes\./),
    ).toBeInTheDocument();
    // url_redacted should be visible
    expect(screen.getByText("postgres://burrow:****@host/db")).toBeInTheDocument();
  });

  it("renders the 'How is this configured?' disclosure verbatim", async () => {
    renderApp(<DatabaseBackend />);
    await screen.findByRole("heading", { name: /database backend/i });
    // The disclosure text includes both env-var tokens
    expect(screen.getByText(/BURROW_DATABASE_URL/)).toBeInTheDocument();
    expect(screen.getByText(/experimental\.postgres_backend/)).toBeInTheDocument();
  });
});
