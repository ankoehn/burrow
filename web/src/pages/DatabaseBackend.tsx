import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "@/lib/api";
import { PageHeader } from "@/components/ds";
import type { DatabaseStatus } from "@/lib/contract";

export default function DatabaseBackend() {
  const { data } = useQuery({
    queryKey: ["database"],
    queryFn: () => apiFetch<DatabaseStatus>("/database"),
    retry: false,
  });

  const status = data;

  return (
    <div className="account-page">
      <PageHeader title="Database backend" subtitle="The storage engine Burrow is currently using." />

      <section className="account-section">
        {status && (
          <>
            {/* Alpha banner — amber, verbatim text */}
            {status.postgres_alpha && (
              <div
                className="notice-inline"
                style={{ borderColor: "var(--warning)", marginBottom: 16 }}
                role="status"
              >
                Postgres backend is alpha — see release notes.
              </div>
            )}

            <div className="field" style={{ marginBottom: 16 }}>
              <span className="muted" style={{ marginRight: 8 }}>Driver:</span>
              <code className="mono">{status.driver}</code>
            </div>

            {status.driver === "postgres" && status.url_redacted && (
              <div className="field" style={{ marginBottom: 16 }}>
                <span className="muted" style={{ marginRight: 8 }}>Connection URL (redacted):</span>
                <code className="mono">{status.url_redacted}</code>
              </div>
            )}
          </>
        )}

        {/* "How is this configured?" disclosure — static, always visible */}
        <details style={{ marginTop: 16 }}>
          <summary className="muted" style={{ cursor: "pointer", fontSize: 13 }}>
            How is this configured?
          </summary>
          <p className="muted" style={{ marginTop: 8, fontSize: 13 }}>
            Set <code>BURROW_DATABASE_URL</code> + <code>experimental.postgres_backend: true</code>. See docs.
          </p>
        </details>
      </section>
    </div>
  );
}
