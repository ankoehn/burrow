import { useEffect, useId, useMemo, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal } from "lucide-react";
import { Toaster } from "@/components/ui/sonner";
import { toast } from "sonner";
import { apiFetch, ApiError } from "@/lib/api";
import { Button, Dialog, DropdownMenu, ErrorNotice, Input, MetricStrip, MetricTile, Select, SkeletonRows, Switch } from "@/components/ds";
import type {
  AiEndpoint, ModelAliasV5, Provider, Service, ServiceAIConfig,
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


const STRATEGY_OPTIONS = [
  { value: "single", label: "Single backend" },
  { value: "failover", label: "Failover" },
  { value: "weighted", label: "Weighted" },
  { value: "header_based", label: "Header-based" },
  { value: "sticky", label: "Sticky session" },
  { value: "multi_provider", label: "Multi-provider (cross-backend)" },
];

const PROVIDER_OPTIONS: { value: Provider; label: string }[] = [
  { value: "ollama", label: "Ollama" },
  { value: "vllm", label: "vLLM" },
  { value: "openai-compat", label: "OpenAI-compat" },
  { value: "openai", label: "OpenAI" },
  { value: "anthropic", label: "Anthropic" },
  { value: "other", label: "Other" },
];

interface AliasFormState {
  alias: string;
  concrete_model: string;
  service_id: string;
  provider: Provider;
  priority: number;
}

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
    queryFn: () => apiFetch<ModelAliasV5[]>("/models/aliases"),
    retry: false,
  });
  const services = useQuery({
    queryKey: ["services"],
    queryFn: () => apiFetch<Service[]>("/services"),
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

  // Add alias dialog state
  const [aliasDialogOpen, setAliasDialogOpen] = useState(false);
  const [aliasForm, setAliasForm] = useState<AliasFormState>({
    alias: "",
    concrete_model: "",
    service_id: id,
    provider: "ollama",
    priority: 100,
  });

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

  const createAlias = useMutation({
    mutationFn: (data: AliasFormState) =>
      apiFetch<ModelAliasV5>("/models/aliases", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    onSuccess: () => {
      toast.success("Alias created.");
      qc.invalidateQueries({ queryKey: ["models", "aliases"] });
      setAliasDialogOpen(false);
      setAliasForm({ alias: "", concrete_model: "", service_id: id, provider: "ollama", priority: 100 });
    },
    onError: (e: unknown) => {
      toast.error(e instanceof ApiError ? e.message : "Couldn't create alias.");
    },
  });

  const updatePriority = useMutation({
    mutationFn: ({ alias, priority }: { alias: string; priority: number }) =>
      apiFetch<void>(`/models/aliases/${alias}`, {
        method: "PUT",
        body: JSON.stringify({ priority }),
      }),
    onSuccess: () => {
      toast.success("Priority updated.");
      qc.invalidateQueries({ queryKey: ["models", "aliases"] });
    },
    onError: (e: unknown) => {
      toast.error(e instanceof ApiError ? e.message : "Couldn't update priority.");
    },
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

  // Look up provider chip for a backend row
  function getProviderForBackend(service_id: string): string {
    const match = (aliases.data ?? []).find((a) => a.service_id === service_id);
    return match?.provider ?? "—";
  }

  // Service options for the "Add alias" dialog
  const serviceOptions = (services.data ?? []).map((s) => ({ value: s.id, label: s.name }));

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

      <MetricStrip ariaLabel="Endpoint metrics">
        <MetricTile label="Requests (24h)" value={fmtInt(metrics.data.requests_24h)} />
        <MetricTile label="Tokens (24h)" value={`${fmtInt(metrics.data.tokens_in_24h)} → ${fmtInt(metrics.data.tokens_out_24h)}`} />
        <MetricTile label="Cost (24h)" value={`$${metrics.data.cost_usd_24h.toFixed(2)}`} />
        <MetricTile label="Cache hit ratio" value={`${Math.round(metrics.data.cache_hit_ratio_24h * 100)}%`} />
      </MetricStrip>

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
          {routing.strategy === "multi_provider" && (
            <div className="field" style={{ gridColumn: "1 / -1" }}>
              <p
                data-testid="multi-provider-banner"
                className="muted"
                style={{ margin: 0, padding: "0.5rem 0.75rem", border: "1px solid var(--border, #e5e7eb)", borderRadius: 6, fontSize: "0.875rem" }}
              >
                Cross-provider failover is allowed only when <code>Idempotency-Key</code> is set and zero bytes have streamed. See routing docs.
              </p>
            </div>
          )}
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

      <section aria-labelledby={`${headingId}-backends`} className="card">
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: "0.75rem" }}>
          <h2 id={`${headingId}-backends`}>Backends</h2>
          <Button
            variant="primary"
            size="sm"
            onClick={() => {
              setAliasForm({ alias: "", concrete_model: "", service_id: id, provider: "ollama", priority: 100 });
              setAliasDialogOpen(true);
            }}
          >
            Add alias
          </Button>
        </div>
        <div className="table-wrap">
          <table className="data" aria-label="Backends">
            <thead>
              <tr>
                <th>Service</th>
                <th>Concrete model</th>
                <th>Weight</th>
                <th>Provider</th>
                <th>Priority</th>
              </tr>
            </thead>
            <tbody>
              {(routing.backends ?? []).map((b) => (
                <tr key={b.service_id}>
                  <td className="mono">{b.service_id}</td>
                  <td className="mono">{b.concrete_model}</td>
                  <td className="mono">{b.weight}</td>
                  <td>
                    <span
                      className="chip"
                      style={{
                        display: "inline-block",
                        padding: "1px 6px",
                        borderRadius: 4,
                        fontSize: "0.75rem",
                        background: "var(--surface-2, #f3f4f6)",
                        fontFamily: "monospace",
                      }}
                    >
                      {getProviderForBackend(b.service_id)}
                    </span>
                  </td>
                  <td>
                    {(() => {
                      const matched = (aliases.data ?? []).find((a) => a.service_id === b.service_id);
                      return (
                        <Input
                          type="number"
                          min={0}
                          max={999}
                          className="mono"
                          aria-label={`Priority for ${matched?.alias ?? b.service_id}`}
                          defaultValue={String(matched?.priority ?? 100)}
                          disabled={!matched}
                          onBlur={(e) => {
                            if (matched) {
                              updatePriority.mutate({ alias: matched.alias, priority: Number(e.target.value) });
                            }
                          }}
                        />
                      );
                    })()}
                  </td>
                </tr>
              ))}
              {(routing.backends ?? []).length === 0 && (
                <tr>
                  <td colSpan={5} className="muted" style={{ textAlign: "center", padding: "1rem 0" }}>
                    No backends configured. Add an alias to get started.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
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

      {/* Add alias dialog */}
      <Dialog
        open={aliasDialogOpen}
        onOpenChange={setAliasDialogOpen}
        title="Add alias"
        description="Create a new model alias binding for this endpoint."
        footer={
          <div style={{ display: "flex", gap: "0.5rem", justifyContent: "flex-end" }}>
            <Button variant="secondary" size="sm" onClick={() => setAliasDialogOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="primary"
              size="sm"
              disabled={createAlias.isPending}
              onClick={() => createAlias.mutate(aliasForm)}
            >
              {createAlias.isPending ? "Creating…" : "Create alias"}
            </Button>
          </div>
        }
      >
        <div className="form-grid" style={{ display: "flex", flexDirection: "column", gap: "0.75rem" }}>
          <div className="field">
            <label htmlFor="alias-field-alias">Alias</label>
            <Input
              id="alias-field-alias"
              value={aliasForm.alias}
              onChange={(e) => setAliasForm((f) => ({ ...f, alias: e.target.value }))}
              placeholder="e.g. fast"
            />
          </div>
          <div className="field">
            <label htmlFor="alias-field-model">Concrete model</label>
            <Input
              id="alias-field-model"
              value={aliasForm.concrete_model}
              onChange={(e) => setAliasForm((f) => ({ ...f, concrete_model: e.target.value }))}
              placeholder="e.g. llama3.1:8b"
            />
          </div>
          <div className="field">
            <label htmlFor="alias-field-service">Service</label>
            <Select
              id="alias-field-service"
              value={aliasForm.service_id}
              onChange={(v) => setAliasForm((f) => ({ ...f, service_id: v }))}
              options={serviceOptions.length > 0 ? serviceOptions : [{ value: id, label: id }]}
            />
          </div>
          <div className="field">
            <label htmlFor="alias-field-provider">Provider</label>
            <Select
              id="alias-field-provider"
              value={aliasForm.provider}
              onChange={(v) => setAliasForm((f) => ({ ...f, provider: v as Provider }))}
              options={PROVIDER_OPTIONS}
            />
          </div>
          <div className="field">
            <label htmlFor="alias-field-priority">Priority</label>
            <Input
              id="alias-field-priority"
              type="number"
              min={0}
              max={999}
              value={String(aliasForm.priority)}
              onChange={(e) => setAliasForm((f) => ({ ...f, priority: Number(e.target.value) }))}
            />
          </div>
        </div>
      </Dialog>

      <Toaster />
    </div>
  );
}
