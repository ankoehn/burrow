import { describe, it, expect, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import { WebAuthnRegisterButton } from "@/components/WebAuthnRegisterButton";

function fakeCredential(): unknown {
  const buf = new Uint8Array([1, 2, 3]).buffer;
  return {
    id: "cred-1",
    rawId: buf,
    type: "public-key",
    response: { clientDataJSON: buf, attestationObject: buf },
  };
}

describe("WebAuthnRegisterButton", () => {
  it("runs the begin → ceremony → finish round-trip", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const create = vi.fn().mockResolvedValue(fakeCredential());
    Object.defineProperty(navigator, "credentials", {
      value: { create, get: vi.fn() },
      configurable: true,
    });
    renderApp(<WebAuthnRegisterButton />);
    await userEvent.click(screen.getByRole("button", { name: /add a passkey/i }));
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url, init]) =>
          String(url).endsWith("/api/v1/auth/webauthn/register/finish")
          && (init as RequestInit | undefined)?.method === "POST",
        ),
      ).toBe(true);
    });
  });
});
