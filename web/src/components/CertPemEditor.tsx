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
          className="input mono resizable"
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
          className="input mono resizable"
          rows={6}
          value={value.key_pem}
          onChange={(e) => onChange({ ...value, key_pem: e.target.value })}
          placeholder="-----BEGIN PRIVATE KEY-----&#10;…&#10;-----END PRIVATE KEY-----"
        />
      </div>

      <div className="cert-actions">
        <Button variant="secondary" size="sm" type="button" onClick={() => void handleValidate()}>
          Validate
        </Button>
        <p className="muted small">
          Client-side checks are advisory. The server runs SAN/chain/key validation before saving.
        </p>
      </div>

      {validation && (
        <div className="cert-validation-status">
          {validation.certOk ? (
            <span className="mono ok">
              Certificate block detected.
              {validation.fingerprint && (
                <> SHA-256: <span className="mono">{validation.fingerprint}</span></>
              )}
              {validation.fingerprintError && (
                <> ({validation.fingerprintError})</>
              )}
            </span>
          ) : (
            <span className="err">{validation.certError}</span>
          )}

          {validation.keyOk ? (
            <span className="ok">Private key block detected.</span>
          ) : (
            <span className="err">{validation.keyError}</span>
          )}
        </div>
      )}
    </div>
  );
}
