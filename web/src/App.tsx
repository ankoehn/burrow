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
import AiEndpoints from "@/pages/AiEndpoints";
import AiEndpointDetail from "@/pages/AiEndpointDetail";
import PromptCache from "@/pages/PromptCache";
import Guardrails from "@/pages/Guardrails";
import RequestInspector from "@/pages/RequestInspector";
import CostBudgets from "@/pages/CostBudgets";
import AuditLog from "@/pages/AuditLog";
import Webhooks from "@/pages/Webhooks";
import ProvisioningKeys from "@/pages/ProvisioningKeys";
import AutomationTokens from "@/pages/AutomationTokens";
import BackupRestore from "@/pages/BackupRestore";
import ConnectionLogs from "@/pages/ConnectionLogs";

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route element={<RequireAuth><Layout /></RequireAuth>}>
        <Route path="/" element={<Tunnels />} />
        <Route path="/tunnels" element={<Tunnels />} />
        <Route path="/services" element={<Services />} />
        <Route path="/ai/endpoints" element={<AiEndpoints />} />
        <Route path="/ai/endpoints/:id" element={<AiEndpointDetail />} />
        <Route path="/cache" element={<PromptCache />} />
        <Route path="/guardrails" element={<Guardrails />} />
        <Route path="/inspector/:serviceId/:requestId?" element={<RequestInspector />} />
        <Route path="/cost" element={<CostBudgets />} />
        <Route path="/audit" element={<AuditLog />} />
        <Route path="/webhooks" element={<Webhooks />} />
        <Route path="/provisioning" element={<ProvisioningKeys />} />
        <Route path="/account/automation" element={<AutomationTokens />} />
        <Route path="/settings/backups" element={<BackupRestore />} />
        <Route path="/tokens" element={<Tokens />} />
        <Route path="/clients" element={<Clients />} />
        <Route path="/clients/connect" element={<ConnectClient />} />
        <Route path="/clients/:id" element={<ClientDetail />} />
        <Route path="/account" element={<Account />} />
        <Route path="/users" element={<Users />} />
        <Route path="/roles" element={<Roles />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="/connection-logs" element={<ConnectionLogs />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
