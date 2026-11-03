"use client";

import { useCallback, useEffect, useRef } from "react";
import { createPoller, type Poller } from "@/lib/poll";

export interface UsePollingOptions {
  intervalMs: number;
  /** false stops the loop (e.g. a task reached a terminal state). Default true. */
  enabled?: boolean;
  maxBackoffMs?: number;
  /** Called when a poll fails (not for aborts). The poller backs off automatically. */
  onError?: (error: unknown) => void;
}

const isAbort = (e: unknown) => (e as { name?: string } | null)?.name === "AbortError";

/**
 * Runs `run` on a bounded schedule: immediately, then every `intervalMs`, one request at a time,
 * paused while the tab is hidden, with exponential backoff after failures.
 *
 * The loop is (re)created ONLY when `enabled`, `intervalMs` or `maxBackoffMs` change. `run` and
 * `onError` are read through refs, so passing fresh closures or reading state inside `run` can
 * never restart the loop. (Depending on fetched data in the effect is what turned a poller into a
 * request storm on 2026-11-03.)
 */
export function usePolling(run: (signal: AbortSignal) => Promise<unknown>, opts: UsePollingOptions) {
  const { intervalMs, enabled = true, maxBackoffMs } = opts;
  const runRef = useRef(run);
  const onErrorRef = useRef(opts.onError);
  useEffect(() => {
    runRef.current = run;
    onErrorRef.current = opts.onError;
  });

  const pollerRef = useRef<Poller | null>(null);
  useEffect(() => {
    if (!enabled) return;
    const poller = createPoller({
      intervalMs,
      maxBackoffMs,
      run: async (signal) => {
        try {
          await runRef.current(signal);
        } catch (e) {
          if (!isAbort(e)) onErrorRef.current?.(e);
          throw e;
        }
      },
    });
    pollerRef.current = poller;
    poller.start();
    return () => {
      poller.stop();
      if (pollerRef.current === poller) pollerRef.current = null;
    };
  }, [enabled, intervalMs, maxBackoffMs]);

  /** Poll now; coalesced if a request is already in flight. */
  const refresh = useCallback(() => pollerRef.current?.refresh(), []);
  return { refresh };
}
