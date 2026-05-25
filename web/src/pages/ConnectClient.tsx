import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Copy } from "lucide-react";
import { toast } from "sonner";
import { apiFetch, ApiError } from "@/lib/api";
import { Button, Input } from "@/components/ds";
import { Toaster } from "@/components/ui/sonner";
import type { NewToken } from "@/lib/contract";

interface ConnectInfo { server: string }

function copy(text: string) {
  void navigator.clipboard?.writeText(text);
  toast.success("Copied.");
}

export default function ConnectClient() {
  const [name, setName] = useState("");
  const [reveal, setReveal] = useState(false);
  const [error, setError] = useState("");
  // P1-2: the control-plane endpoint lives on a different port from the
  // dashboard (e.g. :7000 control vs :8080 dashboard). Ask the relay for the
  // correct host:port; fall back to window.location.host on 404 or error so
  // legacy/standalone deploys still produce a copyable command.
  const ci = useQuery({
    queryKey: ["connect-info"],
    queryFn: () => apiFetch<ConnectInfo>("/clients/connect-info"),
    retry: false,
    staleTime: 5 * 60_000,
  });
  const endpoint = ci.data?.server
    || (typeof window !== "undefined" ? window.location.host : "relay.example.com");

  const mint = useMutation({
    mutationFn: () => apiFetch<NewToken>("/tokens", { method: "POST", body: JSON.stringify({ name }) }),
    onError: (e: unknown) => setError(e instanceof ApiError ? e.message : "Failed to mint token"),
  });

  const tok = mint.data;
  const cmd = tok
    ? `burrow connect --server ${endpoint} --token ${reveal ? tok.token : "bur_••••••••"} --local 127.0.0.1:3000 --remote 9000 --name ${name}`
    : "";
  const cmdToCopy = tok
    ? `burrow connect --server ${endpoint} --token ${tok.token} --local 127.0.0.1:3000 --remote 9000 --name ${name}`
    : "";

  return (
    <div className="account-page">
      <div className="page-head"><div><h1>Connect a client</h1><p className="sub">Bring a machine online so it can expose a local service through this Burrow relay.</p></div></div>

      <section className="account-section" aria-labelledby="ob-1">
        <div className="section-head"><div className="left"><h2 id="ob-1">Name this client</h2></div></div>
        <div className="field">
          <label htmlFor="ob-name">Client name</label>
          <Input id="ob-name" aria-label="Client name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. office-box-1" />
          <p className="muted">Lowercase letters, digits, and hyphens. Once issued, the name lives with the token.</p>
        </div>
        <Button variant="primary" size="sm" disabled={!name || mint.isPending} onClick={() => { setError(""); mint.mutate(); }}>
          {mint.isPending ? "Generating…" : "Generate token"}
        </Button>
        {error && <p role="alert" className="field-error">{error}</p>}
      </section>

      {tok && (
        <>
          <section className="account-section" aria-labelledby="ob-2">
            <div className="section-head"><div className="left"><h2 id="ob-2">Credentials</h2></div></div>
            <p role="status" className="notice-inline">Store this token now. Burrow doesn't keep a copy you can retrieve later — if you lose it, mint a new one for this client.</p>
            <div className="field">
              <label>Server endpoint</label>
              <code className="mono">{endpoint}</code>
            </div>
            <div className="field">
              <label>Client token</label>
              <span className="row gap-2" style={{ alignItems: "center" }}>
                <code className="mono">{reveal ? tok.token : "bur_••••••••"}</code>
                <Button variant="ghost" size="sm" aria-label={reveal ? "Hide token" : "Reveal token"} onClick={() => setReveal((r) => !r)}>
                  {reveal ? "Hide" : "Reveal"}
                </Button>
                <button
                  type="button"
                  className="icon-btn"
                  aria-label="Copy client token"
                  onClick={() => copy(tok.token)}
                >
                  <Copy size={13} />
                </button>
              </span>
            </div>
          </section>

          <section className="account-section" aria-labelledby="ob-3">
            <div className="section-head"><div className="left"><h2 id="ob-3">Install &amp; run</h2></div></div>
            <div className="row gap-2" style={{ alignItems: "flex-start" }}>
              <pre className="cmd-block" style={{ flex: 1 }}><code>{cmd}</code></pre>
              <button
                type="button"
                className="icon-btn"
                aria-label="Copy install command"
                onClick={() => copy(cmdToCopy)}
              >
                <Copy size={13} />
              </button>
            </div>
          </section>
        </>
      )}
      <Toaster />
    </div>
  );
}
