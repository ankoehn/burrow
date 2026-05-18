import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "@/lib/api";
import { formatTimestamp } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Toaster } from "@/components/ui/sonner";
import { toast } from "sonner";

interface Token { id: string; name: string; last_used: string | null; created_at: string; }

export default function Tokens() {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["tokens"], queryFn: () => apiFetch<Token[]>("/tokens"), staleTime: 30_000 });
  const [name, setName] = useState("");
  const [plaintext, setPlaintext] = useState<string | null>(null);
  const create = useMutation({
    mutationFn: () => apiFetch<{ name: string; token: string }>("/tokens", { method: "POST", body: JSON.stringify({ name }) }),
    onSuccess: (r) => { setPlaintext(r.token); setName(""); qc.invalidateQueries({ queryKey: ["tokens"] }); },
    onError: () => toast.error("Failed to create token"),
  });
  const revoke = useMutation({
    mutationFn: (id: string) => apiFetch(`/tokens/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["tokens"] }),
    onError: () => toast.error("Failed to revoke token"),
  });
  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold">Client tokens</h1>
      <form className="mb-4 flex items-end gap-2" onSubmit={(e) => { e.preventDefault(); if (name) create.mutate(); }}>
        <div className="flex flex-col gap-1">
          <Label htmlFor="token-name">Token name</Label>
          <Input id="token-name" placeholder="e.g. laptop" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <Button type="submit" disabled={!name || create.isPending}>Create</Button>
      </form>
      <Table>
        <TableHeader><TableRow><TableHead>Name</TableHead><TableHead>Created</TableHead><TableHead>Last used</TableHead><TableHead></TableHead></TableRow></TableHeader>
        <TableBody>
          {(data ?? []).map((t) => (
            <TableRow key={t.id}>
              <TableCell>{t.name}</TableCell>
              <TableCell>{formatTimestamp(t.created_at)}</TableCell>
              <TableCell>{t.last_used ? formatTimestamp(t.last_used) : "never"}</TableCell>
              <TableCell><Button variant="outline" size="sm" aria-label={`Revoke token ${t.name}`} onClick={() => revoke.mutate(t.id)}>Revoke</Button></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <Dialog open={plaintext !== null} onOpenChange={(o) => !o && setPlaintext(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Copy your token now</DialogTitle>
            <DialogDescription>This is shown once. Use it with <code>burrow connect --token …</code></DialogDescription>
          </DialogHeader>
          <pre className="overflow-x-auto rounded bg-zinc-100 p-3 text-sm dark:bg-zinc-900">{plaintext}</pre>
          <Button onClick={() => setPlaintext(null)}>Done</Button>
        </DialogContent>
      </Dialog>
      <Toaster />
    </div>
  );
}
