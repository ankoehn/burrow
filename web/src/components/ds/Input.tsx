import { forwardRef } from "react";
import type { InputHTMLAttributes } from "react";
import { cx } from "./cx";

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  invalid?: boolean;
  state?: "focus" | "invalid" | "disabled";
  mono?: boolean;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { invalid, state, mono, className, ...rest },
  ref,
) {
  const stateClass =
    state === "focus" ? "is-focus" :
    state === "invalid" ? "is-invalid" : "";
  return (
    <input
      ref={ref}
      className={cx("input", mono && "mono", stateClass, invalid && "is-invalid", className)}
      aria-invalid={invalid || state === "invalid" || undefined}
      disabled={state === "disabled" || rest.disabled}
      {...rest}
    />
  );
});
