import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "@/lib/api";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

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
    <div>
      <h1 className="mb-4 text-xl font-semibold">Tunnels</h1>
      {isLoading ? (
        <p className="text-sm text-zinc-500">Loading…</p>
      ) : !data || data.length === 0 ? (
        <p className="text-sm text-zinc-500">No live tunnels. Run <code>burrow connect</code> with a token.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead><TableHead>Type</TableHead><TableHead>Remote</TableHead>
              <TableHead>Local</TableHead><TableHead>In</TableHead><TableHead>Out</TableHead><TableHead>Status</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.map((t) => (
              <TableRow key={t.id}>
                <TableCell>{t.name || "—"}</TableCell>
                <TableCell>{t.type}</TableCell>
                <TableCell>:{t.remote_port}</TableCell>
                <TableCell>{t.local_addr}</TableCell>
                <TableCell>{t.bytes_in}</TableCell>
                <TableCell>{t.bytes_out}</TableCell>
                <TableCell>{t.connected ? "connected" : "—"}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
