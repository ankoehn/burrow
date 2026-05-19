import { useEffect, useRef } from "react";
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
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onOpenChange?.(false);
    };
    document.addEventListener("keydown", onKey);
    // focus trap: focus first focusable inside dialog
    const t = setTimeout(() => {
      const el = ref.current?.querySelector<HTMLElement>(
        'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
      );
      el?.focus();
    }, 50);
    return () => {
      document.removeEventListener("keydown", onKey);
      clearTimeout(t);
    };
  }, [open, onOpenChange]);

  if (!open) return null;
  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        zIndex: 30,
        borderRadius: "inherit",
      }}
    >
      <div className="dialog-backdrop" onClick={() => onOpenChange?.(false)} />
      <div ref={ref} className="dialog" role="dialog" aria-modal="true" aria-labelledby="dialog-title">
        <div className="dialog-header">
          <h3 id="dialog-title">{title}</h3>
          {description && <p>{description}</p>}
        </div>
        <div className="dialog-body">{children}</div>
        {footer && <div className="dialog-footer">{footer}</div>}
      </div>
    </div>
  );
}
