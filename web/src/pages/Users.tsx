import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiFetch, ApiError } from "@/lib/api";
import { formatTimestamp } from "@/lib/format";
import { useAuth } from "@/auth/useAuth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Toaster } from "@/components/ui/sonner";
import { toast } from "sonner";

export interface User {
  id: string;
  email: string;
  role: string;
  created_at: string;
}

export default function Users() {
  const { user: me } = useAuth();
  const qc = useQueryClient();

  const { data, isLoading, error } = useQuery({
    queryKey: ["users"],
    queryFn: () => apiFetch<User[]>("/users"),
    retry: false,
  });

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<"user" | "admin">("user");
  const [createError, setCreateError] = useState("");

  const createUser = useMutation({
    mutationFn: () =>
      apiFetch<User>("/users", {
        method: "POST",
        body: JSON.stringify({ email, password, role }),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["users"] });
      toast.success("User created successfully");
      setEmail("");
      setPassword("");
      setRole("user");
      setCreateError("");
    },
    onError: (err: unknown) => {
      if (err instanceof ApiError) {
        if (err.status === 409) {
          setCreateError("Email already exists");
        } else if (err.status === 400) {
          const msg = err.message?.toLowerCase() ?? "";
          setCreateError(msg.includes("short") || msg.includes("password") ? "Password too short (minimum 8 characters)" : "Invalid request: " + err.message);
        } else {
          setCreateError("Failed to create user: " + err.message);
        }
      } else {
        setCreateError("Failed to create user");
      }
    },
  });

  const deleteUser = useMutation({
    mutationFn: (id: string) =>
      apiFetch(`/users/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["users"] });
      toast.success("User deleted");
    },
    onError: (err: unknown) => {
      if (err instanceof ApiError) {
        if (err.status === 400) {
          const msg = err.message?.toLowerCase() ?? "";
          if (msg.includes("self") || msg.includes("own") || msg.includes("yourself")) {
            toast.error("You cannot delete your own account");
          } else {
            toast.error("Cannot delete user: " + err.message);
          }
        } else if (err.status === 404) {
          // User already gone — refetch to sync list
          qc.invalidateQueries({ queryKey: ["users"] });
          toast.error("User not found");
        } else {
          toast.error("Failed to delete user");
        }
      } else {
        toast.error("Failed to delete user");
      }
    },
  });

  function handleDelete(u: User) {
    if (!window.confirm(`Delete user ${u.email}? This cannot be undone.`)) return;
    deleteUser.mutate(u.id);
  }

  // Graceful 403 for non-admins or session state mismatches
  if (error instanceof ApiError && error.status === 403) {
    return (
      <div>
        <h1 className="mb-4 text-xl font-semibold">Users</h1>
        <p role="alert" className="text-sm text-red-600">Admin access required.</p>
      </div>
    );
  }

  if (error) {
    return (
      <div>
        <h1 className="mb-4 text-xl font-semibold">Users</h1>
        <p role="alert" className="text-sm text-red-600">
          Failed to load users: {error instanceof ApiError ? error.message : "Unknown error"}
        </p>
      </div>
    );
  }

  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold">Users</h1>

      {/* Create user form */}
      <Card className="mb-6 max-w-md p-6">
        <h2 className="mb-4 text-base font-semibold">Create user</h2>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setCreateError("");
            createUser.mutate();
          }}
          className="space-y-3"
        >
          <div className="flex flex-col gap-1">
            <Label htmlFor="user-email">Email</Label>
            <Input
              id="user-email"
              type="email"
              autoComplete="off"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="user-password">Password</Label>
            <Input
              id="user-password"
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="user-role">Role</Label>
            <select
              id="user-role"
              value={role}
              onChange={(e) => setRole(e.target.value as "user" | "admin")}
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-xs transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
            >
              <option value="user">user</option>
              <option value="admin">admin</option>
            </select>
          </div>
          {createError && (
            <p role="alert" className="text-sm text-red-600">{createError}</p>
          )}
          <Button type="submit" disabled={createUser.isPending}>
            {createUser.isPending ? "Creating…" : "Create user"}
          </Button>
        </form>
      </Card>

      {/* Users list */}
      {isLoading ? (
        <p className="text-sm text-zinc-500">Loading…</p>
      ) : !data || data.length === 0 ? (
        <p className="text-sm text-zinc-500">No users found.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Email</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Created</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.map((u) => (
              <TableRow key={u.id}>
                <TableCell>{u.email}</TableCell>
                <TableCell>{u.role}</TableCell>
                <TableCell>{formatTimestamp(u.created_at)}</TableCell>
                <TableCell>
                  <Button
                    variant="outline"
                    size="sm"
                    aria-label={`Delete user ${u.email}`}
                    disabled={deleteUser.isPending && me?.id !== u.id}
                    onClick={() => handleDelete(u)}
                  >
                    Delete
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <Toaster />
    </div>
  );
}
