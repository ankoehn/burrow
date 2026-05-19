import { useState, useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { ChevronDown, Check } from "lucide-react";

export interface SelectOption {
  value: string;
  label: ReactNode;
}

export interface SelectProps {
  options: SelectOption[];
  value?: string;
  onChange?: (value: string) => void;
  placeholder?: string;
  id?: string;
}

export function Select({ options, value, onChange, placeholder = "Select…", id }: SelectProps) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const selected = options.find((o) => o.value === value);
  return (
    <div ref={wrapRef} style={{ position: "relative" }}>
      <button
        id={id}
        type="button"
        className="select-trigger"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <span className={selected ? "" : "placeholder"}>
          {selected ? selected.label : placeholder}
        </span>
        <ChevronDown size={14} />
      </button>
      {open && (
        <div
          className="menu menu-enter"
          role="listbox"
          style={{ position: "absolute", top: "100%", left: 0, right: 0, marginTop: 4, minWidth: 0 }}
        >
          {options.map((o) => (
            <div
              key={o.value}
              role="option"
              aria-selected={o.value === value}
              className="menu-item"
              onClick={() => {
                onChange?.(o.value);
                setOpen(false);
              }}
            >
              {o.label}
              {o.value === value && <Check size={14} style={{ marginLeft: "auto" }} />}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
