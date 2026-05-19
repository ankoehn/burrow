import type { ReactNode } from "react";
import { cx } from "./cx";

export interface BadgeProps {
  kind?: string;
  className?: string;
  children?: ReactNode;
  nodot?: boolean;
}

export function Badge({ kind, className, children, nodot }: BadgeProps) {
  return (
    <span className={cx("badge", kind && `${kind}`, className)}>
      {!nodot && <span className="dot" />}
      {children}
    </span>
  );
}
