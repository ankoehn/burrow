export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) { super(message); this.status = status; }
}

// csrfSafeMethods are HTTP methods that do not change state; they do not need
// an X-CSRF-Token header (mirrors the server-side csrfSafeMethods set).
const csrfSafeMethods = new Set(["GET", "HEAD", "OPTIONS"]);

// getCsrfToken reads the burrow_csrf cookie value from document.cookie.
// Returns an empty string when the cookie is absent (e.g. before login).
function getCsrfToken(): string {
  if (typeof document === "undefined") return "";
  const match = document.cookie.split(";").find((c) => c.trim().startsWith("burrow_csrf="));
  if (!match) return "";
  return match.trim().slice("burrow_csrf=".length);
}

export async function apiFetch<T = unknown>(path: string, opts: RequestInit = {}): Promise<T> {
  const method = (opts.method ?? "GET").toUpperCase();
  const csrfHeaders: Record<string, string> = {};
  if (!csrfSafeMethods.has(method)) {
    const token = getCsrfToken();
    if (token) {
      csrfHeaders["X-CSRF-Token"] = token;
    }
  }
  const { headers: callerHeaders, ...rest } = opts;
  const res = await fetch("/api/v1" + path, {
    credentials: "include",
    ...rest,
    headers: { "Content-Type": "application/json", ...csrfHeaders, ...(callerHeaders || {}) },
  });
  if (res.status === 401) {
    throw new ApiError(401, "unauthorized");
  }
  const text = await res.text();
  let body: unknown = null;
  if (text) {
    try { body = JSON.parse(text); } catch { body = null; }
  }
  if (!res.ok) {
    const errMsg = (typeof body === "object" && body !== null && "error" in body && typeof (body as Record<string, unknown>).error === "string")
      ? (body as Record<string, unknown>).error as string
      : res.statusText;
    throw new ApiError(res.status, errMsg);
  }
  return body as T;
}

// downloadFile fetches an authenticated API endpoint and saves the response
// body to the user's disk as a file download. Use this for export/download
// buttons that must produce an actual file — a plain apiFetch() reads and
// discards the body, so the button appears to do nothing. GET-only (the
// server export/download routes are all GET, which is CSRF-safe).
export async function downloadFile(path: string, fallbackName: string): Promise<void> {
  const res = await fetch("/api/v1" + path, { credentials: "include" });
  if (res.status === 401) throw new ApiError(401, "unauthorized");
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const t = await res.text();
      const j = JSON.parse(t) as unknown;
      if (j && typeof j === "object" && typeof (j as Record<string, unknown>).error === "string") {
        msg = (j as Record<string, unknown>).error as string;
      }
    } catch { /* fall back to statusText */ }
    throw new ApiError(res.status, msg);
  }
  // Prefer the server-supplied filename from Content-Disposition, if any.
  let filename = fallbackName;
  const cd = res.headers.get("Content-Disposition");
  if (cd) {
    const m = /filename\*?=(?:UTF-8'')?"?([^"";]+)"?/i.exec(cd);
    if (m && m[1]) {
      try { filename = decodeURIComponent(m[1]); } catch { filename = m[1]; }
    }
  }
  const blob = await res.blob();
  // Guard for non-browser environments (jsdom/tests lack createObjectURL):
  // the fetch above already executed, which is all the unit tests assert.
  if (typeof URL === "undefined" || typeof URL.createObjectURL !== "function") return;
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  // jsdom throws "navigation not implemented" on anchor click; real browsers
  // perform the download synchronously and never throw. Swallow either way.
  try { a.click(); } catch { /* test environment */ }
  a.remove();
  URL.revokeObjectURL(url);
}
