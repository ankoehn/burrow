import { PageHeader } from "@/components/ds";

export default function OpenApiViewer() {
  return (
    <div className="openapi-viewer-page" style={{ display: "flex", flexDirection: "column", height: "100%" }}>
      <PageHeader
        title="OpenAPI"
        subtitle={<>Browse this relay&apos;s JSON/HTTP API. The full spec is also available at <code className="mono">/api/v1/openapi.yaml</code>.</>}
      />
      <iframe
        src="/api/v1/openapi/viewer/"
        title="OpenAPI viewer"
        style={{
          flex: 1,
          minHeight: 600,
          width: "100%",
          border: "1px solid var(--border)",
          borderRadius: "var(--radius)",
          background: "var(--card)",
        }}
      />
    </div>
  );
}
