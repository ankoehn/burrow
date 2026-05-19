import type { AccessMode } from "@/lib/contract";
export function AccessModePanel({ mode }: { serviceId: string; serviceName: string; mode: AccessMode; clientId: string }) {
  return <span data-testid="access-mode-stub">{mode}</span>;
}
