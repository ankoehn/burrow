import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, ShieldAlert } from "lucide-react";
import { apiFetch, ApiError } from "@/lib/api";
import { formatTimestamp } from "@/lib/format";
import { useAuth } from "@/auth/useAuth";
import { Button, Input, Dialog } from "@/components/ds";
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
  const [confirmTarget, setConfirmTarget] = useState<User | null>(null);

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

  function confirmDelete() {
    if (confirmTarget) deleteUser.mutate(confirmTarget.id);
    setConfirmTarget(null);
  }

  // Graceful 403 for non-admins or session state mismatches
  if (error instanceof ApiError && error.status === 403) {
    return (
      <div className="users-page">
        <div className="page-head">
          <div><h1>Users</h1></div>
        </div>
        <div className="notice-block warn">
          <div className="icon-bubble"><ShieldAlert size={18} /></div>
          <p role="alert">Admin access required.</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="users-page">
        <div className="page-head">
          <div><h1>Users</h1></div>
        </div>
        <div className="notice-block error">
          <div className="icon-bubble"><AlertTriangle size={18} /></div>
          <p role="alert">
            Failed to load users: {error instanceof ApiError ? error.message : "Unknown error"}
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="users-page" style={{ position: "relative" }}>
      <div className="page-head">
        <div><h1>Users</h1></div>
      </div>

      <section className="account-section" aria-labelledby="sec-create-user">
        <div className="section-head">
          <div className="left">
            <h2 id="sec-create-user">Create user</h2>
          </div>
        </div>
        <form
          className="users-form"
          onSubmit={(e) => {
            e.preventDefault();
            setCreateError("");
            createUser.mutate();
          }}
        >
          <div className="field">
            <label htmlFor="user-email">Email</label>
            <Input
              id="user-email"
              type="email"
              autoComplete="off"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
            />
          </div>
          <div className="field">
            <label htmlFor="user-password">Password</label>
            <Input
              id="user-password"
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </div>
          <div className="field">
            <label htmlFor="user-role">Role</label>
            <select
              id="user-role"
              className="input"
              value={role}
              onChange={(e) => setRole(e.target.value as "user" | "admin")}
            >
              <option value="user">user</option>
              <option value="admin">admin</option>
            </select>
          </div>
          {createError && (
            <p role="alert" className="field-error">{createError}</p>
          )}
          <div className="actions">
            <Button type="submit" variant="primary" disabled={createUser.isPending}>
              {createUser.isPending ? "Creating…" : "Create user"}
            </Button>
          </div>
        </form>
      </section>

      {isLoading ? (
        <div className="table-wrap" style={{ padding: 16 }}>
          <p className="muted">Loading…</p>
        </div>
      ) : !data || data.length === 0 ? (
        <div className="state-card">
          <p>No users found.</p>
        </div>
      ) : (
        <div className="table-wrap">
          <table className="data" aria-label="Users">
            <thead>
              <tr>
                <th>Email</th>
                <th>Role</th>
                <th>Created</th>
                <th className="col-actions"></th>
              </tr>
            </thead>
            <tbody>
              {data.map((u) => (
                <tr key={u.id}>
                  <td>{u.email}</td>
                  <td>{u.role}</td>
                  <td className="col-created">{formatTimestamp(u.created_at)}</td>
                  <td className="col-actions">
                    <Button
                      variant="secondary"
                      size="sm"
                      aria-label={`Delete user ${u.email}`}
                      disabled={deleteUser.isPending && me?.id !== u.id}
                      onClick={() => setConfirmTarget(u)}
                    >
                      Delete
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Dialog
        open={confirmTarget !== null}
        onOpenChange={(o) => { if (!o) setConfirmTarget(null); }}
        title="Delete user"
        description={
          confirmTarget
            ? `Delete user ${confirmTarget.email}? This cannot be undone.`
            : ""
        }
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirmTarget(null)}>Cancel</Button>
            <Button variant="destructive-solid" onClick={confirmDelete}>Delete</Button>
          </>
        }
      >
        <div className="confirm-icon" aria-hidden="true">
          <AlertTriangle size={16} />
        </div>
      </Dialog>
      <Toaster />
    </div>
  );
}
