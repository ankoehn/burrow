import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MtlsPanel } from "@/components/MtlsPanel";

describe("MtlsPanel", () => {
  it("renders a mono textarea for the CA PEM", () => {
    render(<MtlsPanel value="" onChange={() => {}} />);
    const ta = screen.getByLabelText(/ca pem/i);
    expect(ta.tagName.toLowerCase()).toBe("textarea");
    expect(ta.className).toContain("mono");
  });

  it("Compute fingerprint calls crypto.subtle.digest and surfaces the hex", async () => {
    const fakeBuf = new Uint8Array([0xde, 0xad, 0xbe, 0xef]).buffer;
    const digestMock = vi.fn().mockResolvedValue(fakeBuf);
    Object.defineProperty(globalThis, "crypto", {
      value: { subtle: { digest: digestMock } },
      configurable: true,
    });
    render(<MtlsPanel value={"-----BEGIN CERTIFICATE-----\nMIIB...=\n-----END CERTIFICATE-----"} onChange={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: /compute fingerprint/i }));
    await waitFor(() => expect(digestMock).toHaveBeenCalledTimes(1));
    expect(digestMock.mock.calls[0]![0]).toBe("SHA-256");
    // Cross-realm Uint8Array (jsdom realm vs test realm) — verify shape, not identity.
    const arg = digestMock.mock.calls[0]![1] as { byteLength: number; BYTES_PER_ELEMENT: number };
    expect(arg.byteLength).toBeGreaterThan(0);
    expect(arg.BYTES_PER_ELEMENT).toBe(1);
    expect(await screen.findByText(/deadbeef/)).toBeInTheDocument();
  });
});
