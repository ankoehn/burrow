import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { Moon, Sun, Waypoints, KeyRound, Users, UserCircle, LogOut, Boxes, ShieldCheck, ServerCog } from "lucide-react";
import { apiFetch } from "@/lib/api";
import { Button, cx } from "@/components/ds";
import { useTheme } from "@/components/theme-provider";
import { useAuth } from "@/auth/useAuth";

/* Brand mark — geometric tunnel-and-arrow glyph (currentColor, never Signal Teal). */
function BurrowMark({ size = 20 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="3" y="5.5" width="12" height="13" rx="2.5" />
      <path d="M9 12h11.5" />
      <path d="M17 9l3.5 3-3.5 3" />
    </svg>
  );
}

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
  const navItem = ({ isActive }: { isActive: boolean }) => cx("nav-item", isActive && "is-active");
  const avatarInitial = (user?.email?.[0] ?? "U").toUpperCase();
  return (
    <div className="shell">
      <a className="skip-link" href="#main">Skip to content</a>
      <nav className="sidebar" aria-label="Main">
        <div className="sidebar-brand">
          <BurrowMark />
          <span className="wordmark">Burrow</span>
        </div>

        <div className="sidebar-nav">
          <div className="nav-group">
            <div className="nav-group-title">Tunneling</div>
            <NavLink to="/clients" className={navItem}>
              <span className="nav-icon"><Boxes size={16} /></span>
              <span className="nav-label">Clients</span>
            </NavLink>
            <NavLink to="/tunnels" className={navItem}>
              <span className="nav-icon"><Waypoints size={16} /></span>
              <span className="nav-label">Tunnels</span>
            </NavLink>
            <NavLink to="/tokens" className={navItem}>
              <span className="nav-icon"><KeyRound size={16} /></span>
              <span className="nav-label">Tokens</span>
            </NavLink>
          </div>

          {user?.role === "admin" && (
            <div className="nav-group">
              <div className="nav-group-title">Access control</div>
              <NavLink to="/users" className={navItem}>
                <span className="nav-icon"><Users size={16} /></span>
                <span className="nav-label">Users</span>
              </NavLink>
              <NavLink to="/roles" className={navItem}>
                <span className="nav-icon"><ShieldCheck size={16} /></span>
                <span className="nav-label">Roles</span>
              </NavLink>
            </div>
          )}

          {user?.role === "admin" && (
            <div className="nav-group">
              <div className="nav-group-title">Administration</div>
              <NavLink to="/settings" className={navItem}>
                <span className="nav-icon"><ServerCog size={16} /></span>
                <span className="nav-label">Settings</span>
              </NavLink>
            </div>
          )}

          <div className="nav-group">
            <div className="nav-group-title">Account</div>
            <NavLink to="/account" className={navItem}>
              <span className="nav-icon"><UserCircle size={16} /></span>
              <span className="nav-label">Account</span>
            </NavLink>
          </div>
        </div>

        <div
          className="sidebar-footer"
          style={{ flexDirection: "column", alignItems: "stretch", gap: 8 }}
        >
          <div className="row gap-2" style={{ alignItems: "center" }}>
            <div className="user-chip" style={{ cursor: "default" }}>
              <span className="avatar">{avatarInitial}</span>
              <span className="user-meta">
                {user?.email && <span className="user-email">{user.email}</span>}
                {user?.role && <span className="user-role">{user.role.toUpperCase()}</span>}
              </span>
            </div>
            <button
              className="theme-toggle"
              onClick={toggleTheme}
              aria-label={theme === "dark" ? "Switch to light theme" : "Switch to dark theme"}
            >
              {theme === "dark" ? <Sun size={15} /> : <Moon size={15} />}
            </button>
          </div>
          <Button variant="secondary" size="sm" icon={<LogOut size={14} />} onClick={logout}>
            Log out
          </Button>
        </div>
      </nav>

      <main className="shell-main" id="main" tabIndex={-1}>
        <div className="shell-content"><Outlet /></div>
      </main>
    </div>
  );
}
