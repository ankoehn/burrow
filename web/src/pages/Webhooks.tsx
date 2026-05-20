import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy } from "lucide-react";
import { Toaster } from "@/components/ui/sonner";
import { toast } from "sonner";
import { apiFetch, ApiError } from "@/lib/api";
import { Badge, Button, Dialog, DropdownMenu, Input, SkeletonRows } from "@/components/ds";
import type { CreatedWebhook, Webhook, WebhookDelivery } from "@/lib/contract";

function copy(text: string) {
  void navigator.clipboard?.writeText(text);
}

export default function Webhooks() {
  const qc = useQueryClient();
  const list = useQuery({
    queryKey: ["webhooks"],
    queryFn: () => apiFetch<Webhook[]>("/webhooks"),
    retry: false,
  });
  const deliveries = useQuery({
    queryKey: ["webhook-deliveries"],
    queryFn: () => apiFetch<WebhookDelivery[]>("/webhooks/deliveries?limit=50"),
    retry: false,
  });

  const [addOpen, setAddOpen] = useState(false);
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [createdSecret, setCreatedSecret] = useState<{ webhook: Webhook; signing_secret: string } | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: () =>
      apiFetch<CreatedWebhook>("/webhooks", {
        method: "POST",
        body: JSON.stringify({ name, url, events: ["audit.tokens.create"] }),
      }),
    onSuccess: (res) => {
      setCreatedSecret(res);
      qc.invalidateQueries({ queryKey: ["webhooks"] });
      setAddOpen(false);
      setName("");
      setUrl("");
      setErr(null);
    },
    onError: (e: unknown) =>
      setErr(e instanceof ApiError ? e.message : "Couldn't create webhook."),
  });

  const pause = useMutation({
    mutationFn: (id: string) => apiFetch<void>(`/webhooks/${id}/pause`, { method: "POST", body: "{}" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["webhooks"] }),
  });
  const resume = useMutation({
    mutationFn: (id: string) => apiFetch<void>(`/webhooks/${id}/resume`, { method: "POST", body: "{}" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["webhooks"] }),
  });
  const remove = useMutation({
    mutationFn: (id: string) => apiFetch<void>(`/webhooks/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["webhooks"] }),
  });

  function submit() {
    setErr(null);
    try {
      const u = new URL(url);
      if (u.protocol !== "https:") {
        setErr("URL must use https://");
        return;
      }
    } catch {
      setErr("URL must use https://");
      return;
    }
    create.mutate();
  }

  function statusOf(w: Webhook): { kind: string; text: string } {
    if (w.paused) return { kind: "status-paused", text: "Paused" };
    if (w.consecutive_failures >= 3) return { kind: "status-failing", text: "Failing" };
    return { kind: "status-connected", text: "Healthy" };
  }

  return (
    <div className="webhooks-page">
      <div className="page-head">
        <div>
          <h1>Webhooks</h1>
          <p className="muted">
            Burrow signs every webhook with an HMAC-SHA256 signature in the
            {" "}<code className="mono">Burrow-Signature</code> header. Verify on receipt.
            {" "}<a href="/docs/webhooks">Docs</a>
          </p>
        </div>
        <Button variant="primary" size="sm" onClick={() => setAddOpen(true)}>Add webhook</Button>
      </div>

      {!list.data ? (
        <SkeletonRows n={3} />
      ) : (
        <div className="table-wrap">
          <table className="data" aria-label="Webhooks">
            <thead>
              <tr><th>Name</th><th>URL</th><th>Status</th><th>Failures</th><th className="col-actions"></th></tr>
            </thead>
            <tbody>
              {list.data.length === 0
                ? <tr><td colSpan={5} className="muted">No webhooks yet.</td></tr>
                : list.data.map((w) => {
                    const s = statusOf(w);
                    return (
                      <tr key={w.id}>
                        <td>{w.name}</td>
                        <td>
                          <span className="row gap-2" style={{ alignItems: "center" }}>
                            <span className="mono small truncate">{w.url}</span>
                            <button
                              type="button"
                              className="icon-btn"
                              aria-label="Copy webhook URL"
                              onClick={() => copy(w.url)}
                            >
                              <Copy size={13} />
                            </button>
                          </span>
                        </td>
                        <td><Badge kind={s.kind}>{s.text}</Badge></td>
                        <td className="mono">{w.consecutive_failures}</td>
                        <td className="col-actions">
                          <DropdownMenu
                            trigger={<button type="button" className="icon-btn" aria-label={`Actions for ${w.name}`}>⋯</button>}
                            items={[
                              w.paused
                                ? { label: "Resume", onSelect: () => resume.mutate(w.id) }
                                : { label: "Pause", onSelect: () => pause.mutate(w.id) },
                              { label: "Send test event", onSelect: () => { void apiFetch(`/webhooks/${w.id}/test`, { method: "POST", body: "{}" }); } },
                              { label: "Delete", danger: true, onSelect: () => remove.mutate(w.id) },
                            ]}
                          />
                        </td>
                      </tr>
                    );
                  })}
            </tbody>
          </table>
        </div>
      )}

      <section className="card">
        <h2>Recent deliveries</h2>
        <div className="table-wrap">
          <table className="data" aria-label="Webhook deliveries">
            <thead>
              <tr><th>When</th><th>Event</th><th>Status</th><th>Latency</th></tr>
            </thead>
            <tbody>
              {(deliveries.data ?? []).map((d) => (
                <tr key={d.id}>
                  <td className="mono small">{d.ts}</td>
                  <td className="mono">{d.event}</td>
                  <td className="mono">{d.status_code}</td>
                  <td className="mono">{d.latency_ms} ms</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <Dialog
        open={addOpen}
        onOpenChange={(o) => { setAddOpen(o); if (!o) setErr(null); }}
        title="Add webhook"
        footer={
          <>
            <Button variant="secondary" onClick={() => setAddOpen(false)}>Cancel</Button>
            <Button variant="primary" disabled={create.isPending} onClick={submit}>Create</Button>
          </>
        }
      >
        <div className="field">
          <label htmlFor="wh-name">Name</label>
          <Input id="wh-name" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="wh-url">URL</label>
          <Input id="wh-url" className="mono" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://example.com/hook" />
        </div>
        {err && <p role="alert" className="notice-inline error">{err}</p>}
      </Dialog>

      <Dialog
        open={createdSecret !== null}
        onOpenChange={(o) => { if (!o) setCreatedSecret(null); }}
        title="Signing secret"
        footer={
          <Button variant="primary" onClick={() => setCreatedSecret(null)}>
            I've saved it
          </Button>
        }
      >
        <p>Save this signing secret now — you won't see it again.</p>
        <div className="row gap-2" style={{ alignItems: "center" }}>
          <code className="mono small">{createdSecret?.signing_secret}</code>
          <button
            type="button"
            className="icon-btn"
            aria-label="Copy signing secret"
            onClick={() => { if (createdSecret) { copy(createdSecret.signing_secret); toast.success("Copied."); } }}
          >
            <Copy size={13} />
          </button>
        </div>
      </Dialog>
      <Toaster />
    </div>
  );
}
