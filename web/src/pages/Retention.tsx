import { useEffect, useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiFetch, ApiError } from "@/lib/api";
import { Button, Input } from "@/components/ds";
import { Toaster } from "@/components/ui/sonner";
import { toast } from "sonner";
import type { RetentionSettings } from "@/lib/contract";

// Per-field range definitions (min, max). min of 0 means 0 is allowed (= ∞).
const FIELD_META: {
  key: keyof Omit<RetentionSettings, "audit_retention_note">;
  label: string;
  min: number;
  max: number;
  placeholder: string;
}[] = [
  { key: "audit_retention_days",                   label: "Audit log (days)",              min: 0, max: 3650, placeholder: "0" },
  { key: "usage_retention_days",                   label: "Usage events (days)",            min: 1, max: 3650, placeholder: "90" },
  { key: "redaction_retention_days",               label: "Redaction events (days)",        min: 1, max: 3650, placeholder: "30" },
  { key: "connection_logs_retention_days",         label: "Connection logs (days)",         min: 1, max: 3650, placeholder: "30" },
  { key: "connection_log_rollups_retention_days",  label: "Connection log rollups (days)",  min: 0, max: 3650, placeholder: "0" },
  { key: "webhook_deliveries_retention_days",      label: "Webhook deliveries (days)",      min: 1, max: 365,  placeholder: "30" },
  { key: "inspector_retention_count",              label: "Inspector ring-buffer size",     min: 1, max: 1000, placeholder: "100" },
];

function isOutOfRange(value: number, min: number, max: number): boolean {
  return isNaN(value) || value < min || value > max;
}

export default function Retention() {
  const qc = useQueryClient();

  const { data } = useQuery({
    queryKey: ["settings", "retention"],
    queryFn: () => apiFetch<RetentionSettings>("/settings/retention"),
    retry: false,
  });

  // Local form state: string values per field so the input stays controlled.
  type FormState = Record<string, string>;
  const [form, setForm] = useState<FormState>({});

  useEffect(() => {
    if (data) {
      const s: FormState = {};
      for (const { key } of FIELD_META) {
        s[key] = String(data[key]);
      }
      setForm(s);
    }
  }, [data]);

  const set = (k: string, v: string) => setForm((f) => ({ ...f, [k]: v }));

  // Determine which fields are out of range.
  const errors: Partial<Record<string, string>> = {};
  for (const { key, min, max, label } of FIELD_META) {
    const v = Number(form[key]);
    if (form[key] !== undefined && isOutOfRange(v, min, max)) {
      errors[key] = `${label}: must be between ${min} and ${max}`;
    }
  }
  const hasErrors = Object.keys(errors).length > 0;

  const save = useMutation({
    mutationFn: () => {
      const payload: Partial<RetentionSettings> = {};
      for (const { key } of FIELD_META) {
        (payload as Record<string, number>)[key] = Number(form[key]);
      }
      return apiFetch("/settings/retention", { method: "PUT", body: JSON.stringify(payload) });
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["settings", "retention"] });
      toast.success("Retention settings saved.");
    },
    onError: (e: unknown) =>
      toast.error(e instanceof ApiError ? e.message : "Save failed"),
  });

  return (
    <div className="account-page">
      <div className="page-head">
        <div>
          <h1>Retention &amp; compliance</h1>
          <p className="sub">Configure how long Burrow retains logs and ring-buffer entries.</p>
        </div>
      </div>

      <section className="account-section">
        <form
          className="pw-form"
          onSubmit={(e) => { e.preventDefault(); if (!hasErrors) save.mutate(); }}
        >
          <div className="form-grid">
            {FIELD_META.map(({ key, label, min, max, placeholder }) => {
              const invalid = key in errors;
              return (
                <div className="field" key={key}>
                  <label htmlFor={`ret-${key}`}>{label}</label>
                  <Input
                    id={`ret-${key}`}
                    type="number"
                    mono
                    min={min}
                    max={max}
                    placeholder={placeholder}
                    value={form[key] ?? ""}
                    onChange={(e) => set(key, e.target.value)}
                    invalid={invalid}
                    aria-invalid={invalid || undefined}
                  />
                  {invalid && (
                    <p role="alert" className="field-error">{errors[key]}</p>
                  )}
                  {/* Show the advisory note below the audit_retention_days field */}
                  {key === "audit_retention_days" && data?.audit_retention_note && (
                    <p className="muted" style={{ marginTop: 4, fontSize: 12 }}>
                      {data.audit_retention_note}
                    </p>
                  )}
                </div>
              );
            })}
          </div>

          <div className="actions" style={{ marginTop: 16 }}>
            <Button
              type="submit"
              variant="primary"
              disabled={hasErrors || save.isPending}
            >
              {save.isPending ? "Saving…" : "Save"}
            </Button>
          </div>
        </form>
      </section>

      <Toaster />
    </div>
  );
}
