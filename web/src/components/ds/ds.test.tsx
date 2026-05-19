import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { Button, Switch, DropdownMenu } from "./index";

describe("ds primitives", () => {
  it("Button renders variant class and fires onClick", () => {
    const fn = vi.fn();
    render(<Button variant="primary" onClick={fn}>Go</Button>);
    const b = screen.getByRole("button", { name: "Go" });
    expect(b.className).toContain("btn-primary");
    fireEvent.click(b);
    expect(fn).toHaveBeenCalled();
  });
  it("Switch toggles via keyboard/click and exposes role=switch", () => {
    const fn = vi.fn();
    render(<Switch checked={false} onChange={fn} aria-label="t" />);
    fireEvent.click(screen.getByRole("switch"));
    expect(fn).toHaveBeenCalledWith(true);
  });
  it("DropdownMenu opens and selects an item", () => {
    const fn = vi.fn();
    render(<DropdownMenu trigger={<button>⋯</button>} items={[{ label: "Del", onSelect: fn }]} />);
    fireEvent.click(screen.getByText("⋯"));
    fireEvent.click(screen.getByText("Del"));
    expect(fn).toHaveBeenCalled();
  });
});
