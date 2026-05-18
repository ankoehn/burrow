export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) { super(message); this.status = status; }
}

export async function apiFetch<T = unknown>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch("/api/v1" + path, {
    credentials: "include",
    headers: { "Content-Type": "application/json", ...(opts.headers || {}) },
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
