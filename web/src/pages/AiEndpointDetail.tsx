import { useEffect, useId, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal } from "lucide-react";
import { Toaster } from "@/components/ui/sonner";
import { toast } from "sonner";
import { apiFetch, ApiError } from "@/lib/api";
import { Button, DropdownMenu, ErrorNotice, Select, SkeletonRows, Switch } from "@/components/ds";
import type {
  AiEndpoint, ModelAlias, Service, ServiceAIConfig,
} from "@/lib/contract";

interface EndpointMetrics {
  requests_24h: number;
  tokens_in_24h: number;
  tokens_out_24h: number;
  cost_usd_24h: number;
  cache_hit_ratio_24h: number;
  requests_per_minute: number[];
}

interface InspectorRow {
  id: string;
  ts: string;
  method: string;
  path: string;
  status: number;
  duration_ms: number;
  cache: "HIT" | "MISS" | "SKIP";
}

function fmtInt(n: number): string {
  return n.toLocaleString("en-US");
}

function Sparkline({ data }: { data: number[] }) {
  const max = Math.max(1, ...data);
  const step = data.length > 1 ? 240 / (data.length - 1) : 0;
  const points = data.map((v, i) => `${(i * step).toFixed(2)},${(60 - (v / max) * 56 - 2).toFixed(2)}`).join(" ");
  return (
    <svg
      viewBox="0 0 240 60"
      role="img"
      aria-label="requests per minute, last 24h"
      width="240"
      height="60"
    >
      <polyline points={points} fill="none" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  );
}

function MetricTile({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div role="group" aria-label={`${label} metric`} className="metric-tile">
      <div className="metric-label">{label}</div>
      <div className="metric-value mono">{value}</div>
      {sub && <div className="metric-sub muted mono">{sub}</div>}
    </div>
  );
}

const STRATEGY_OPTIONS = [
  { value: "single", label: "Single backend" },
  { value: "failover", label: "Failover" },
  { value: "weighted", label: "Weighted" },
  { value: "header_based", label: "Header-based" },
  { value: "sticky", label: "Sticky session" },
];

export default function AiEndpointDetail() {
  const { id = "" } = useParams<{ id: string }>();
  const nav = useNavigate();
  const qc = useQueryClient();
  const headingId = useId();

  const svc = useQuery({
    queryKey: ["service", id],
    queryFn: () => apiFetch<Service>(`/services/${id}`),
    retry: false,
    enabled: Boolean(id),
  });
  const cfg = useQuery({
    queryKey: ["service", id, "ai-config"],
    queryFn: () => apiFetch<ServiceAIConfig>(`/services/${id}/ai-config`),
    retry: false,
    enabled: Boolean(id),
  });
  const metrics = useQuery({
    queryKey: ["service", id, "metrics"],
    queryFn: () => apiFetch<EndpointMetrics>(`/ai/endpoints/${id}/metrics`),
    retry: false,
    enabled: Boolean(id),
  });
  const endpoints = useQuery({
    queryKey: ["ai", "endpoints"],
    queryFn: () => apiFetch<AiEndpoint[]>("/ai/endpoints"),
    retry: false,
  });
  const aliases = useQuery({
    queryKey: ["models", "aliases"],
    queryFn: () => apiFetch<ModelAlias[]>("/models/aliases"),
    retry: false,
  });
  const recent = useQuery({
    queryKey: ["inspector", id],
    queryFn: () => apiFetch<InspectorRow[]>(`/services/${id}/inspector/requests?limit=10`),
    retry: false,
    enabled: Boolean(id),
  });

  // Local draft of the routing tab — synced from cfg.data on first arrival.
  const [draft, setDraft] = useState<ServiceAIConfig | null>(null);
  useEffect(() => {
    if (cfg.data && !draft) setDraft(cfg.data);
  }, [cfg.data, draft]);

  const save = useMutation({
    mutationFn: (next: ServiceAIConfig) =>
      apiFetch<void>(`/services/${id}/ai-config`, {
        method: "PUT",
        body: JSON.stringify(next),
      }),
    onSuccess: () => {
      toast.success("Routing saved.");
      qc.invalidateQueries({ queryKey: ["service", id, "ai-config"] });
    },
    onError: (e: unknown) => {
      toast.error(e instanceof ApiError ? e.message : "Couldn't save routing.");
    },
  });

  const clearCache = useMutation({
    mutationFn: () =>
      apiFetch<void>(`/services/${id}/cache/entries`, { method: "DELETE" }),
    onSuccess: () => toast.success("Cache cleared."),
    onError: (e: unknown) =>
      toast.error(e instanceof ApiError ? e.message : "Couldn't clear cache."),
  });

  const disable = useMutation({
    mutationFn: () => apiFetch<void>(`/services/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      toast.success("Endpoint disabled.");
      qc.invalidateQueries({ queryKey: ["services"] });
      nav("/ai/endpoints");
    },
    onError: (e: unknown) =>
      toast.error(e instanceof ApiError ? e.message : "Couldn't disable endpoint."),
  });

  const aiRow = (endpoints.data ?? []).find((e) => e.service_id === id);
  const alias = (aliases.data ?? []).find((a) => a.service_id === id);
  const resolvedAlias =
    alias && draft?.routing.model_alias
      ? `${draft.routing.model_alias} → ${alias.concrete_model}`
      : aiRow
        ? `${aiRow.model_alias} → ${aiRow.concrete_model}`
        : null;

  const baseUrl = useMemo(() => {
    if (!svc.data) return null;
    return svc.data.type === "http" && svc.data.hostname
      ? `https://${svc.data.hostname}/v1`
      : null;
  }, [svc.data]);

  if (svc.error || cfg.error || metrics.error) {
    const e = svc.error ?? cfg.error ?? metrics.error;
    return (
      <ErrorNotice
        action={
          <Button variant="secondary" size="sm" onClick={() => { void svc.refetch(); void cfg.refetch(); void metrics.refetch(); }}>
            Retry
          </Button>
        }
      >
        Couldn't load endpoint: {e instanceof ApiError ? e.message : "Unknown error"}
      </ErrorNotice>
    );
  }

  if (!draft || !metrics.data || !svc.data) {
    return (
      <div className="ai-endpoint-detail-page">
        <div className="page-head"><div><h1>AI endpoint</h1></div></div>
        <SkeletonRows n={6} />
      </div>
    );
  }

  const routing = draft.routing;

  function setRouting(next: Partial<ServiceAIConfig["routing"]>) {
    if (!draft) return;
    setDraft({ ...draft, routing: { ...draft.routing, ...next } });
  }
  function setCircuitBreaker(next: Partial<ServiceAIConfig["routing"]["circuit_breaker"]>) {
    if (!draft) return;
    setDraft({
      ...draft,
      routing: { ...draft.routing, circuit_breaker: { ...draft.routing.circuit_breaker, ...next } },
    });
  }

  const togglePause = () => {
    if (!draft) return;
    const next: ServiceAIConfig = {
      ...draft,
      routing: { ...draft.routing, paused: !draft.routing.paused },
    };
    setDraft(next);
    save.mutate(next);
  };

  const sticky = routing.strategy === "sticky";

  return (
    <div className="ai-endpoint-detail-page">
      <div className="page-head">
        <div>
          <h1 id={headingId}>AI endpoint · {svc.data.name}</h1>
          <p className="sub">Routing, traffic, and recent traffic for this gateway endpoint.</p>
        </div>
        <div className="row gap-2">
          <label className="row gap-2" style={{ alignItems: "center" }}>
            <span>Pause endpoint</span>
            <Switch
              aria-label="Pause endpoint"
              checked={routing.paused}
              onChange={togglePause}
            />
          </label>
          <DropdownMenu
            trigger={
              <button type="button" className="icon-btn" aria-label="More actions">
                <MoreHorizontal size={14} />
              </button>
            }
            items={[
              { label: "Rotate cache", onSelect: () => clearCache.mutate() },
              { label: "Clear cache", onSelect: () => clearCache.mutate() },
              { label: "Disable", danger: true, onSelect: () => disable.mutate() },
              { label: "Export logs (NDJSON)", onSelect: () => { void apiFetch(`/services/${id}/inspector/export?format=ndjson`); } },
            ]}
          />
        </div>
      </div>

      <div className="meta-strip">
        {resolvedAlias && <span className="mono">{resolvedAlias}</span>}
        {baseUrl && <span className="mono">{baseUrl}</span>}
        {aiRow?.client_session_id && (
          <Link to={`/clients/${aiRow.client_session_id}`} className="mono">
            {aiRow.client_session_id}
          </Link>
        )}
        <span className="muted">
          last seen {aiRow?.status === "Connected" ? "just now" : "—"}
        </span>
      </div>

      <div className="metric-strip" role="list" aria-label="Endpoint metrics">
        <MetricTile label="Requests (24h)" value={fmtInt(metrics.data.requests_24h)} />
        <MetricTile label="Tokens (24h)" value={`${fmtInt(metrics.data.tokens_in_24h)} → ${fmtInt(metrics.data.tokens_out_24h)}`} />
        <MetricTile label="Cost (24h)" value={`$${metrics.data.cost_usd_24h.toFixed(2)}`} />
        <MetricTile label="Cache hit ratio" value={`${Math.round(metrics.data.cache_hit_ratio_24h * 100)}%`} />
      </div>

      <div className="sparkline-wrap">
        <Sparkline data={metrics.data.requests_per_minute} />
      </div>

      <section aria-labelledby={`${headingId}-routing`} className="card">
        <h2 id={`${headingId}-routing`}>Routing</h2>
        <div className="form-grid">
          <div className="field">
            <label htmlFor={`${headingId}-strategy`}>Routing strategy</label>
            <Select
              id={`${headingId}-strategy`}
              value={routing.strategy}
              onChange={(v) =>
                setRouting({ strategy: v as ServiceAIConfig["routing"]["strategy"] })
              }
              options={STRATEGY_OPTIONS}
            />
          </div>
          <div className="field">
            <label className="row gap-2" htmlFor={`${headingId}-sticky`}>
              <Switch
                id={`${headingId}-sticky`}
                aria-label="Sticky session"
                checked={sticky}
                onChange={(v) => setRouting({ strategy: v ? "sticky" : "single" })}
              />
              <span>Sticky session</span>
            </label>
          </div>
          <div className="field">
            <label htmlFor={`${headingId}-fpct`}>Circuit-breaker failure %</label>
            <input
              id={`${headingId}-fpct`}
              type="number"
              className="input mono"
              min={0}
              max={100}
              value={routing.circuit_breaker.failure_pct}
              onChange={(e) => setCircuitBreaker({ failure_pct: Number(e.target.value) })}
            />
          </div>
        </div>
        <div className="actions">
          <Button variant="primary" size="sm" disabled={save.isPending} onClick={() => save.mutate(draft)}>
            {save.isPending ? "Saving…" : "Save routing"}
          </Button>
        </div>
      </section>

      <section aria-labelledby={`${headingId}-recent`} className="card">
        <h2 id={`${headingId}-recent`}>Recent requests</h2>
        <div className="table-wrap">
          <table className="data" aria-label="Recent requests">
            <thead>
              <tr>
                <th>When</th>
                <th>Method</th>
                <th>Path</th>
                <th>Status</th>
                <th>Latency</th>
                <th>Cache</th>
              </tr>
            </thead>
            <tbody>
              {(recent.data ?? []).map((r) => (
                <tr
                  key={r.id}
                  role="button"
                  tabIndex={0}
                  onClick={() => nav(`/inspector/${id}/${r.id}`)}
                  onKeyDown={(e) => { if (e.key === "Enter") nav(`/inspector/${id}/${r.id}`); }}
                  style={{ cursor: "pointer" }}
                >
                  <td className="mono small">{r.ts}</td>
                  <td className="mono">{r.method}</td>
                  <td className="mono">{r.path}</td>
                  <td className="mono">{r.status}</td>
                  <td className="mono">{r.duration_ms} ms</td>
                  <td className="mono">{r.cache}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
      <Toaster />
    </div>
  );
}
