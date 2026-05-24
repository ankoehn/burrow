import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiFetch, ApiError } from "@/lib/api";
import { Button, Input } from "@/components/ds";
import { ACCESS_MODES, type AccessMode } from "@/lib/contract";
import { ApiKeysPanel } from "@/components/ApiKeysPanel";
import { AccessPolicyEditor } from "@/components/AccessPolicyEditor";
import { MtlsPanel } from "@/components/MtlsPanel";
import { IPGeoPanel } from "@/components/IPGeoPanel";
import { Toaster } from "@/components/ui/sonner";
import { toast } from "sonner";

// Mode explainers — kept verbatim from the v0.2.0 component (UI_DESIGN §4.15);
// v0.3.0 drops the disabled gating and v0.4.0 adds the mtls row.
const META: Record<AccessMode, { title: string; help: string }> = {
  open: { title: "Open — raw passthrough", help: "Burrow adds nothing. The only mode available in v0.2.0; safe default for TCP tunnels." },
  api_key: { title: "API key — header check", help: "Burrow verifies an API key header before proxying." },
  burrow_login: { title: "Burrow login — role-based", help: "Visitors sign in with their Burrow account at a hosted gate." },
  mtls: { title: "mTLS — client certificate", help: "Burrow terminates TLS and requires a client certificate chained to a CA you upload." },
};

export function AccessModePanel({ serviceId, serviceName, mode, clientId }: { serviceId: string; serviceName: string; mode: AccessMode; clientId?: string }) {
  const qc = useQueryClient();
  const [selected, setSelected] = useState<AccessMode>(mode);
  // RFC 7230 header-name tokens disallow colon + whitespace; the prior
  // default "Authorization: Bearer" persisted as an uninterpretable header
  // and the backend (api/service_handlers.go: isValidHTTPHeaderName) now
  // rejects it with 400. "Authorization" is the proxy's safe default; it
  // also strips a "Bearer " prefix for that header only.
  const [apiKeyHeader, setApiKeyHeader] = useState("Authorization");
  const [caPem, setCaPem] = useState("");
  const [err, setErr] = useState<string | null>(null);

  function buildBody(): Record<string, unknown> {
    if (selected === "api_key") return { access_mode: selected, api_key_header: apiKeyHeader };
    if (selected === "mtls") return { access_mode: selected, ca_pem: caPem };
    return { access_mode: selected };
  }

  const save = useMutation({
    mutationFn: () =>
      apiFetch(`/services/${serviceId}/access-mode`, {
        method: "PUT",
        body: JSON.stringify(buildBody()),
      }),
    onSuccess: () => {
      setErr(null);
      qc.invalidateQueries({ queryKey: ["services"] });
      qc.invalidateQueries({ queryKey: ["service", serviceId] });
      if (clientId) qc.invalidateQueries({ queryKey: ["client", clientId] });
      toast.success(`Access settings saved. ${serviceName} · mode: ${selected}`);
    },
    onError: (e: unknown) => {
      if (e instanceof ApiError && e.status === 403) {
        setErr("You don't have permission to configure this service.");
      } else if (e instanceof ApiError) {
        setErr(e.message);
      } else {
        setErr("Couldn't save access settings.");
      }
    },
  });

  return (
    <div className="access-panel">
      <div role="radiogroup" aria-label="Access mode" className="mode-list">
        {ACCESS_MODES.map((m) => {
          const meta = META[m];
          return (
            <div
              key={m}
              role="radio"
              aria-label={meta.title}
              aria-checked={selected === m}
              tabIndex={0}
              className="mode-card"
              onClick={() => setSelected(m)}
              onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); setSelected(m); } }}
            >
              <strong>{meta.title}</strong>
              <p className="muted">{meta.help}</p>
            </div>
          );
        })}
      </div>

      {selected === "api_key" && (
        <div className="mode-detail">
          <div className="field">
            <label htmlFor={`api-key-header-${serviceId}`}>API key header</label>
            <Input
              id={`api-key-header-${serviceId}`}
              className="mono"
              value={apiKeyHeader}
              onChange={(e) => setApiKeyHeader(e.target.value)}
            />
          </div>
          <ApiKeysPanel serviceId={serviceId} />
        </div>
      )}

      {selected === "burrow_login" && (
        <div className="mode-detail">
          <AccessPolicyEditor serviceId={serviceId} />
        </div>
      )}

      {selected === "mtls" && (
        <div className="mode-detail">
          <MtlsPanel value={caPem} onChange={setCaPem} />
        </div>
      )}

      {err && <p role="alert" className="notice-inline">{err}</p>}

      <div className="actions">
        <Button variant="primary" size="sm" disabled={save.isPending} onClick={() => save.mutate()}>
          {save.isPending ? "Saving…" : "Save changes"}
        </Button>
      </div>

      <IPGeoPanel serviceId={serviceId} />
      <Toaster />
    </div>
  );
}
