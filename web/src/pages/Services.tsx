import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Copy } from "lucide-react";
import { apiFetch, ApiError } from "@/lib/api";
import { Button, Badge, Dialog, SkeletonRows, ErrorNotice } from "@/components/ds";
import type { Service, AccessMode } from "@/lib/contract";
import { AccessModePanel } from "@/components/AccessModePanel";

const ACCESS_LABEL: Record<AccessMode, string> = {
  open: "Open",
  api_key: "API key",
  burrow_login: "Burrow login",
  mtls: "mTLS",
};

function copy(text: string) {
  void navigator.clipboard?.writeText(text);
}

export default function Services() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["services"],
    queryFn: () => apiFetch<Service[]>("/services"),
    retry: false,
  });
  const [configure, setConfigure] = useState<Service | null>(null);

  return (
    <div className="services-page" style={{ position: "relative" }}>
      <div className="page-head">
        <div>
          <h1>Services</h1>
          <p className="sub">Durable services exposed through this relay, with their access configuration.</p>
        </div>
      </div>

      {error ? (
        <ErrorNotice
          action={<Button variant="secondary" size="sm" onClick={() => void refetch()}>Retry</Button>}
        >
          Couldn't load services: {error instanceof ApiError ? error.message : "Unknown error"}
        </ErrorNotice>
      ) : isLoading ? (
        <div className="table-wrap" style={{ padding: 16 }}>
          <SkeletonRows n={4} />
        </div>
      ) : !data || data.length === 0 ? (
        <div className="state-card">
          <p>No services yet. Run <code>burrow connect</code> with <code>--type http</code>.</p>
        </div>
      ) : (
        <div className="table-wrap">
          <table className="data" aria-label="Services">
            <thead>
              <tr>
                <th>Name</th>
                <th>Type</th>
                <th>Hostname</th>
                <th>Access</th>
                <th>Status</th>
                <th className="col-actions"></th>
              </tr>
            </thead>
            <tbody>
              {data.map((s) => (
                <tr key={s.id}>
                  <td className="col-name">{s.name}</td>
                  <td><Badge kind={`type-${s.type}`} nodot>{s.type}</Badge></td>
                  <td>
                    {s.type === "http" && s.hostname ? (
                      <span className="row gap-2" style={{ alignItems: "center" }}>
                        <span className="mono">{s.hostname}</span>
                        <button
                          type="button"
                          className="icon-btn"
                          aria-label={`Copy hostname ${s.hostname}`}
                          onClick={() => copy(s.hostname)}
                        >
                          <Copy size={13} />
                        </button>
                      </span>
                    ) : (
                      <span className="muted">—</span>
                    )}
                  </td>
                  <td><Badge kind={`access-${s.access_mode}`} nodot>{ACCESS_LABEL[s.access_mode]}</Badge></td>
                  <td>
                    {s.connected
                      ? <Badge kind="status-connected">connected</Badge>
                      : <span className="muted">idle</span>}
                  </td>
                  <td className="col-actions">
                    <Button variant="secondary" size="sm" onClick={() => setConfigure(s)}>Configure</Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Dialog
        open={configure !== null}
        onOpenChange={(o) => { if (!o) setConfigure(null); }}
        title={configure ? `Access · ${configure.name}` : ""}
        description={configure?.type === "tcp"
          ? "Raw TCP service — only Open passthrough applies."
          : "Choose how Burrow gates requests before proxying to this service."}
        footer={<Button variant="secondary" onClick={() => setConfigure(null)}>Close</Button>}
      >
        {configure && (
          <AccessModePanel
            serviceId={configure.id}
            serviceName={configure.name}
            mode={configure.access_mode}
            clientId={`svc:${configure.id}`}
          />
        )}
      </Dialog>
    </div>
  );
}
