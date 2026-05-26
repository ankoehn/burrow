import type { ReactNode } from "react";
import { cx } from "./cx";

export interface PageHeaderProps {
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
  className?: string;
}

export function PageHeader({ title, subtitle, actions, className }: PageHeaderProps) {
  return (
    <div className={cx("page-header", className)}>
      <div className="left">
        <h1>{title}</h1>
        {subtitle != null && <p>{subtitle}</p>}
      </div>
      {actions != null && <div className="actions">{actions}</div>}
    </div>
  );
}
