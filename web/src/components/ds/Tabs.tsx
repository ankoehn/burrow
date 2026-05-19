import { useRef } from "react";
import type { KeyboardEvent, ReactNode } from "react";

export interface TabItem {
  value: string;
  label: ReactNode;
  content?: ReactNode;
}

export interface TabsProps {
  tabs: TabItem[];
  value: string;
  onChange: (value: string) => void;
}

export function Tabs({ tabs, value, onChange }: TabsProps) {
  const listRef = useRef<HTMLDivElement>(null);
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const i = tabs.findIndex((t) => t.value === value);
    if (e.key === "ArrowRight") {
      e.preventDefault();
      onChange(tabs[(i + 1) % tabs.length].value);
    }
    if (e.key === "ArrowLeft") {
      e.preventDefault();
      onChange(tabs[(i - 1 + tabs.length) % tabs.length].value);
    }
  };
  const active = tabs.find((t) => t.value === value);
  return (
    <div className="tabs">
      <div role="tablist" className="tabs-list" ref={listRef} onKeyDown={onKey}>
        {tabs.map((t) => (
          <button
            key={t.value}
            role="tab"
            aria-selected={t.value === value}
            tabIndex={t.value === value ? 0 : -1}
            className="tab"
            onClick={() => onChange(t.value)}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" className="tab-panel">
        {active?.content}
      </div>
    </div>
  );
}
