import type { ReactNode } from "react";
import { cx } from "./cx";

export interface FormFieldGroupProps {
  children: ReactNode;
  className?: string;
}

export function FormFieldGroup({ children, className }: FormFieldGroupProps) {
  return <div className={cx("form-field-group", className)}>{children}</div>;
}
