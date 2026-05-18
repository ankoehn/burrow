import { useAuth } from "@/auth/useAuth";
import { Card } from "@/components/ui/card";

export default function Account() {
  const { user } = useAuth();
  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold">Account</h1>
      <Card className="max-w-md space-y-2 p-6 text-sm">
        <div><span className="text-zinc-500">Email:</span> {user?.email}</div>
        <div><span className="text-zinc-500">Role:</span> {user?.role}</div>
        <p className="pt-2 text-xs text-zinc-500">Password change is not available in this MVP.</p>
      </Card>
    </div>
  );
}
