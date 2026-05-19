/// <reference types="node" />
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, it, expect } from "vitest";

const FORBIDDEN = /googleapis\.com|gstatic\.com|jsdelivr\.net|rsms\.me|unpkg\.com|cdnjs|https?:\/\/cdn\./i;

function walk(dir: string, acc: string[] = []): string[] {
  for (const e of readdirSync(dir)) {
    const p = join(dir, e);
    if (statSync(p).isDirectory()) walk(p, acc);
    else if (/\.(tsx?|css|html)$/.test(e)) acc.push(p);
  }
  return acc;
}

describe("no CDN / no phone-home", () => {
  it("no external font/asset CDN references under web/src or index.html", () => {
    const files = [...walk("src"), "index.html"].filter(
      (f) => !f.endsWith("no-cdn.test.ts"),
    );
    const hits = files.filter((f) => FORBIDDEN.test(readFileSync(f, "utf8")));
    expect(hits).toEqual([]);
  });
});
