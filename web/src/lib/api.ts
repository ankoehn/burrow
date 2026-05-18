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
  const res = await fetch("/api/v1" + path, {
    credentials: "include",
    headers: { "Content-Type": "application/json", ...csrfHeaders, ...(opts.headers || {}) },
    ...opts,
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
