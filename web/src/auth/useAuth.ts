import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "@/lib/api";

export interface Me { id: string; email: string; role: string; }

export function useAuth() {
  const q = useQuery({ queryKey: ["me"], queryFn: () => apiFetch<Me>("/me") });
  return { user: q.data, loading: q.isLoading, error: q.error as Error | null };
}
