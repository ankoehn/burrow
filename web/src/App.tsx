import { Routes, Route, Navigate } from "react-router-dom";
import { RequireAuth } from "@/auth/RequireAuth";
import { Layout } from "@/components/Layout";
import Login from "@/pages/Login";
import Tunnels from "@/pages/Tunnels";
import Tokens from "@/pages/Tokens";
import Account from "@/pages/Account";

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route element={<RequireAuth><Layout /></RequireAuth>}>
        <Route path="/" element={<Tunnels />} />
        <Route path="/tunnels" element={<Tunnels />} />
        <Route path="/tokens" element={<Tokens />} />
        <Route path="/account" element={<Account />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
