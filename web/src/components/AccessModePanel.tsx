import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiFetch, ApiError } from "@/lib/api";
import { Button } from "@/components/ds";
import { ACCESS_MODES, type AccessMode } from "@/lib/contract";
import { Toaster } from "@/components/ui/sonner";
import { toast } from "sonner";

const META: Record<AccessMode, { title: string; help: string; enabled: boolean; tag: string }> = {
  open: { title: "Open — raw passthrough", help: "Burrow adds nothing. The only mode available in v0.2.0; safe default for TCP tunnels.", enabled: true, tag: "default · v0.2.0" },
  api_key: { title: "API key — header check", help: "Burrow verifies an API key header before proxying.", enabled: false, tag: "needs HTTP tunnels · v0.3.0" },
  burrow_login: { title: "Burrow login — role-based", help: "Visitors sign in with their Burrow account at a hosted gate.", enabled: false, tag: "needs HTTP tunnels · v0.3.0" },
};

export function AccessModePanel({ serviceId, serviceName, mode, clientId }: { serviceId: string; serviceName: string; mode: AccessMode; clientId: string }) {
  const qc = useQueryClient();
  const [selected, setSelected] = useState<AccessMode>(mode);
  const dirty = selected !== mode;

  const save = useMutation({
    mutationFn: () => apiFetch(`/tunnels/${serviceId}/access-mode`, { method: "PUT", body: JSON.stringify({ access_mode: selected }) }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["client", clientId] }); toast.success(`Access settings saved. ${serviceName} · mode: ${selected}`); },
    onError: (e: unknown) => toast.error(e instanceof ApiError ? e.message : "Couldn't save access settings."),
  });

  return (
    <div className="access-panel">
      <div role="radiogroup" aria-label="Access mode" className="mode-list">
        {ACCESS_MODES.map((m) => {
          const meta = META[m];
          return (
            <div
              key={m}
              role="radio"
              aria-label={meta.title}
              aria-checked={selected === m}
              aria-disabled={!meta.enabled}
              tabIndex={meta.enabled ? 0 : -1}
              className="mode-card"
              onClick={() => { if (meta.enabled) setSelected(m); }}
              onKeyDown={(e) => { if (meta.enabled && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); setSelected(m); } }}
            >
              <strong>{meta.title}</strong>
              <p className="muted">{meta.help}</p>
              <span className="muted">{meta.tag}</span>
            </div>
          );
        })}
      </div>
      <div className="actions">
        <Button variant="primary" size="sm" disabled={(!dirty && selected !== "open") || save.isPending} onClick={() => save.mutate()}>
          {save.isPending ? "Saving…" : "Save changes"}
        </Button>
      </div>
      <Toaster />
    </div>
  );
}
