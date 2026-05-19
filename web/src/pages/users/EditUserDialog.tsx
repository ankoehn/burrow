import type { UserAdmin } from "@/lib/contract";
export function EditUserDialog({ user, onClose }: { user: UserAdmin | null; selfId?: string; onClose: () => void }) {
  return user ? <div data-testid="edit-user-stub" hidden onClick={onClose} /> : null;
}
