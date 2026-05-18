import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { apiFetch, ApiError } from "@/lib/api";
import { useAuth } from "@/auth/useAuth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card } from "@/components/ui/card";
import { Toaster } from "@/components/ui/sonner";
import { toast } from "sonner";

export default function Account() {
  const { user } = useAuth();
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [clientError, setClientError] = useState("");
  const [serverError, setServerError] = useState("");

  const changePw = useMutation({
    mutationFn: () =>
      apiFetch("/auth/change-password", {
        method: "POST",
        body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
      }),
    onSuccess: () => {
      toast.success("Password changed successfully");
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
      setServerError("");
      setClientError("");
    },
    onError: (err: unknown) => {
      if (err instanceof ApiError) {
        if (err.status === 401) {
          // Expected form error: wrong current password. Do NOT navigate away.
          setServerError("Current password is incorrect");
        } else if (err.status === 400) {
          const msg = err.message?.toLowerCase() ?? "";
          setServerError(msg.includes("short") || msg.includes("password") ? "Password too short (minimum 8 characters)" : "Invalid request: " + err.message);
        } else {
          setServerError("An error occurred. Please try again.");
        }
      } else {
        setServerError("An error occurred. Please try again.");
      }
    },
  });

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setClientError("");
    setServerError("");
    if (newPassword !== confirmPassword) {
      setClientError("New passwords do not match");
      return;
    }
    if (newPassword.length < 8) {
      setClientError("New password must be at least 8 characters");
      return;
    }
    changePw.mutate();
  }

  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold">Account</h1>
      <Card className="max-w-md space-y-2 p-6 text-sm">
        <div><span className="text-zinc-500">Email:</span> {user?.email}</div>
        <div><span className="text-zinc-500">Role:</span> {user?.role}</div>
      </Card>
      <Card className="mt-6 max-w-md p-6">
        <h2 className="mb-4 text-base font-semibold">Change password</h2>
        <form onSubmit={handleSubmit} className="space-y-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="current-password">Current password</Label>
            <Input
              id="current-password"
              type="password"
              autoComplete="current-password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              required
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="new-password">New password</Label>
            <Input
              id="new-password"
              type="password"
              autoComplete="new-password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              required
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="confirm-password">Confirm new password</Label>
            <Input
              id="confirm-password"
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              required
            />
          </div>
          {clientError && (
            <p role="alert" className="text-sm text-red-600">{clientError}</p>
          )}
          {serverError && (
            <p role="alert" className="text-sm text-red-600">{serverError}</p>
          )}
          <Button type="submit" disabled={changePw.isPending}>
            {changePw.isPending ? "Saving…" : "Change password"}
          </Button>
        </form>
      </Card>
      <Toaster />
    </div>
  );
}
