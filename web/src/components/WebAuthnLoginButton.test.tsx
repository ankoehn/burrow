import { describe, it, expect, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "@/mocks/test-utils";
import { Route, Routes } from "react-router-dom";
import { WebAuthnLoginButton } from "@/components/WebAuthnLoginButton";

function fakeAssertion(): unknown {
  const buf = new Uint8Array([4, 5, 6]).buffer;
  return {
    id: "cred-1",
    rawId: buf,
    type: "public-key",
    response: {
      clientDataJSON: buf,
      authenticatorData: buf,
      signature: buf,
      userHandle: buf,
    },
  };
}

describe("WebAuthnLoginButton", () => {
  it("runs begin → ceremony → finish and navigates to '/'", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const get = vi.fn().mockResolvedValue(fakeAssertion());
    Object.defineProperty(navigator, "credentials", {
      value: { create: vi.fn(), get },
      configurable: true,
    });
    renderApp(
      <Routes>
        <Route path="/" element={<div>HOME</div>} />
        <Route path="/login" element={<WebAuthnLoginButton />} />
      </Routes>,
      "/login",
    );
    await userEvent.click(screen.getByRole("button", { name: /sign in with a passkey/i }));
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    await waitFor(() => {
      expect(
        fetchSpy.mock.calls.some(([url, init]) =>
          String(url).endsWith("/api/v1/auth/webauthn/login/finish")
          && (init as RequestInit | undefined)?.method === "POST",
        ),
      ).toBe(true);
    });
    expect(await screen.findByText("HOME")).toBeInTheDocument();
  });
});
