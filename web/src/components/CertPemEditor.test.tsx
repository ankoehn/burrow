import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CertPemEditor } from "./CertPemEditor";
import type { CertPemEditorValue } from "./CertPemEditor";

const VALID_CERT = `-----BEGIN CERTIFICATE-----
MIIDmTCCAoGgAwIBAgIUMOCK
-----END CERTIFICATE-----`;

const VALID_KEY = `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG
-----END PRIVATE KEY-----`;

function renderEditor(initial: CertPemEditorValue = { cert_pem: "", key_pem: "" }) {
  let current = initial;
  const onChange = vi.fn((v: CertPemEditorValue) => { current = v; });
  const utils = render(<CertPemEditor value={current} onChange={onChange} />);
  return { ...utils, onChange, getCurrent: () => current };
}

describe("CertPemEditor", () => {
  it("renders both textareas with the correct aria-labels", () => {
    renderEditor();
    expect(screen.getByLabelText("Certificate (PEM)")).toBeTruthy();
    expect(screen.getByLabelText("Private key (PEM)")).toBeTruthy();
  });

  it("renders the honesty disclosure verbatim", () => {
    renderEditor();
    expect(
      screen.getByText(
        "Client-side checks are advisory. The server runs SAN/chain/key validation before saving.",
      ),
    ).toBeTruthy();
  });

  it("Validate detects a missing CERTIFICATE block", async () => {
    renderEditor({ cert_pem: "not a pem block", key_pem: VALID_KEY });
    const btn = screen.getByRole("button", { name: /validate/i });
    await userEvent.click(btn);
    expect(screen.getByText(/no certificate block/i)).toBeTruthy();
  });

  it("Validate detects a missing PRIVATE KEY block", async () => {
    renderEditor({ cert_pem: VALID_CERT, key_pem: "not a key block" });
    const btn = screen.getByRole("button", { name: /validate/i });
    await userEvent.click(btn);
    expect(screen.getByText(/no private key block/i)).toBeTruthy();
  });

  it("Validate computes a SHA-256 fingerprint when both blocks present", async () => {
    // Ensure crypto.subtle.digest is available (jsdom may not have it)
    if (!globalThis.crypto?.subtle) {
      const mockDigest = vi.fn().mockResolvedValue(new Uint8Array(32).fill(0xab).buffer);
      vi.stubGlobal("crypto", { subtle: { digest: mockDigest } });
    }

    renderEditor({ cert_pem: VALID_CERT, key_pem: VALID_KEY });
    const btn = screen.getByRole("button", { name: /validate/i });
    await userEvent.click(btn);

    // Wait for async fingerprint computation
    await screen.findByText(/[0-9a-f]{16,}/i);
  });

  it("calls onChange when cert textarea changes", async () => {
    const { onChange } = renderEditor();
    const ta = screen.getByLabelText("Certificate (PEM)") as HTMLTextAreaElement;
    await userEvent.clear(ta);
    await userEvent.type(ta, "abc");
    expect(onChange).toHaveBeenCalled();
  });

  it("calls onChange when key textarea changes", async () => {
    const { onChange } = renderEditor();
    const ta = screen.getByLabelText("Private key (PEM)") as HTMLTextAreaElement;
    await userEvent.clear(ta);
    await userEvent.type(ta, "xyz");
    expect(onChange).toHaveBeenCalled();
  });
});
