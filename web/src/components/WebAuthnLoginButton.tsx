import React from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { apiFetch, ApiError } from "@/lib/api";
import { Button } from "@/components/ds";
import { startAuthentication, type AuthenticationResult, type BeginAuthenticationOptions } from "@/lib/webauthn";

export function WebAuthnLoginButton({ className, style }: { className?: string; style?: React.CSSProperties }) {
  const nav = useNavigate();
  const qc = useQueryClient();
  const login = useMutation({
    mutationFn: async () => {
      const opts = await apiFetch<BeginAuthenticationOptions>(
        "/auth/webauthn/login/begin",
        { method: "POST", body: "{}" },
      );
      const result = await startAuthentication(opts);
      await apiFetch<void>("/auth/webauthn/login/finish", {
        method: "POST",
        body: JSON.stringify(result satisfies AuthenticationResult),
      });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["me"] });
      nav("/", { replace: true });
    },
    onError: (e: unknown) =>
      toast.error(e instanceof ApiError ? e.message : "Couldn't sign in with passkey."),
  });
  return (
    <Button
      variant="secondary"
      size="sm"
      disabled={login.isPending}
      onClick={() => login.mutate()}
      className={className}
      style={style}
    >
      {login.isPending ? "Signing in…" : "Sign in with a passkey"}
    </Button>
  );
}
