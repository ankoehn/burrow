import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { Moon, Sun } from "lucide-react";
import { apiFetch } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { useTheme } from "@/components/theme-provider";
import { useAuth } from "@/auth/useAuth";

export function Layout() {
  const nav = useNavigate();
  const qc = useQueryClient();
  const { theme, toggleTheme } = useTheme();
  const { user } = useAuth();
  async function logout() {
    try { await apiFetch("/auth/logout", { method: "POST" }); } catch { /* ignore */ }
    qc.clear();
    nav("/login", { replace: true });
  }
  const link = "block rounded px-3 py-2 text-sm hover:bg-zinc-200 dark:hover:bg-zinc-800";
  const active = "bg-zinc-200 font-medium dark:bg-zinc-800";
  return (
    <div className="flex min-h-screen">
      <aside className="w-56 border-r border-zinc-200 p-4 dark:border-zinc-800">
        <div className="mb-6 px-3 text-lg font-semibold">Burrow</div>
        <nav aria-label="Main" className="space-y-1">
          <NavLink to="/tunnels" className={({ isActive }) => `${link} ${isActive ? active : ""}`}>Tunnels</NavLink>
          <NavLink to="/tokens" className={({ isActive }) => `${link} ${isActive ? active : ""}`}>Tokens</NavLink>
          <NavLink to="/account" className={({ isActive }) => `${link} ${isActive ? active : ""}`}>Account</NavLink>
          {user?.role === "admin" && (
            <NavLink to="/users" className={({ isActive }) => `${link} ${isActive ? active : ""}`}>Users</NavLink>
          )}
        </nav>
        <div className="mt-6 px-3 flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={logout}>Log out</Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={toggleTheme}
            aria-label={theme === "dark" ? "Switch to light theme" : "Switch to dark theme"}
          >
            {theme === "dark" ? <Sun /> : <Moon />}
          </Button>
        </div>
      </aside>
      <main className="flex-1 p-8"><Outlet /></main>
    </div>
  );
}
