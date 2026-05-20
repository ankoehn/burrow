import { useId, useState } from "react";
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

  return (
    <div className="mtls-panel">
      <div className="field">
        <label htmlFor={id}>CA PEM</label>
        <textarea
          id={id}
          className="input mono"
          rows={8}
          spellCheck={false}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="-----BEGIN CERTIFICATE-----&#10;…&#10;-----END CERTIFICATE-----"
        />
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
