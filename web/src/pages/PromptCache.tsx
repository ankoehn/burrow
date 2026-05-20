import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Toaster } from "@/components/ui/sonner";
import { toast } from "sonner";
import { apiFetch, ApiError } from "@/lib/api";
import {
  Badge, Button, Dialog, Input, Select, SkeletonRows, Switch, Tabs,
} from "@/components/ds";
import type { CacheSettings } from "@/lib/contract";

interface CacheSettingsPayload {
  global: CacheSettings;
  per_service: Record<string, CacheSettings>;
}

const APPLIES_OPTIONS = [
  { value: "global", label: "Global (cache shared across endpoints)" },
  { value: "per_endpoint", label: "Per-endpoint" },
  { value: "per_api_key", label: "Per-API-key" },
];

function ExactPanel({
  draft, setDraft, isSaving, onSave,
}: {
  draft: CacheSettings;
  setDraft: (next: CacheSettings) => void;
  isSaving: boolean;
  onSave: () => void;
}) {
  return (
    <div className="exact-cache">
      <div className="row gap-2" style={{ alignItems: "center" }}>
        <Switch
          aria-label="Enable exact cache"
          checked={draft.enabled}
          onChange={(v) => setDraft({ ...draft, enabled: v })}
        />
        <span>Cache exact (verbatim) requests</span>
      </div>
      <div className="form-grid">
        <div className="field">
          <label htmlFor="cache-applies">Applies per</label>
          <Select
            id="cache-applies"
            value={draft.applies_per}
            onChange={(v) => setDraft({ ...draft, applies_per: v as CacheSettings["applies_per"] })}
            options={APPLIES_OPTIONS}
          />
        </div>
        <div className="field">
          <label htmlFor="cache-ttl">TTL (seconds)</label>
          <Input
            id="cache-ttl"
            type="number"
            className="mono"
            min={0}
            value={draft.ttl_seconds}
            onChange={(e) => setDraft({ ...draft, ttl_seconds: Number(e.target.value) })}
          />
        </div>
        <div className="field">
          <label htmlFor="cache-max-entries">Max entries</label>
          <Input
            id="cache-max-entries"
            type="number"
            className="mono"
            min={1}
            value={draft.max_entries}
            onChange={(e) => setDraft({ ...draft, max_entries: Number(e.target.value) })}
          />
        </div>
        <div className="field">
          <label htmlFor="cache-max-per-entry">Max per-entry (KiB)</label>
          <Input
            id="cache-max-per-entry"
            type="number"
            className="mono"
            min={1}
            value={draft.max_per_entry_kb}
            onChange={(e) => setDraft({ ...draft, max_per_entry_kb: Number(e.target.value) })}
          />
        </div>
      </div>
      <div className="actions">
        <Button variant="primary" size="sm" disabled={isSaving} onClick={onSave}>
          {isSaving ? "Saving…" : "Save cache settings"}
        </Button>
      </div>
    </div>
  );
}

function SemanticPanel() {
  return (
    <div className="semantic-cache">
      <p className="muted">
        Semantic caching is off by default. Enable only after reading the docs — vector
        similarity can return stale or near-miss answers.
      </p>
      <div className="row gap-2" style={{ alignItems: "center" }}>
        <Switch
          aria-label="Enable semantic cache"
          checked={false}
          aria-disabled
          disabled
          title="Available in v0.5"
        />
        <span>Enable semantic cache</span>
        <Badge nodot kind="badge-preview">v0.5 preview</Badge>
      </div>
    </div>
  );
}

export default function PromptCache() {
  const qc = useQueryClient();
  const settings = useQuery({
    queryKey: ["cache", "settings"],
    queryFn: () => apiFetch<CacheSettingsPayload>("/cache/settings"),
    retry: false,
  });
  const [draft, setDraft] = useState<CacheSettings | null>(null);
  useEffect(() => {
    if (settings.data && !draft) setDraft(settings.data.global);
  }, [settings.data, draft]);

  const save = useMutation({
    mutationFn: () =>
      apiFetch<void>("/cache/settings", {
        method: "PUT",
        body: JSON.stringify({
          global: draft,
          per_service: settings.data?.per_service ?? {},
        }),
      }),
    onSuccess: () => {
      toast.success("Cache settings saved.");
      qc.invalidateQueries({ queryKey: ["cache", "settings"] });
    },
    onError: (e: unknown) =>
      toast.error(e instanceof ApiError ? e.message : "Couldn't save cache settings."),
  });

  const clear = useMutation({
    mutationFn: () => apiFetch<void>("/cache/entries", { method: "DELETE" }),
    onSuccess: () => toast.success("Cache cleared."),
    onError: (e: unknown) =>
      toast.error(e instanceof ApiError ? e.message : "Couldn't clear cache."),
  });

  const [tab, setTab] = useState("exact");
  const [clearOpen, setClearOpen] = useState(false);

  if (!draft) {
    return (
      <div className="prompt-cache-page">
        <div className="page-head"><div><h1>Prompt cache</h1></div></div>
        <SkeletonRows n={4} />
      </div>
    );
  }

  return (
    <div className="prompt-cache-page">
      <div className="page-head">
        <div>
          <h1>Prompt cache</h1>
          <p>The cache lives on this relay. No data is sent anywhere.</p>
        </div>
        <Button variant="secondary" size="sm" onClick={() => setClearOpen(true)}>
          Clear cache
        </Button>
      </div>

      <Tabs
        value={tab}
        onChange={setTab}
        tabs={[
          { value: "exact", label: "Exact match", content: (
              <ExactPanel
                draft={draft}
                setDraft={setDraft}
                isSaving={save.isPending}
                onSave={() => save.mutate()}
              />
            ) },
          { value: "semantic", label: "Semantic", content: <SemanticPanel /> },
        ]}
      />

      <Dialog
        open={clearOpen}
        onOpenChange={setClearOpen}
        title="Clear cache?"
        footer={
          <>
            <Button variant="secondary" onClick={() => setClearOpen(false)}>Cancel</Button>
            <Button
              variant="destructive"
              onClick={() => { clear.mutate(); setClearOpen(false); }}
            >
              Clear
            </Button>
          </>
        }
      >
        <p>Clear all cached responses? This cannot be undone.</p>
      </Dialog>
      <Toaster />
    </div>
  );
}
