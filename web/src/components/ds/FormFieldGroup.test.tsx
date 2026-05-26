import { render } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { FormFieldGroup } from "./FormFieldGroup";

describe("FormFieldGroup", () => {
  it("renders a .form-field-group with children", () => {
    const { container } = render(
      <FormFieldGroup>
        <div data-testid="a">A</div>
        <div data-testid="b">B</div>
      </FormFieldGroup>,
    );
    const group = container.querySelector(".form-field-group");
    expect(group).not.toBeNull();
    expect(group?.children.length).toBe(2);
  });

  it("forwards className", () => {
    const { container } = render(<FormFieldGroup className="extra"><span /></FormFieldGroup>);
    expect(container.querySelector(".form-field-group.extra")).not.toBeNull();
  });
});
