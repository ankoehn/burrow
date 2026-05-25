import { useId, useState, type ChangeEvent } from "react";
import { Button } from "@/components/ds";

function toHex(buf: ArrayBuffer): string {
  return Array.from(new Uint8Array(buf))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

export interface MtlsPanelProps {
  value: string;
  onChange: (next: string) => void;
}

export function MtlsPanel({ value, onChange }: MtlsPanelProps) {
  const id = useId();
  const [fingerprint, setFingerprint] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function compute() {
    if (!value || typeof crypto === "undefined" || !crypto.subtle) return;
    setBusy(true);
    try {
      const bytes = new TextEncoder().encode(value);
      const digest = await crypto.subtle.digest("SHA-256", bytes);
      setFingerprint(toHex(digest));
    } finally {
      setBusy(false);
    }
  }

  // P2-3 — file upload alternative: lets operators pick a .pem/.crt off
  // disk instead of pasting. We don't validate content here; the backend
  // already rejects invalid CA bundles with a 400, and PEM happens to be
  // the only format we accept (the hint says so).
  async function onFile(e: ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0];
    if (!f) return;
    const text = await f.text();
    onChange(text);
    // Reset the input so picking the SAME file twice still fires onChange.
    e.target.value = "";
  }

  return (
    <div className="mtls-panel">
      <div className="field">
        <label htmlFor={id}>CA PEM</label>
        <input
          type="file"
          accept=".pem,.crt,.cer,application/x-pem-file,application/x-x509-ca-cert"
          aria-label="Upload CA bundle"
          onChange={(e) => { void onFile(e); }}
          style={{ marginBottom: 6 }}
        />
        <textarea
          id={id}
          className="input mono"
          rows={8}
          spellCheck={false}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="-----BEGIN CERTIFICATE-----&#10;…&#10;-----END CERTIFICATE-----"
        />
        <p className="muted" style={{ marginTop: 4, fontSize: 12 }}>
          Paste a PEM-encoded CA bundle, or upload a <code>.pem</code> /
          {" "}<code>.crt</code> file.
        </p>
      </div>
      <div className="row gap-2" style={{ alignItems: "center" }}>
        <Button variant="secondary" size="sm" disabled={busy || !value} onClick={() => { void compute(); }}>
          {busy ? "Computing…" : "Compute fingerprint"}
        </Button>
        {fingerprint && (
          <code className="mono small" aria-label="CA fingerprint">
            sha256:{fingerprint}
          </code>
        )}
      </div>
    </div>
  );
}
