import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { envelope, fixtures } from "@/test/fixtures";

vi.mock("@/lib/supabase", () => ({
  getSupabase: () => null,
  getSupabaseAccessToken: async () => "test-token",
}));

type Api = typeof import("@/lib/api");
let mod: Api;
const fetchMock = vi.fn();

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

/** A fetch that stays pending until released, and rejects with AbortError when its signal aborts. */
function pendingFetch() {
  const resolvers: Array<() => void> = [];
  const signals: Array<AbortSignal | undefined> = [];
  fetchMock.mockImplementation((_url: string, init?: RequestInit) => {
    signals.push(init?.signal ?? undefined);
    return new Promise<Response>((resolve, reject) => {
      resolvers.push(() => resolve(json(envelope(fixtures.task))));
      init?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")));
    });
  });
  return { release: () => resolvers.forEach((r) => r()), signals };
}

beforeEach(async () => {
  vi.resetModules();
  vi.stubEnv("NEXT_PUBLIC_API_URL", "http://api.test");
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockReset();
  mod = await import("@/lib/api");
});
afterEach(() => vi.unstubAllEnvs());

describe("in-flight GET de-duplication", () => {
  it("shares one network request between concurrent identical GETs", async () => {
    const p = pendingFetch();
    const [a, b, c] = [mod.api.getTask("t1"), mod.api.getTask("t1"), mod.api.getTask("t1")];
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    p.release();
    const results = await Promise.all([a, b, c]);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(results.every((r) => r.id === fixtures.task.id)).toBe(true);
  });

  it("does not share requests for different URLs", async () => {
    fetchMock.mockImplementation(async () => json(envelope(fixtures.task)));
    await Promise.all([mod.api.getTask("t1"), mod.api.getTask("t2")]);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("does not cache: a GET after the previous one finished makes a fresh request", async () => {
    fetchMock.mockImplementation(async () => json(envelope(fixtures.task)));
    await mod.api.getTask("t1");
    await mod.api.getTask("t1");
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("never de-duplicates writes", async () => {
    fetchMock.mockImplementation(async () => json(envelope(fixtures.task)));
    await Promise.all([mod.api.cancelTask("t1"), mod.api.cancelTask("t1")]);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("a failed shared request rejects every waiter and is not cached", async () => {
    fetchMock.mockImplementationOnce(async () => json({ success: false, error: { message: "boom" } }, 503));
    const results = await Promise.allSettled([mod.api.getTask("t1"), mod.api.getTask("t1")]);
    expect(results.map((r) => r.status)).toEqual(["rejected", "rejected"]);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    fetchMock.mockImplementation(async () => json(envelope(fixtures.task)));
    await expect(mod.api.getTask("t1")).resolves.toBeTruthy();
  });
});

describe("abort signals", () => {
  it("aborting one caller rejects it with AbortError but keeps the shared request alive for the other", async () => {
    const p = pendingFetch();
    const first = new AbortController();
    const a = mod.api.getTask("t1", { signal: first.signal });
    const b = mod.api.getTask("t1");
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    first.abort();
    await expect(a).rejects.toMatchObject({ name: "AbortError" });
    expect(p.signals[0]?.aborted).toBe(false);
    p.release();
    await expect(b).resolves.toMatchObject({ id: fixtures.task.id });
  });

  it("aborts the underlying request once every caller has aborted", async () => {
    const p = pendingFetch();
    const one = new AbortController();
    const two = new AbortController();
    const a = mod.api.getTask("t1", { signal: one.signal });
    const b = mod.api.getTask("t1", { signal: two.signal });
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    one.abort();
    expect(p.signals[0]?.aborted).toBe(false);
    two.abort();
    expect(p.signals[0]?.aborted).toBe(true);
    await expect(a).rejects.toMatchObject({ name: "AbortError" });
    await expect(b).rejects.toMatchObject({ name: "AbortError" });
  });

  it("rejects immediately when the signal is already aborted", async () => {
    pendingFetch();
    const c = new AbortController();
    c.abort();
    await expect(mod.api.getTask("t1", { signal: c.signal })).rejects.toMatchObject({ name: "AbortError" });
  });
});
