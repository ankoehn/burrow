/**
 * Format a byte count using IEC binary prefixes (KiB, MiB, GiB, …).
 * Returns "0 B" for 0, NaN, or non-finite values.
 */
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "0 B";
  if (n === 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
  const i = Math.min(Math.floor(Math.log2(n) / 10), units.length - 1);
  const value = n / Math.pow(1024, i);
  // 1 decimal place for anything above plain bytes
  return i === 0 ? `${n} B` : `${value.toFixed(1)} ${units[i]}`;
}

const TS_FORMAT = new Intl.DateTimeFormat("en-GB", {
  day: "2-digit",
  month: "short",
  year: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
  timeZone: "UTC",
  timeZoneName: "short",
});

/**
 * Format an RFC3339 timestamp into an unambiguous human-readable string
 * (en-GB locale, UTC zone). Returns "—" for null/undefined/empty/unparseable.
 *
 * Always UTC so tail-tracing across distributed components stays
 * timezone-neutral.
 */
export function formatTimestamp(s: string | null | undefined): string {
  if (!s) return "—";
  const d = new Date(s);
  if (isNaN(d.getTime())) return "—";
  return TS_FORMAT.format(d);
}

/**
 * Returns both a display string and the original ISO timestamp, for use as
 * `<time dateTime={iso} title={iso}>{display}</time>`.
 */
export function formatTimestampWithTooltip(s: string | null | undefined): { display: string; iso: string } {
  if (!s) return { display: "—", iso: "" };
  const d = new Date(s);
  if (isNaN(d.getTime())) return { display: "—", iso: "" };
  return { display: TS_FORMAT.format(d), iso: s };
}
