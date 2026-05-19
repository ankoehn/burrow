import { Routes, Route, Navigate } from "react-router-dom";
import { RequireAuth } from "@/auth/RequireAuth";
import { Layout } from "@/components/Layout";
import Login from "@/pages/Login";
import Tunnels from "@/pages/Tunnels";
import Services from "@/pages/Services";
import Tokens from "@/pages/Tokens";
import Account from "@/pages/Account";
import Users from "@/pages/Users";
import Roles from "@/pages/Roles";
import Settings from "@/pages/Settings";
import Clients from "@/pages/Clients";
import ClientDetail from "@/pages/ClientDetail";
import ConnectClient from "@/pages/ConnectClient";

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route element={<RequireAuth><Layout /></RequireAuth>}>
        <Route path="/" element={<Tunnels />} />
        <Route path="/tunnels" element={<Tunnels />} />
        <Route path="/services" element={<Services />} />
        <Route path="/tokens" element={<Tokens />} />
        <Route path="/clients" element={<Clients />} />
        <Route path="/clients/connect" element={<ConnectClient />} />
        <Route path="/clients/:id" element={<ClientDetail />} />
        <Route path="/account" element={<Account />} />
        <Route path="/users" element={<Users />} />
        <Route path="/roles" element={<Roles />} />
        <Route path="/settings" element={<Settings />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
