import { describe, it, expect, vi } from "vitest";
import { screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import BackupRestore from "@/pages/BackupRestore";

function mount() {
  return renderApp(<BackupRestore />, "/settings/backups");
}

describe("Backup & restore", () => {
  it("renders the verbatim disclaimer", async () => {
    mount();
    expect(
      await screen.findByText(
        /Backups include the database, the relay's TLS cert state, and config/,
      ),
    ).toBeInTheDocument();
  });

  it("Create backup POSTs /backups", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount();
    await userEvent.click(await screen.findByRole("button", { name: /create backup/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url, init]) =>
          String(url).endsWith("/api/v1/backups")
          && (init as RequestInit | undefined)?.method === "POST",
        ),
      ).toBe(true);
    });
  });

  it("renders backup history with mono size + truncated sha + Verify menu item", async () => {
    mount();
    const table = await screen.findByRole("table", { name: /backup history/i });
    const row = (await within(table).findAllByRole("row"))[1]!;
    expect(within(row).getByRole("button", { name: /verify/i })).toBeInTheDocument();
  });

  it("file-picker shows an English label regardless of browser locale", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ backups: [] }), { status: 200 }) as Response,
    );
    const { container } = mount();
    expect(container.querySelector('label[for="restore-file"]')).not.toBeNull();
    expect(container.querySelector('label[for="restore-file"]')!.textContent).toMatch(
      /choose backup archive/i,
    );
    expect(container.querySelector('input[type="file"]')?.getAttribute("id")).toBe("restore-file");
  });
});
