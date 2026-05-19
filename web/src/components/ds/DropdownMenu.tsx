import { useState, useEffect, useRef, cloneElement } from "react";
import type { ReactElement, ReactNode, MouseEvent } from "react";
import { cx } from "./cx";

export interface DropdownItem {
  label?: string;
  onSelect?: () => void;
  danger?: boolean;
  icon?: ReactNode;
  shortcut?: string;
  sep?: boolean;
}

export interface DropdownMenuProps {
  trigger: ReactElement;
  items: DropdownItem[];
  align?: "left" | "right";
}

export function DropdownMenu({ trigger, items, align = "right" }: DropdownMenuProps) {
  const [open, setOpen] = useState(false);
  const [focusIdx, setFocusIdx] = useState(0);
  const wrapRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: globalThis.MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
        triggerRef.current?.focus();
      }
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setFocusIdx((i) => Math.min(items.length - 1, i + 1));
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setFocusIdx((i) => Math.max(0, i - 1));
      }
      if (e.key === "Enter") {
        e.preventDefault();
        items[focusIdx]?.onSelect?.();
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", onDoc);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, items, focusIdx]);

  const injected: Record<string, unknown> = {
    ref: triggerRef,
    onClick: (e: MouseEvent) => {
      e.stopPropagation();
      setOpen((o) => !o);
      setFocusIdx(0);
    },
    "aria-haspopup": "menu",
    "aria-expanded": open,
  };

  return (
    <div ref={wrapRef} style={{ position: "relative", display: "inline-block" }}>
      {cloneElement(trigger, injected)}
      {open && (
        <div
          className="menu menu-enter"
          role="menu"
          style={{
            position: "absolute",
            top: "100%",
            [align === "right" ? "right" : "left"]: 0,
            marginTop: 4,
            zIndex: 20,
          }}
        >
          {items.map((it, i) =>
            it.sep ? (
              <div key={`s${i}`} className="menu-sep" />
            ) : (
              <div
                key={it.label}
                role="menuitem"
                className={cx("menu-item", it.danger && "danger", focusIdx === i && "is-focus")}
                onMouseEnter={() => setFocusIdx(i)}
                onClick={() => {
                  it.onSelect?.();
                  setOpen(false);
                }}
              >
                {it.icon && (
                  <span style={{ color: it.danger ? "inherit" : "var(--muted-foreground)" }}>
                    {it.icon}
                  </span>
                )}
                <span>{it.label}</span>
                {it.shortcut && <span className="shortcut">{it.shortcut}</span>}
              </div>
            ),
          )}
        </div>
      )}
    </div>
  );
}
