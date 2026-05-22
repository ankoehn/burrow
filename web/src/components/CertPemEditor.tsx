import { useState } from "react";
import { Button } from "@/components/ds";

export interface CertPemEditorValue {
  cert_pem: string;
  key_pem: string;
}

interface ValidationState {
  certOk: boolean;
  keyOk: boolean;
  certError?: string;
  keyError?: string;
  fingerprint?: string;
  fingerprintError?: string;
}

async function computeFingerprint(cert_pem: string): Promise<string> {
  try {
    const buf = new TextEncoder().encode(cert_pem);
    const digest = await globalThis.crypto.subtle.digest("SHA-256", buf);
    const hex = Array.from(new Uint8Array(digest))
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("");
    return hex;
  } catch {
    return "";
  }
}

const CERT_RE = /-----BEGIN CERTIFICATE-----/;
const KEY_RE = /-----BEGIN ([A-Z]+ )?PRIVATE KEY-----/;

export function CertPemEditor({
  value,
  onChange,
}: {
  value: CertPemEditorValue;
  onChange: (v: CertPemEditorValue) => void;
}) {
  const [validation, setValidation] = useState<ValidationState | null>(null);

  async function handleValidate() {
    const certOk = CERT_RE.test(value.cert_pem);
    const keyOk = KEY_RE.test(value.key_pem);

    const state: ValidationState = {
      certOk,
      keyOk,
      certError: certOk ? undefined : "No certificate block detected (missing -----BEGIN CERTIFICATE-----).",
      keyError: keyOk ? undefined : "No private key block detected (missing -----BEGIN PRIVATE KEY-----).",
    };

    if (certOk) {
      const fp = await computeFingerprint(value.cert_pem);
      if (fp) {
        state.fingerprint = fp;
      } else {
        state.fingerprintError = "Fingerprint unavailable in this environment.";
      }
    }

    setValidation(state);
  }

  return (
    <div className="cert-pem-editor">
      <div className="field">
        <label htmlFor="cert-pem-cert">Certificate (PEM)</label>
        <textarea
          id="cert-pem-cert"
          aria-label="Certificate (PEM)"
          className="mono"
          rows={6}
          value={value.cert_pem}
          onChange={(e) => onChange({ ...value, cert_pem: e.target.value })}
          placeholder="-----BEGIN CERTIFICATE-----&#10;…&#10;-----END CERTIFICATE-----"
        />
      </div>

      <div className="field">
        <label htmlFor="cert-pem-key">Private key (PEM)</label>
        <textarea
          id="cert-pem-key"
          aria-label="Private key (PEM)"
          className="mono"
          rows={6}
          value={value.key_pem}
          onChange={(e) => onChange({ ...value, key_pem: e.target.value })}
          placeholder="-----BEGIN PRIVATE KEY-----&#10;…&#10;-----END PRIVATE KEY-----"
        />
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 6, marginTop: 8 }}>
        <Button variant="secondary" size="sm" type="button" onClick={() => void handleValidate()}>
          Validate
        </Button>
        <p className="muted" style={{ fontSize: 12, margin: 0 }}>
          Client-side checks are advisory. The server runs SAN/chain/key validation before saving.
        </p>
      </div>

      {validation && (
        <div className="cert-validation-status" style={{ marginTop: 8, display: "flex", flexDirection: "column", gap: 4 }}>
          {validation.certOk ? (
            <span className="mono" style={{ fontSize: 12, color: "var(--success)" }}>
              Certificate block detected.
              {validation.fingerprint && (
                <> SHA-256: <span className="mono">{validation.fingerprint}</span></>
              )}
              {validation.fingerprintError && (
                <> ({validation.fingerprintError})</>
              )}
            </span>
          ) : (
            <span style={{ fontSize: 12, color: "var(--destructive)" }}>
              {validation.certError}
            </span>
          )}

          {validation.keyOk ? (
            <span style={{ fontSize: 12, color: "var(--success)" }}>
              Private key block detected.
            </span>
          ) : (
            <span style={{ fontSize: 12, color: "var(--destructive)" }}>
              {validation.keyError}
            </span>
          )}
        </div>
      )}
    </div>
  );
}
