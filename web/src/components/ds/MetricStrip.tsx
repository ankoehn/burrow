import type { ReactNode } from "react";
import { cx } from "./cx";

export interface MetricStripProps {
  ariaLabel: string;
  children: ReactNode;
  className?: string;
}

export function MetricStrip({ ariaLabel, children, className }: MetricStripProps) {
  return (
    <div role="list" aria-label={ariaLabel} className={cx("metric-strip", className)}>
      {children}
    </div>
  );
}

export interface MetricTileProps {
  label: ReactNode;
  value: ReactNode;
  sub?: ReactNode;
  tooltip?: string;
  children?: ReactNode;
  className?: string;
}

export function MetricTile({ label, value, sub, tooltip, children, className }: MetricTileProps) {
  return (
    <div role="listitem" title={tooltip} className={cx("metric-tile", className)}>
      <span className="label">{label}</span>
      <span className="value">{value}</span>
      {sub != null && <span className="sub">{sub}</span>}
      {children}
    </div>
  );
}
