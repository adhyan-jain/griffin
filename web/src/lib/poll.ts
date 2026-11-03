/**
 * Framework-free polling scheduler.
 *
 * Guarantees, by construction:
 *  - one loop per Poller: start() is idempotent and the next tick is only scheduled after the
 *    current request settles (recursive setTimeout), so timers never stack and requests never overlap;
 *  - stop() clears the timer, aborts the in-flight request and removes the visibility listener;
 *  - failures back off exponentially (with jitter, hard-capped) and a success resets the cadence;
 *  - nothing runs while the tab is hidden; becoming visible triggers one immediate refresh;
 *  - refresh() while a request is in flight is coalesced into at most one follow-up request.
 */
export interface PollerOptions {
  /** One poll. Must honour the signal. A rejection (other than AbortError) counts as a failure. */
  run: (signal: AbortSignal) => Promise<unknown>;
  intervalMs: number;
  /** Upper bound for the delay between attempts after failures. Default 60s. */
  maxBackoffMs?: number;
  /** Skip polling while document.hidden. Default true. */
  pauseWhenHidden?: boolean;
  /** Injectable for deterministic tests. */
  random?: () => number;
}

export interface Poller {
  start(): void;
  stop(): void;
  /** Poll now (coalesced if a request is already in flight). */
  refresh(): void;
}

const isAbort = (e: unknown) => (e as { name?: string } | null)?.name === "AbortError";

export function createPoller(opts: PollerOptions): Poller {
  const { run, intervalMs, maxBackoffMs = 60_000, pauseWhenHidden = true, random = Math.random } = opts;

  let running = false;
  let inFlight = false;
  let queued = false;
  let failures = 0;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let controller: AbortController | null = null;

  const hiddenNow = () => pauseWhenHidden && typeof document !== "undefined" && document.hidden;
  const clearTimer = () => {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  };

  const nextDelay = () => {
    if (failures === 0) return intervalMs;
    const base = intervalMs * 2 ** failures;
    return Math.min(maxBackoffMs, Math.round(base * (0.75 + 0.5 * random())));
  };

  const schedule = (delay: number) => {
    clearTimer();
    if (!running || hiddenNow()) return;
    timer = setTimeout(() => {
      timer = null;
      void tick();
    }, delay);
  };

  const tick = async (): Promise<void> => {
    if (!running) return;
    if (inFlight) {
      queued = true;
      return;
    }
    if (hiddenNow()) return;

    inFlight = true;
    queued = false;
    const mine = new AbortController();
    controller = mine;
    try {
      await run(mine.signal);
      failures = 0;
    } catch (e) {
      if (!isAbort(e) && !mine.signal.aborted) failures += 1;
    } finally {
      inFlight = false;
      if (controller === mine) controller = null;
    }

    if (!running) return;
    if (queued) {
      queued = false;
      void tick();
      return;
    }
    schedule(nextDelay());
  };

  const onVisibility = () => {
    if (!running) return;
    if (hiddenNow()) {
      clearTimer();
      controller?.abort();
      return;
    }
    if (inFlight) {
      queued = true; // the aborted request will be followed by one fresh request
      return;
    }
    clearTimer();
    void tick();
  };

  return {
    start() {
      if (running) return;
      running = true;
      failures = 0;
      if (pauseWhenHidden && typeof document !== "undefined") document.addEventListener("visibilitychange", onVisibility);
      if (!hiddenNow()) void tick();
    },
    stop() {
      running = false;
      queued = false;
      clearTimer();
      controller?.abort();
      controller = null;
      if (typeof document !== "undefined") document.removeEventListener("visibilitychange", onVisibility);
    },
    refresh() {
      if (!running || hiddenNow()) return;
      if (inFlight) {
        queued = true;
        return;
      }
      clearTimer();
      void tick();
    },
  };
}
