import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card } from "@/components/ui/card";

export default function Login() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const nav = useNavigate();
  const qc = useQueryClient();
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    try {
      await apiFetch("/auth/login", { method: "POST", body: JSON.stringify({ email, password }) });
      await qc.invalidateQueries({ queryKey: ["me"] });
      nav("/", { replace: true });
    } catch {
      setErr("Invalid email or password");
    }
  }
  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <Card className="w-full max-w-sm p-6">
        <h1 className="mb-4 text-xl font-semibold">Sign in to Burrow</h1>
        <form onSubmit={submit} className="space-y-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="login-email">Email</Label>
            <Input id="login-email" type="email" placeholder="you@example.com" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="login-password">Password</Label>
            <Input id="login-password" type="password" placeholder="Password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </div>
          {err && <p role="alert" className="text-sm text-red-600">{err}</p>}
          <Button type="submit" className="w-full">Sign in</Button>
        </form>
      </Card>
    </div>
  );
}
