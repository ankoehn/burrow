import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { apiFetch, ApiError } from "@/lib/api";
import { Button, Input, Select } from "@/components/ds";
import type { SettingsMap } from "@/lib/contract";
import { Toaster } from "@/components/ui/sonner";
import { toast } from "sonner";

export default function Settings() {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["settings"], queryFn: () => apiFetch<SettingsMap>("/settings"), retry: false });
  const [form, setForm] = useState<SettingsMap>({});
  const [showTest, setShowTest] = useState(false);
  const [testTo, setTestTo] = useState("");
  const [testError, setTestError] = useState("");

  useEffect(() => { if (data) setForm({ ...data }); }, [data]);
  const set = (k: string, v: string) => setForm((f) => ({ ...f, [k]: v }));
  const configured = !!form["smtp.host"];

  const save = useMutation({
    mutationFn: () => apiFetch("/settings", { method: "PUT", body: JSON.stringify(form) }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["settings"] }); toast.success("Email settings saved."); },
    onError: (e: unknown) => toast.error(e instanceof ApiError ? e.message : "Save failed"),
  });
  const test = useMutation({
    mutationFn: () => apiFetch("/settings/test-email", { method: "POST", body: JSON.stringify({ to: testTo }) }),
    onSuccess: () => { setTestError(""); toast.success(`Sent a test email to ${testTo}.`); },
    onError: (e: unknown) => setTestError(e instanceof ApiError ? e.message : "Test failed"),
  });

  return (
    <div className="account-page">
      <div className="page-head"><div><h1>Settings</h1><p className="sub">Admin-only configuration for this Burrow relay.</p></div></div>

      {/* ---- v0.5.0 nav cards ---- */}
      <section className="account-section" aria-labelledby="sec-configuration">
        <div className="section-head"><div className="left"><h2 id="sec-configuration">Configuration</h2></div></div>
        <div className="settings-nav-grid">
          <Link to="/settings/retention" className="settings-nav-card">
            <div className="settings-nav-card-title">Retention &amp; compliance</div>
            <div className="settings-nav-card-desc muted">Audit log, usage events, inspector ring buffer, and other retention knobs.</div>
          </Link>
          <Link to="/settings/database" className="settings-nav-card">
            <div className="settings-nav-card-title">Database backend</div>
            <div className="settings-nav-card-desc muted">Driver in use (SQLite default; Postgres alpha).</div>
          </Link>
          <Link to="/settings/backups" className="settings-nav-card">
            <div className="settings-nav-card-title">Backup &amp; restore</div>
            <div className="settings-nav-card-desc muted">Snapshots of the relay&apos;s SQLite database.</div>
          </Link>
          <a href="/api/v1/openapi/viewer" target="_blank" rel="noreferrer" className="settings-nav-card">
            <div className="settings-nav-card-title">OpenAPI viewer</div>
            <div className="settings-nav-card-desc muted">Browse the JSON/HTTP API docs.</div>
          </a>
          <Link to="/services" className="settings-nav-card">
            <div className="settings-nav-card-title">Custom domains</div>
            <div className="settings-nav-card-desc muted">Per-service CNAME + cert pairs (managed per service).</div>
          </Link>
        </div>
      </section>

      <section className="account-section" aria-labelledby="sec-smtp">
        <div className="section-head"><div className="left"><h2 id="sec-smtp">Email / SMTP</h2></div></div>
        {!configured && <p role="status" className="notice-inline">Email isn't set up yet. User invites are disabled until you configure and test SMTP.</p>}
        <form className="pw-form" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
          <div className="field">
            <label htmlFor="smtp-host">SMTP server</label>
            <Input id="smtp-host" value={form["smtp.host"] ?? ""} onChange={(e) => set("smtp.host", e.target.value)} placeholder="smtp.example.com" />
          </div>
          <div className="field">
            <label htmlFor="smtp-port">Port</label>
            <Input id="smtp-port" inputMode="numeric" value={form["smtp.port"] ?? ""} onChange={(e) => set("smtp.port", e.target.value)} placeholder="587" />
          </div>
          <div className="field">
            <label htmlFor="smtp-enc">Encryption</label>
            <Select id="smtp-enc"
              options={[{ value: "starttls", label: "STARTTLS" }, { value: "implicit", label: "Implicit TLS" }, { value: "none", label: "None" }]}
              value={form["smtp.tls"] ?? "starttls"} onChange={(v) => set("smtp.tls", v)} />
          </div>
          <div className="field">
            <label htmlFor="smtp-user">Username</label>
            <Input id="smtp-user" value={form["smtp.username"] ?? ""} onChange={(e) => set("smtp.username", e.target.value)} placeholder="burrow@example.com" />
          </div>
          <div className="field">
            <label htmlFor="smtp-from">From address</label>
            <Input id="smtp-from" value={form["smtp.from"] ?? ""} onChange={(e) => set("smtp.from", e.target.value)} placeholder="burrow@example.com" />
          </div>
          <p className="muted">Password is supplied via BURROW_SMTP_PASSWORD or BURROW_SMTP_PASSWORD_FILE. We never echo a stored secret.</p>
          <div className="actions">
            <Button type="submit" variant="primary" disabled={save.isPending}>Save settings</Button>
          </div>
        </form>

        <div className="section-head" style={{ marginTop: 16 }}><div className="left"><h2>Test connection</h2></div></div>
        {!showTest ? (
          <Button variant="secondary" size="sm" onClick={() => setShowTest(true)}>Send test email</Button>
        ) : (
          <div className="row gap-2" style={{ alignItems: "flex-end" }}>
            <div className="field">
              <label htmlFor="smtp-test-to">Test recipient</label>
              <Input id="smtp-test-to" value={testTo} onChange={(e) => setTestTo(e.target.value)} placeholder="you@example.com" />
            </div>
            <Button variant="secondary" size="sm" disabled={test.isPending} onClick={() => { setTestError(""); test.mutate(); }}>
              {test.isPending ? "Testing…" : "Test now"}
            </Button>
          </div>
        )}
        {testError && <p role="alert" className="field-error">{testError}</p>}
      </section>
      <Toaster />
    </div>
  );
}
