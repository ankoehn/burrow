import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import { QueryClientProvider, QueryClient } from "@tanstack/react-query";
import Tunnels from "./Tunnels";

class FakeES {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 2;
  readyState: number = FakeES.OPEN;
  onerror: ((e: any) => void) | null = null;
  listeners: Record<string, ((e: any) => void)[]> = {};
  constructor() { (FakeES as any).last = this; }
  addEventListener(t: string, fn: (e: any) => void) { (this.listeners[t] ||= []).push(fn); }
  removeEventListener(t: string, fn: (e: any) => void) {
    this.listeners[t] = (this.listeners[t] || []).filter((f) => f !== fn);
  }
  close() { this.readyState = FakeES.CLOSED; }
  emit(t: string) { (this.listeners[t] || []).forEach((fn) => fn({})); }
  triggerError() { if (this.onerror) this.onerror({}); }
}
(globalThis as any).EventSource = FakeES as any;

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={qc}><Tunnels /></QueryClientProvider>);
  return qc;
}

describe("Tunnels", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("renders rows with byte counters and refetches on an SSE message", async () => {
    let calls = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
      calls++;
      return new Response(JSON.stringify([{ id: "t1", name: "web", type: "tcp", remote_port: 9000, local_addr: "127.0.0.1:3000", bytes_in: 11, bytes_out: 22, connected: true }]), { status: 200 }) as any;
    });
    setup();
    expect(await screen.findByText("web")).toBeInTheDocument();
    // bytes_in: 11 B, bytes_out: 22 B — rendered with formatBytes
    expect(screen.getByText("11 B")).toBeInTheDocument();
    expect(screen.getByText("22 B")).toBeInTheDocument();
    const before = calls;
    act(() => { (FakeES as any).last.emit("tunnels"); });
    await waitFor(() => expect(calls).toBeGreaterThan(before));
  });

  it("renders the data table using the design-system table.data class", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify([{ id: "t1", name: "web", type: "tcp", remote_port: 9000, local_addr: "127.0.0.1:3000", bytes_in: 11, bytes_out: 22, connected: true }]), { status: 200 }) as any
    );
    setup();
    await screen.findByText("web");
    expect(screen.getByRole("table").className).toContain("data");
  });

  it("invalidates ['me'] when SSE stream closes (onerror + CLOSED)", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response("[]", { status: 200 }) as any
    );
    const qc = setup();
    // Wait for initial render
    await screen.findByText(/No live tunnels/i);

    // Spy on qc.invalidateQueries AFTER the query client is set up
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    act(() => {
      const es: FakeES = (FakeES as any).last;
      es.readyState = FakeES.CLOSED;
      es.triggerError();
    });

    await waitFor(() => {
      expect(invalidate).toHaveBeenCalledWith(expect.objectContaining({ queryKey: ["me"] }));
    });
  });

  it("does NOT invalidate ['me'] on transient SSE error while CONNECTING", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response("[]", { status: 200 }) as any
    );
    const qc = setup();
    await screen.findByText(/No live tunnels/i);

    const invalidate = vi.spyOn(qc, "invalidateQueries");

    act(() => {
      const es: FakeES = (FakeES as any).last;
      es.readyState = FakeES.CONNECTING; // browser is retrying
      es.triggerError();
    });

    // Give it a tick to ensure no spurious invalidation
    await new Promise((r) => setTimeout(r, 10));
    expect(invalidate).not.toHaveBeenCalledWith(expect.objectContaining({ queryKey: ["me"] }));
  });
});
