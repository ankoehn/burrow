import { describe, it, expect } from "vitest";
import { parseUserAgent } from "./userAgent";

describe("parseUserAgent", () => {
  it("Chrome on Windows", () => {
    const out = parseUserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36");
    expect(out).toEqual({ browser: "Chrome 148", os: "Windows" });
  });
  it("Safari on macOS", () => {
    const out = parseUserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15");
    expect(out).toEqual({ browser: "Safari 17", os: "macOS" });
  });
  it("Firefox on Linux", () => {
    const out = parseUserAgent("Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0");
    expect(out).toEqual({ browser: "Firefox 120", os: "Linux" });
  });
  it("unknown UA falls back to a short label", () => {
    const out = parseUserAgent("curl/8.7.1");
    expect(out.browser).toBeTruthy();
    expect(out.os).toBeTruthy();
  });
  it("empty input returns Unknown/Unknown", () => {
    expect(parseUserAgent("")).toEqual({ browser: "Unknown", os: "Unknown" });
  });
  it("Safari on iOS (iPhone)", () => {
    const out = parseUserAgent("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1");
    expect(out).toEqual({ browser: "Safari 17", os: "iOS" });
  });
  it("Chrome on iOS (CriOS)", () => {
    const out = parseUserAgent("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/124.0.0 Mobile/15E148 Safari/604.1");
    expect(out).toEqual({ browser: "Chrome 124", os: "iOS" });
  });
  it("Safari on iPad", () => {
    const out = parseUserAgent("Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1");
    expect(out).toEqual({ browser: "Safari 17", os: "iOS" });
  });
});
