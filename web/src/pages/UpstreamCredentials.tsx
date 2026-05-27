import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Toaster } from "@/components/ui/sonner";
import { toast } from "sonner";
import { apiFetch, ApiError } from "@/lib/api";
import { Button, Dialog, Input, Select } from "@/components/ds";
import type { UpstreamCredentialBinding } from "@/lib/contract";

interface SlotsResponse {
  slots: string[];
}

interface BindingResponse extends Partial<UpstreamCredentialBinding> {
  slot_present: boolean;
}

export function UpstreamCredentialsPanel({
  serviceId,
  serviceName,
}: {
  serviceId: string;
  serviceName?: string;
}) {
  const qc = useQueryClient();

  // Fetch all available slots
  const slotsQuery = useQuery({
    queryKey: ["upstream-credentials", "slots"],
    queryFn: () => apiFetch<SlotsResponse>("/upstream-credentials/slots"),
    retry: false,
  });

  // Fetch the current binding for this service
  const bindingQuery = useQuery({
    queryKey: ["service", serviceId, "upstream-credential"],
    queryFn: () =>
      apiFetch<BindingResponse>(`/services/${serviceId}/upstream-credential`),
    retry: false,
  });

  const slots = (slotsQuery.data?.slots ?? []).slice().sort();
  const binding = bindingQuery.data;
  const hasBinding = !!binding?.slot;

  // Form state — initialized lazily once queries resolve.
  // We track whether we've done the first-load init to avoid re-setting on
  // subsequent binding query re-fetches (which would wipe user edits).
  const [initialized, setInitialized] = useState(false);
  const [selectedSlot, setSelectedSlot] = useState<string>("");
  const [headerName, setHeaderName] = useState("Authorization");
  const [headerFormat, setHeaderFormat] = useState("Bearer {key}");
  const [unbindOpen, setUnbindOpen] = useState(false);

  // One-time initialization once both queries have settled.
  useEffect(() => {
    if (initialized) return;
    if (bindingQuery.isLoading || slotsQuery.isLoading) return;
    // Both queries have settled — do the init.
    if (binding?.slot) {
      setSelectedSlot(binding.slot);
      setHeaderName(binding.header_name ?? "Authorization");
      setHeaderFormat(binding.header_format ?? "Bearer {key}");
    } else if (slots.length > 0) {
      setSelectedSlot(slots[0]!);
    }
    setInitialized(true);
  }, [initialized, binding, slots, bindingQuery.isLoading, slotsQuery.isLoading]);

  const formatInvalid = !headerFormat.includes("{key}");

  const save = useMutation({
    mutationFn: () =>
      apiFetch<void>(`/services/${serviceId}/upstream-credential`, {
        method: "PUT",
        body: JSON.stringify({
          slot: selectedSlot,
          header_name: headerName,
          header_format: headerFormat,
        }),
      }),
    onSuccess: () => {
      toast.success("Upstream key binding saved.");
      void qc.invalidateQueries({ queryKey: ["service", serviceId, "upstream-credential"] });
    },
    onError: (e: unknown) =>
      toast.error(
        e instanceof ApiError ? e.message : "Couldn't save upstream key binding.",
      ),
  });

  const unbind = useMutation({
    mutationFn: () =>
      apiFetch<void>(`/services/${serviceId}/upstream-credential`, {
        method: "DELETE",
      }),
    onSuccess: () => {
      setUnbindOpen(false);
      toast.success("Upstream key binding removed.");
      void qc.invalidateQueries({ queryKey: ["service", serviceId, "upstream-credential"] });
    },
    onError: (e: unknown) =>
      toast.error(
        e instanceof ApiError ? e.message : "Couldn't remove upstream key binding.",
      ),
  });

  const slotOptions = slots.map((s) => ({ value: s, label: s }));

  // No-binding empty state: queries resolved, no slot field in response
  const isNoBinding =
    initialized &&
    binding !== undefined &&
    binding.slot_present === false &&
    !binding.slot;

  // Slot-missing notice: bound but env var absent
  const isSlotMissing =
    initialized &&
    binding !== undefined &&
    !!binding.slot &&
    binding.slot_present === false;

  return (
    <div className="upstream-credentials-panel">
      {isNoBinding && (
        <p className="muted">
          No upstream key bound. Visitor requests proxy through unchanged.
        </p>
      )}

      {isSlotMissing && binding?.slot && (
        <p role="alert" className="alert-danger">
          Slot <strong>{binding.slot}</strong> is bound but the environment variable is not set on this server. Requests will fail until the operator sets{" "}
          <code>BURROW_UPSTREAM_KEY_{binding.slot}</code>.
        </p>
      )}

      {/* Form only shown once both queries have resolved */}
      {initialized && (
        <div className="form-grid">
          <div className="field">
            <label htmlFor={`upstream-slot-${serviceId}`}>Slot</label>
            <Select
              id={`upstream-slot-${serviceId}`}
              value={selectedSlot}
              onChange={setSelectedSlot}
              options={slotOptions}
            />
          </div>
          <p className="muted" style={{ gridColumn: "1 / -1" }}>
            Slot values live in environment variables on this server, never in the database.
          </p>
          <div className="field">
            <label htmlFor={`upstream-header-name-${serviceId}`}>Header name</label>
            <Input
              id={`upstream-header-name-${serviceId}`}
              className="mono"
              value={headerName}
              onChange={(e) => setHeaderName(e.target.value)}
            />
          </div>
          <div className="field">
            <label htmlFor={`upstream-header-format-${serviceId}`}>Header format</label>
            <Input
              id={`upstream-header-format-${serviceId}`}
              className="mono"
              value={headerFormat}
              onChange={(e) => setHeaderFormat(e.target.value)}
            />
          </div>
        </div>
      )}

      {initialized && formatInvalid && (
        <p role="alert" className="alert-danger">
          Header format must include <code>{"{key}"}</code> where the upstream key should appear.
        </p>
      )}

      {initialized && (
        <div className="panel-actions">
          <Button
            variant="primary"
            size="sm"
            disabled={save.isPending || formatInvalid || !selectedSlot}
            onClick={() => save.mutate()}
          >
            {save.isPending ? "Saving…" : "Save"}
          </Button>

          {hasBinding && (
            <Button
              variant="destructive"
              size="sm"
              onClick={() => setUnbindOpen(true)}
            >
              Unbind
            </Button>
          )}
        </div>
      )}

      <Dialog
        open={unbindOpen}
        onOpenChange={setUnbindOpen}
        title="Unbind upstream key"
        footer={
          <>
            <Button variant="secondary" onClick={() => setUnbindOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={unbind.isPending}
              onClick={() => unbind.mutate()}
            >
              {unbind.isPending ? "Removing…" : "Confirm"}
            </Button>
          </>
        }
      >
        <p>
          Unbind the upstream key for <strong>{serviceName ?? serviceId}</strong>? Visitor
          requests will no longer have an upstream key injected.
        </p>
      </Dialog>

      <Toaster />
    </div>
  );
}
