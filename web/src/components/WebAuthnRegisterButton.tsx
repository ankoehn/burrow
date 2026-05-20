import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { apiFetch, ApiError } from "@/lib/api";
import { Button } from "@/components/ds";
import { startRegistration, type BeginRegistrationOptions, type RegistrationResult } from "@/lib/webauthn";

export function WebAuthnRegisterButton() {
  const qc = useQueryClient();
  const register = useMutation({
    mutationFn: async () => {
      const opts = await apiFetch<BeginRegistrationOptions>(
        "/auth/webauthn/register/begin",
        { method: "POST", body: "{}" },
      );
      const result = await startRegistration(opts);
      await apiFetch<void>("/auth/webauthn/register/finish", {
        method: "POST",
        body: JSON.stringify(result satisfies RegistrationResult),
      });
    },
    onSuccess: () => {
      toast.success("Passkey added.");
      qc.invalidateQueries({ queryKey: ["webauthn", "credentials"] });
    },
    onError: (e: unknown) =>
      toast.error(e instanceof ApiError ? e.message : "Couldn't add passkey."),
  });
  return (
    <Button
      variant="primary"
      size="sm"
      disabled={register.isPending}
      onClick={() => register.mutate()}
    >
      {register.isPending ? "Adding…" : "Add a passkey"}
    </Button>
  );
}
