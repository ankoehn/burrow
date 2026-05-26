import type { ReactNode } from "react";
import { cx } from "./cx";

export type FormFieldWidth = "sm" | "md" | "lg" | "full";

export interface FormFieldProps {
  label: ReactNode;
  htmlFor: string;
  help?: ReactNode;
  error?: ReactNode;
  w?: FormFieldWidth;
  children: ReactNode;
  className?: string;
}

export function FormField({
  label,
  htmlFor,
  help,
  error,
  w = "full",
  children,
  className,
}: FormFieldProps) {
  const widthClass = `w-${w}`;
  return (
    <div className={cx("form-field", widthClass, className)}>
      <label htmlFor={htmlFor}>{label}</label>
      {children}
      {error != null ? (
        <span className="error" role="alert">{error}</span>
      ) : help != null ? (
        <span className="help">{help}</span>
      ) : null}
    </div>
  );
}
