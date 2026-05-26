import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { AccessModeCard } from "./AccessModeCard";

describe("AccessModeCard", () => {
  it("renders title + description", () => {
    render(
      <AccessModeCard
        title="Open"
        description="Burrow adds no auth."
        selected={false}
        onSelect={() => {}}
      />,
    );
    expect(screen.getByText("Open")).toBeInTheDocument();
    expect(screen.getByText("Burrow adds no auth.")).toBeInTheDocument();
  });

  it("has role=radio and aria-checked reflecting selected", () => {
    const { container, rerender } = render(
      <AccessModeCard title="x" description="y" selected={false} onSelect={() => {}} />,
    );
    let el = container.querySelector('[role="radio"]')!;
    expect(el.getAttribute("aria-checked")).toBe("false");
    rerender(<AccessModeCard title="x" description="y" selected={true} onSelect={() => {}} />);
    el = container.querySelector('[role="radio"]')!;
    expect(el.getAttribute("aria-checked")).toBe("true");
  });

  it("fires onSelect on click", () => {
    const fn = vi.fn();
    render(<AccessModeCard title="x" description="y" selected={false} onSelect={fn} />);
    fireEvent.click(screen.getByText("x"));
    expect(fn).toHaveBeenCalledTimes(1);
  });
});
