import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
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
          <Input type="email" aria-label="Email" placeholder="Email" value={email} onChange={(e) => setEmail(e.target.value)} required />
          <Input type="password" aria-label="Password" placeholder="Password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          {err && <p role="alert" className="text-sm text-red-600">{err}</p>}
          <Button type="submit" className="w-full">Sign in</Button>
        </form>
      </Card>
    </div>
  );
}
