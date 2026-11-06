"use client";

import { useEffect, useSyncExternalStore } from "react";

/**
 * Visual-effects settings shared by the deep field, the hero core and the glass cards.
 * `full` = everything, `lite` = fewer particles, lower resolution, 30 fps, `off` = no WebGL at all.
 * The default adapts to the device (phones, low memory, few cores, data saver); the choice is
 * remembered per browser and never leaves it.
 */
export type FxMode = "full" | "lite" | "off";
export const FX_KEY = "griffin_fx";

const modeListeners = new Set<() => void>();
let mode: FxMode | null = null;

function autoMode(): FxMode {
  try {
    const nav = navigator as Navigator & { deviceMemory?: number; connection?: { saveData?: boolean } };
    if (nav.connection?.saveData) return "off";
    if (window.matchMedia("(max-width: 767px)").matches || (nav.hardwareConcurrency ?? 8) <= 4 || (nav.deviceMemory ?? 8) <= 4) return "lite";
  } catch {
    /* fall through to full */
  }
  return "full";
}

function readMode(): FxMode {
  try {
    const v = localStorage.getItem(FX_KEY);
    if (v === "full" || v === "lite" || v === "off") return v;
  } catch {
    /* storage can be blocked */
  }
  return autoMode();
}

export function setFxMode(next: FxMode) {
  mode = next;
  try {
    localStorage.setItem(FX_KEY, next);
  } catch {
    /* the choice just will not persist */
  }
  modeListeners.forEach((l) => l());
}

const subscribeMode = (l: () => void) => {
  modeListeners.add(l);
  return () => modeListeners.delete(l);
};

/** `off` on the server so nothing WebGL renders until the client has decided. */
export function useFxMode(): [FxMode, (m: FxMode) => void] {
  const m = useSyncExternalStore(subscribeMode, () => (mode ??= readMode()), () => "off" as FxMode);
  return [m, setFxMode];
}

/** Live value of the OS reduced-motion preference (a one-time read would miss mid-session changes). */
export function useReducedMotion(): boolean {
  return useSyncExternalStore(
    (cb) => {
      const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
      mq.addEventListener("change", cb);
      return () => mq.removeEventListener("change", cb);
    },
    () => window.matchMedia("(prefers-reduced-motion: reduce)").matches,
    () => false,
  );
}

// How busy the workforce is, 0..1. Read per frame by the shaders, so it is a plain variable, not state.
let activity = 0.12;
export const readActivity = () => activity;

/** Pages that know how many tasks are running call this; the field brightens and pulses with it. */
export function useFxActivity(running: number | null) {
  useEffect(() => {
    activity = running === null ? 0.12 : Math.min(1, 0.12 + running / 6);
    return () => {
      activity = 0.12;
    };
  }, [running]);
}
