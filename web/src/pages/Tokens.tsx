import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";

interface Token { id: string; name: string; last_used: string | null; created_at: string; }

export default function Tokens() {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["tokens"], queryFn: () => apiFetch<Token[]>("/tokens") });
  const [name, setName] = useState("");
  const [plaintext, setPlaintext] = useState<string | null>(null);
  const create = useMutation({
    mutationFn: () => apiFetch<{ name: string; token: string }>("/tokens", { method: "POST", body: JSON.stringify({ name }) }),
    onSuccess: (r) => { setPlaintext(r.token); setName(""); qc.invalidateQueries({ queryKey: ["tokens"] }); },
  });
  const revoke = useMutation({
    mutationFn: (id: string) => apiFetch(`/tokens/${id}`, { method: "DELETE" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["tokens"] }),
  });
  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold">Client tokens</h1>
      <form className="mb-4 flex gap-2" onSubmit={(e) => { e.preventDefault(); if (name) create.mutate(); }}>
        <Input placeholder="Token name (e.g. laptop)" value={name} onChange={(e) => setName(e.target.value)} />
        <Button type="submit" disabled={!name || create.isPending}>Create</Button>
      </form>
      <Table>
        <TableHeader><TableRow><TableHead>Name</TableHead><TableHead>Created</TableHead><TableHead>Last used</TableHead><TableHead></TableHead></TableRow></TableHeader>
        <TableBody>
          {(data ?? []).map((t) => (
            <TableRow key={t.id}>
              <TableCell>{t.name}</TableCell>
              <TableCell>{t.created_at}</TableCell>
              <TableCell>{t.last_used ?? "never"}</TableCell>
              <TableCell><Button variant="outline" size="sm" onClick={() => revoke.mutate(t.id)}>Revoke</Button></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <Dialog open={plaintext !== null} onOpenChange={(o) => !o && setPlaintext(null)}>
        <DialogContent>
          <DialogHeader><DialogTitle>Copy your token now</DialogTitle></DialogHeader>
          <p className="text-sm text-zinc-500">This is shown once. Use it with <code>burrow connect --token …</code></p>
          <pre className="overflow-x-auto rounded bg-zinc-100 p-3 text-sm dark:bg-zinc-900">{plaintext}</pre>
          <Button onClick={() => setPlaintext(null)}>Done</Button>
        </DialogContent>
      </Dialog>
    </div>
  );
}
