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

/**
 * Format an RFC3339 timestamp string into a locale-aware human-readable string.
 * Returns "—" for null, undefined, empty, or unparseable values.
 */
export function formatTimestamp(s: string | null | undefined): string {
  if (!s) return "—";
  const d = new Date(s);
  if (isNaN(d.getTime())) return "—";
  return d.toLocaleString();
}
