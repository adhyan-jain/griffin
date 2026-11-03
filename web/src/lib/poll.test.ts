import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createPoller } from "@/lib/poll";

let hidden = false;
const setHidden = (value: boolean) => {
  hidden = value;
  document.dispatchEvent(new Event("visibilitychange"));
};

beforeEach(() => {
  vi.useFakeTimers();
  hidden = false;
  Object.defineProperty(document, "hidden", { configurable: true, get: () => hidden });
});
afterEach(() => {
  vi.useRealTimers();
});

const abortError = () => new DOMException("Aborted", "AbortError");

describe("createPoller", () => {
  it("runs once immediately, then once per interval (recursive timeout, no stacking)", async () => {
    const run = vi.fn().mockResolvedValue(undefined);
    const p = createPoller({ run, intervalMs: 1000, random: () => 0.5 });
    p.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(run).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(3000);
    expect(run).toHaveBeenCalledTimes(4);
    p.stop();
  });

  it("start() is idempotent: a second start does not create a second loop", async () => {
    const run = vi.fn().mockResolvedValue(undefined);
    const p = createPoller({ run, intervalMs: 1000, random: () => 0.5 });
    p.start();
    p.start();
    p.start();
    await vi.advanceTimersByTimeAsync(2000);
    expect(run).toHaveBeenCalledTimes(3); // t=0, 1s, 2s: exactly one loop
    p.stop();
  });

  it("never overlaps: the next tick is scheduled only after the slow request finishes", async () => {
    let active = 0;
    let maxActive = 0;
    const run = vi.fn(async () => {
      active++;
      maxActive = Math.max(maxActive, active);
      await new Promise((r) => setTimeout(r, 2500)); // slower than the interval
      active--;
    });
    const p = createPoller({ run, intervalMs: 1000, random: () => 0.5 });
    p.start();
    await vi.advanceTimersByTimeAsync(20_000);
    expect(maxActive).toBe(1);
    // 2.5s request + 1s wait = 3.5s per cycle, so about 6 runs in 20s, never ~20.
    expect(run.mock.calls.length).toBeLessThanOrEqual(6);
    p.stop();
  });

  it("stop() aborts the in-flight request, prevents further ticks, and can restart", async () => {
    const signals: AbortSignal[] = [];
    const run = vi.fn((signal: AbortSignal) => {
      signals.push(signal);
      return new Promise<void>((_, reject) => signal.addEventListener("abort", () => reject(abortError())));
    });
    const p = createPoller({ run, intervalMs: 1000, random: () => 0.5 });
    p.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(signals[0].aborted).toBe(false);
    p.stop();
    expect(signals[0].aborted).toBe(true);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(run).toHaveBeenCalledTimes(1);
    p.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(run).toHaveBeenCalledTimes(2);
    p.stop();
  });

  it("backs off exponentially on failure, capped, and resets after a success", async () => {
    const times: number[] = [];
    let fail = true;
    const run = vi.fn(async () => {
      times.push(Date.now());
      if (fail) throw new Error("503");
    });
    const p = createPoller({ run, intervalMs: 1000, maxBackoffMs: 5000, random: () => 0.5 });
    p.start();
    await vi.advanceTimersByTimeAsync(30_000);
    const gaps = times.slice(1).map((t, i) => t - times[i]);
    expect(gaps.slice(0, 4)).toEqual([2000, 4000, 5000, 5000]); // 2s, 4s, then capped at 5s
    fail = false;
    await vi.advanceTimersByTimeAsync(6000); // let the next retry succeed
    const before = run.mock.calls.length;
    await vi.advanceTimersByTimeAsync(3000);
    expect(run.mock.calls.length - before).toBe(3); // back to the normal 1s cadence
    p.stop();
  });

  it("applies bounded jitter to backoff delays", async () => {
    const times: number[] = [];
    const run = vi.fn(async () => {
      times.push(Date.now());
      throw new Error("down");
    });
    const p = createPoller({ run, intervalMs: 1000, maxBackoffMs: 60_000, random: () => 1 });
    p.start();
    await vi.advanceTimersByTimeAsync(10_000);
    const first = times[1] - times[0];
    expect(first).toBe(2500); // 2000ms * (0.75 + 0.5 * 1)
    p.stop();
  });

  it("does not treat an AbortError as a failure (no backoff)", async () => {
    const run = vi.fn().mockRejectedValue(abortError());
    const p = createPoller({ run, intervalMs: 1000, random: () => 0.5 });
    p.start();
    await vi.advanceTimersByTimeAsync(3000);
    expect(run).toHaveBeenCalledTimes(4); // still the plain 1s cadence
    p.stop();
  });

  it("pauses while the tab is hidden and refreshes immediately when it becomes visible", async () => {
    const run = vi.fn().mockResolvedValue(undefined);
    const p = createPoller({ run, intervalMs: 1000, random: () => 0.5 });
    p.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(run).toHaveBeenCalledTimes(1);
    setHidden(true);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(run).toHaveBeenCalledTimes(1); // nothing while hidden
    setHidden(false);
    await vi.advanceTimersByTimeAsync(0);
    expect(run).toHaveBeenCalledTimes(2); // immediate catch-up
    p.stop();
  });

  it("does not start fetching while hidden at start time", async () => {
    hidden = true;
    const run = vi.fn().mockResolvedValue(undefined);
    const p = createPoller({ run, intervalMs: 1000, random: () => 0.5 });
    p.start();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(run).not.toHaveBeenCalled();
    setHidden(false);
    await vi.advanceTimersByTimeAsync(0);
    expect(run).toHaveBeenCalledTimes(1);
    p.stop();
  });

  it("refresh() while a request is in flight queues exactly one follow-up, never a concurrent request", async () => {
    let active = 0;
    let maxActive = 0;
    const run = vi.fn(async () => {
      active++;
      maxActive = Math.max(maxActive, active);
      await new Promise((r) => setTimeout(r, 500));
      active--;
    });
    const p = createPoller({ run, intervalMs: 60_000, random: () => 0.5 });
    p.start();
    await vi.advanceTimersByTimeAsync(100); // first request in flight
    p.refresh();
    p.refresh();
    p.refresh();
    await vi.advanceTimersByTimeAsync(5000);
    expect(maxActive).toBe(1);
    expect(run).toHaveBeenCalledTimes(2); // original + one coalesced follow-up
    p.stop();
  });

  it("stop() removes its visibility listener (no ticks after teardown)", async () => {
    const run = vi.fn().mockResolvedValue(undefined);
    const p = createPoller({ run, intervalMs: 1000, random: () => 0.5 });
    p.start();
    await vi.advanceTimersByTimeAsync(0);
    p.stop();
    setHidden(true);
    setHidden(false);
    await vi.advanceTimersByTimeAsync(5000);
    expect(run).toHaveBeenCalledTimes(1);
  });
});
