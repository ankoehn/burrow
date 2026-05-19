import { forwardRef } from "react";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { cx } from "./cx";

export type ButtonVariant =
  | "primary"
  | "secondary"
  | "ghost"
  | "destructive"
  | "destructive-solid";

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: "md" | "sm";
  icon?: ReactNode;
  iconOnly?: boolean;
  loading?: boolean;
  state?: "hover" | "active" | "focus" | "disabled";
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = "secondary", size = "md", icon, iconOnly, loading, state, className, children, ...rest },
  ref,
) {
  const stateClass =
    state === "hover" ? "is-hover" :
    state === "active" ? "is-active" :
    state === "focus" ? "is-focus" : "";
  return (
    <button
      ref={ref}
      className={cx(
        "btn",
        `btn-${variant}`,
        size === "sm" && "btn-sm",
        iconOnly && "btn-icon",
        stateClass,
        className,
      )}
      disabled={state === "disabled" || rest.disabled}
      {...rest}
    >
      {loading ? <span className="spinner" /> : icon}
      {!iconOnly && children}
    </button>
  );
});
