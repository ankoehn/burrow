import { describe, it, expect, vi } from "vitest";
import { screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import { CustomRoleEditor } from "@/components/CustomRoleEditor";

function mountNew() {
  return renderApp(<CustomRoleEditor open roleName={null} onClose={() => {}} />);
}
function mountEdit(name = "analyst") {
  return renderApp(<CustomRoleEditor open roleName={name} onClose={() => {}} />);
}

describe("CustomRoleEditor (§4.27)", () => {
  it("New mode shows a Create button and posts /roles on save", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mountNew();
    const dlg = await screen.findByRole("dialog");
    expect(within(dlg).getByRole("button", { name: /^create$/i })).toBeInTheDocument();
    await userEvent.type(within(dlg).getByLabelText(/^name$/i), "newrole");
    await userEvent.click(within(dlg).getByRole("tab", { name: /permissions/i }));
    // toggle a permission switch via aria-label
    const sw = await within(dlg).findByRole("switch", { name: "audit:read" });
    await userEvent.click(sw);
    await userEvent.click(within(dlg).getByRole("button", { name: /^create$/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url, init]) =>
          String(url).endsWith("/api/v1/roles")
          && (init as RequestInit | undefined)?.method === "POST",
        ),
      ).toBe(true);
    });
  });

  it("Edit mode pre-fills name (disabled) + description and Save PUTs /roles/:name", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mountEdit();
    const dlg = await screen.findByRole("dialog");
    await waitFor(() => expect(within(dlg).getByLabelText(/^name$/i)).toHaveValue("analyst"));
    expect(within(dlg).getByLabelText(/^name$/i)).toBeDisabled();
    // tweak description and save
    const desc = within(dlg).getByLabelText(/description/i);
    await userEvent.clear(desc);
    await userEvent.type(desc, "Analyst, traffic & audit only");
    await userEvent.click(within(dlg).getByRole("button", { name: /^save$/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url, init]) =>
          String(url).endsWith("/api/v1/roles/analyst")
          && (init as RequestInit | undefined)?.method === "PUT",
        ),
      ).toBe(true);
    });
  });

  it("Delete role opens a confirm and DELETEs /roles/:name", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mountEdit();
    const dlg = await screen.findByRole("dialog");
    await waitFor(() => expect(within(dlg).getByLabelText(/^name$/i)).toHaveValue("analyst"));
    await userEvent.click(within(dlg).getByRole("button", { name: /delete role/i }));
    // Two Dialogs share aria-labelledby="dialog-title" so role+name lookup is
    // unreliable; anchor on the confirm body text instead.
    const confirmBody = await screen.findByText(/Delete role 'analyst'\? Users with this role/i);
    const confirm = confirmBody.closest("[role='dialog']") as HTMLElement;
    await userEvent.click(within(confirm).getByRole("button", { name: /^delete$/i }));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url, init]) =>
          String(url).endsWith("/api/v1/roles/analyst")
          && (init as RequestInit | undefined)?.method === "DELETE",
        ),
      ).toBe(true);
    });
  });
});
