import { useEffect, useId, useRef } from "react";
import type { ReactNode } from "react";

export interface DialogProps {
  open: boolean;
  onOpenChange?: (open: boolean) => void;
  title?: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
}

export function Dialog({ open, onOpenChange, title, description, children, footer }: DialogProps) {
  const ref = useRef<HTMLDivElement>(null);
  // Unique per-instance id so nested dialogs don't share aria-labelledby —
  // duplicate IDs were causing assistive tech and Playwright's
  // getByRole("dialog", { name }) to resolve to the wrong dialog.
  const titleId = useId();
  // Keep the latest onOpenChange without making it an effect dependency:
  // callers commonly pass a fresh closure every render, and re-running the
  // focus-trap effect on every render would re-fire the initial-focus timer
  // and steal focus back from whatever the user is typing into.
  const onOpenChangeRef = useRef(onOpenChange);
  onOpenChangeRef.current = onOpenChange;
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onOpenChangeRef.current?.(false);
    };
    document.addEventListener("keydown", onKey);
    // Focus trap: move focus to the first focusable element once, on open —
    // but never steal focus if it is already inside the dialog (the user may
    // already be interacting with a field).
    const t = setTimeout(() => {
      if (ref.current?.contains(document.activeElement)) return;
      const el = ref.current?.querySelector<HTMLElement>(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
      );
      el?.focus();
    }, 50);
    return () => {
      document.removeEventListener("keydown", onKey);
      clearTimeout(t);
    };
  }, [open]);

  if (!open) return null;
  return (
    <div
      style={{
        position: "fixed",
        inset: 0,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        zIndex: 30,
        padding: 16,
      }}
    >
      <div className="dialog-backdrop" onClick={() => onOpenChange?.(false)} />
      <div ref={ref} className="dialog" role="dialog" aria-modal="true" aria-labelledby={titleId}>
        <div className="dialog-header">
          <h3 id={titleId}>{title}</h3>
          {description && <p>{description}</p>}
        </div>
        <div className="dialog-body">{children}</div>
        {footer && <div className="dialog-footer">{footer}</div>}
      </div>
    </div>
  );
}
