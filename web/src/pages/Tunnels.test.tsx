import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClientProvider, QueryClient } from "@tanstack/react-query";
import Tunnels from "./Tunnels";

class FakeES {
  listeners: Record<string, ((e: any) => void)[]> = {};
  constructor() { (FakeES as any).last = this; }
  addEventListener(t: string, fn: (e: any) => void) { (this.listeners[t] ||= []).push(fn); }
  removeEventListener(t: string, fn: (e: any) => void) {
    this.listeners[t] = (this.listeners[t] || []).filter((f) => f !== fn);
  }
  close() {}
  emit(t: string) { (this.listeners[t] || []).forEach((fn) => fn({})); }
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
    expect(screen.getByText("11")).toBeInTheDocument();
    expect(screen.getByText("22")).toBeInTheDocument();
    const before = calls;
    (FakeES as any).last.emit("tunnels");
    await waitFor(() => expect(calls).toBeGreaterThan(before));
  });
});
