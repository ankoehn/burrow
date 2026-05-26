import { useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal } from "lucide-react";
import { apiFetch, ApiError } from "@/lib/api";
import { Badge, Button, DropdownMenu, ErrorNotice, MetricStrip, MetricTile, SkeletonRows } from "@/components/ds";
import type { AiEndpoint, CostSummary } from "@/lib/contract";

function fmtInt(n: number): string {
  return n.toLocaleString("en-US");
}

function fmtUsd(n: number): string {
  return `$${n.toFixed(2)}`;
}

function hitRatio(hits: number, requests: number): string {
  if (requests <= 0) return "0%";
  return `${Math.round((hits / requests) * 100)}%`;
}


const STATUS_BADGE: Record<AiEndpoint["status"], string> = {
  Connected: "status-connected",
  Degraded: "status-degraded",
  Offline: "status-offline",
};

export default function AiEndpoints() {
  const qc = useQueryClient();
  const nav = useNavigate();
  const endpoints = useQuery({
    queryKey: ["ai", "endpoints"],
    queryFn: () => apiFetch<AiEndpoint[]>("/ai/endpoints"),
    retry: false,
  });
  const summary = useQuery({
    queryKey: ["ai", "summary"],
    queryFn: () => apiFetch<CostSummary>("/cost/summary?window=today"),
    retry: false,
  });

  // Live updates: invalidate on the existing SSE "tunnels" channel.
  // jsdom test envs without an EventSource stub skip the subscription.
  useEffect(() => {
    if (typeof EventSource === "undefined") return;
    const es = new EventSource("/api/v1/events");
    const onTick = () => qc.invalidateQueries({ queryKey: ["ai", "endpoints"] });
    es.addEventListener("tunnels", onTick);
    return () => {
      es.removeEventListener("tunnels", onTick);
      es.close();
    };
  }, [qc]);

  const list = endpoints.data ?? [];
  const totalRequests = list.reduce((a, e) => a + e.requests_24h, 0);
  const totalCacheHits = list.reduce((a, e) => a + e.cache_hits_24h, 0);
  const tokensIn = summary.data?.tokens_in ?? 0;
  const tokensOut = summary.data?.tokens_out ?? 0;
  const totalUsd = summary.data?.total_usd ?? 0;

  // P1-9 — when the AI gateway feature isn't compiled into this relay the
  // endpoint returns 404; rendering the KPI strip with zeros next to the
  // error banner suggests an empty-but-working install. Detect that case
  // and show a single "feature unavailable" card instead.
  const featureAbsent = endpoints.error instanceof ApiError && endpoints.error.status === 404;

  if (featureAbsent) {
    return (
      <div className="ai-endpoints-page">
        <div className="page-head">
          <div>
            <h1>AI endpoints</h1>
          </div>
        </div>
        <div className="state-card">
          <p>
            The AI gateway isn&apos;t available on this relay. Ask your operator to
            enable it, or run a build that includes the AI gateway.
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="ai-endpoints-page">
      <div className="page-head">
        <div>
          <h1>AI endpoints</h1>
          <p className="sub">
            Services exposing an OpenAI-compatible API through this relay — with cache,
            cost, and traffic at a glance.
          </p>
        </div>
      </div>

      <MetricStrip ariaLabel="AI endpoint metrics">
        <MetricTile label="Requests (24h)" value={fmtInt(totalRequests)} />
        <MetricTile
          label="Tokens in/out (24h)"
          value={`${fmtInt(tokensIn)} → ${fmtInt(tokensOut)}`}
        />
        <MetricTile
          label="Cost estimate (24h)"
          value={fmtUsd(totalUsd)}
          tooltip="Estimates from the bundled pricing table — operator-overridable."
        />
        <MetricTile
          label="Cache hit ratio (24h)"
          value={hitRatio(totalCacheHits, totalRequests)}
          sub={`${fmtInt(totalCacheHits)} / ${fmtInt(totalRequests)}`}
        />
      </MetricStrip>

      {endpoints.error ? (
        <ErrorNotice
          action={
            <Button variant="secondary" size="sm" onClick={() => void endpoints.refetch()}>
              Retry
            </Button>
          }
        >
          Couldn't load AI endpoints:{" "}
          {endpoints.error instanceof ApiError ? endpoints.error.message : "Unknown error"}
        </ErrorNotice>
      ) : endpoints.isLoading ? (
        <div className="table-wrap" style={{ padding: 16 }}>
          <SkeletonRows n={3} />
        </div>
      ) : list.length === 0 ? (
        <div className="state-card">
          <p>
            No AI endpoints yet. Create a service with API-key access mode and
            OpenAI-compatible upstream.
          </p>
        </div>
      ) : (
        <div className="table-wrap">
          <table className="data" aria-label="AI endpoints">
            <thead>
              <tr>
                <th>Name</th>
                <th>Backend</th>
                <th>Keys</th>
                <th>Requests (24h)</th>
                <th>Cache hits</th>
                <th>Latency p95</th>
                <th>Status</th>
                <th className="col-actions"></th>
              </tr>
            </thead>
            <tbody>
              {list.map((e) => (
                <tr key={e.service_id}>
                  <td className="col-name">
                    <div>{e.name}</div>
                    <div className="mono muted small">
                      {`${e.model_alias} → ${e.concrete_model}`}
                    </div>
                  </td>
                  <td>
                    <Badge kind={`backend-${e.backend_type}`} nodot>
                      {e.backend_type}
                    </Badge>
                  </td>
                  <td className="mono">{fmtInt(e.api_key_count)}</td>
                  <td className="mono">{fmtInt(e.requests_24h)}</td>
                  <td>
                    <span className="mono">{fmtInt(e.cache_hits_24h)}</span>
                    <span className="muted small">
                      {" "}
                      ({hitRatio(e.cache_hits_24h, e.requests_24h)})
                    </span>
                  </td>
                  <td className="mono">{fmtInt(e.latency_p95_ms)} ms</td>
                  <td>
                    <Badge kind={STATUS_BADGE[e.status]}>{e.status}</Badge>
                  </td>
                  <td className="col-actions">
                    <DropdownMenu
                      trigger={
                        <button
                          type="button"
                          className="icon-btn"
                          aria-label={`More actions for ${e.name}`}
                        >
                          <MoreHorizontal size={14} />
                        </button>
                      }
                      items={[
                        { label: "Inspect", onSelect: () => nav(`/ai/endpoints/${e.service_id}`) },
                        { label: "Keys", onSelect: () => nav(`/services?focus=${e.service_id}&panel=api-keys`) },
                        { label: "Access settings", onSelect: () => nav(`/services?focus=${e.service_id}`) },
                        { label: "Cost", onSelect: () => nav(`/cost`) },
                        { label: "Disable", danger: true, onSelect: () => { /* wired by detail page */ } },
                      ]}
                    />
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
