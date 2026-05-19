import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "@/lib/api";
import { formatBytes } from "@/lib/format";
import { Badge, SkeletonRows } from "@/components/ds";

interface Tunnel {
  id: string; name: string; type: string; remote_port: number;
  local_addr: string; bytes_in: number; bytes_out: number; connected: boolean;
}

export default function Tunnels() {
  const qc = useQueryClient();
  // SSE is primary; poll every 30 s as a fallback when SSE is unavailable.
  const { data, isLoading } = useQuery({
    queryKey: ["tunnels"],
    queryFn: () => apiFetch<Tunnel[]>("/tunnels"),
    refetchInterval: 30000,
  });
  useEffect(() => {
    // NOTE: EventSource requires same-origin (the Go server must serve this SPA).
    const es = new EventSource("/api/v1/events");
    const onTunnels = () => qc.invalidateQueries({ queryKey: ["tunnels"] });
    es.addEventListener("tunnels", onTunnels);
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) {
        // Stream closed — session may have expired. Invalidate /me so the
        // centralized RequireAuth handler can redirect to /login if needed.
        es.close();
        qc.invalidateQueries({ queryKey: ["me"] });
      }
      // If readyState is CONNECTING the browser is auto-retrying; do nothing.
    };
    return () => {
      es.removeEventListener("tunnels", onTunnels);
      es.onerror = null;
      es.close();
    };
  }, [qc]);
  return (
    <div className="tunnels-page">
      <div className="page-head">
        <div>
          <h1>Tunnels</h1>
        </div>
      </div>
      {isLoading ? (
        <div className="table-wrap" style={{ padding: 16 }}>
          <SkeletonRows n={4} />
        </div>
      ) : !data || data.length === 0 ? (
        <div className="state-card">
          <div className="icon-bubble">
            <svg
              width={18}
              height={18}
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.7"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <circle cx="12" cy="4.5" r="2.5" />
              <path d="m10.2 6.3-3.9 3.9" />
              <circle cx="4.5" cy="12" r="2.5" />
              <path d="M7 12h10" />
              <circle cx="19.5" cy="12" r="2.5" />
              <path d="m13.8 17.7 3.9-3.9" />
              <circle cx="12" cy="19.5" r="2.5" />
            </svg>
          </div>
          <p>No live tunnels. Run <code>burrow connect</code> with a token.</p>
        </div>
      ) : (
        <div className="table-wrap">
          <table className="data" aria-label="Tunnels">
            <thead>
              <tr>
                <th>Name</th>
                <th>Type</th>
                <th>Remote</th>
                <th>Local</th>
                <th className="col-bytes">In</th>
                <th className="col-bytes">Out</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {data.map((t) => (
                <tr key={t.id}>
                  <td className={t.name ? "col-name" : "col-name muted-em"}>
                    {t.name || "—"}
                  </td>
                  <td><Badge nodot>{t.type}</Badge></td>
                  <td className="col-remote">:{t.remote_port}</td>
                  <td className="col-local">{t.local_addr}</td>
                  <td className="col-bytes">{formatBytes(t.bytes_in)}</td>
                  <td className="col-bytes">{formatBytes(t.bytes_out)}</td>
                  <td>
                    {t.connected
                      ? <Badge kind="status-connected">connected</Badge>
                      : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
