export function CreateUserDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return open ? <div data-testid="create-user-stub" hidden onClick={onClose} /> : null;
}
