import type { ReactNode } from "react";
import { cx } from "./cx";

export interface AccessModeCardProps {
  title: ReactNode;
  description: ReactNode;
  selected: boolean;
  onSelect: () => void;
  className?: string;
}

export function AccessModeCard({
  title,
  description,
  selected,
  onSelect,
  className,
}: AccessModeCardProps) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cx("access-mode-card", className)}
    >
      <p className="title">{title}</p>
      <p className="desc">{description}</p>
    </button>
  );
}
