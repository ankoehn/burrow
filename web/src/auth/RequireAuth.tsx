import type { ReactNode } from "react";
import { Navigate } from "react-router-dom";
import { useAuth } from "./useAuth";

export function RequireAuth({ children }: { children: ReactNode }) {
  const { user, loading, error } = useAuth();
  if (loading) return <div className="p-8 text-sm text-zinc-500">Loading…</div>;
  if (error || !user) return <Navigate to="/login" replace />;
  return <>{children}</>;
}
