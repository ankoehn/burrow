import { describe, it, expect } from "vitest";
import { formatBytes, formatTimestamp, formatTimestampWithTooltip } from "./format";

describe("formatBytes", () => {
  it("returns '0 B' for 0", () => {
    expect(formatBytes(0)).toBe("0 B");
  });
  it("returns '0 B' for NaN", () => {
    expect(formatBytes(NaN)).toBe("0 B");
  });
  it("returns '0 B' for negative", () => {
    expect(formatBytes(-1)).toBe("0 B");
  });
  it("formats bytes under 1024 as plain B", () => {
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1)).toBe("1 B");
  });
  it("formats KiB correctly", () => {
    expect(formatBytes(1024)).toBe("1.0 KiB");
    expect(formatBytes(1536)).toBe("1.5 KiB");
  });
  it("formats MiB correctly", () => {
    expect(formatBytes(1024 * 1024)).toBe("1.0 MiB");
    expect(formatBytes(1024 * 1024 * 2.5)).toBe("2.5 MiB");
  });
  it("formats GiB correctly", () => {
    expect(formatBytes(1024 * 1024 * 1024)).toBe("1.0 GiB");
  });
  it("returns '0 B' for Infinity", () => {
    expect(formatBytes(Infinity)).toBe("0 B");
  });
});

describe("formatTimestamp", () => {
  it("returns '—' for null", () => {
    expect(formatTimestamp(null)).toBe("—");
  });
  it("returns '—' for undefined", () => {
    expect(formatTimestamp(undefined)).toBe("—");
  });
  it("returns '—' for empty string", () => {
    expect(formatTimestamp("")).toBe("—");
  });
  it("returns '—' for an invalid date string", () => {
    expect(formatTimestamp("not-a-date")).toBe("—");
  });
  it("returns a non-empty string for a valid RFC3339 timestamp", () => {
    const result = formatTimestamp("2024-01-15T10:30:00Z");
    expect(result).not.toBe("—");
    expect(result.length).toBeGreaterThan(0);
  });
  it("returns a non-empty string for another valid RFC3339 timestamp", () => {
    const result = formatTimestamp("2023-06-01T00:00:00.000Z");
    expect(result).not.toBe("—");
  });
});

describe("formatTimestamp locale", () => {
  it("renders en-GB unambiguous format regardless of system locale", () => {
    const s = "2026-05-25T07:42:51.834Z";
    const out = formatTimestamp(s);
    // Guarantee no month-first US ambiguity (no "5/25").
    expect(out).not.toMatch(/^\d{1,2}\/\d{1,2}\/\d{4}/);
    expect(out).not.toBe("—");
    expect(out).toContain("2026");
  });
});

describe("formatTimestampWithTooltip", () => {
  it("returns display + iso pair", () => {
    const s = "2026-05-25T07:42:51.834Z";
    const out = formatTimestampWithTooltip(s);
    expect(out.iso).toBe("2026-05-25T07:42:51.834Z");
    expect(out.display).toBe(formatTimestamp(s));
  });
  it("null input returns em-dash + empty iso", () => {
    expect(formatTimestampWithTooltip(null)).toEqual({ display: "—", iso: "" });
  });
});
