"use client";

import { useEffect, useSyncExternalStore } from "react";
import { api } from "@/lib/api";

/**
 * The name of the top manager (JJ until the owner picks another). It lives on the server, so it follows
 * the owner to every device; a copy is kept in the browser so the first paint already shows it.
 */
export const DEFAULT_MANAGER_NAME = "JJ";
const KEY = "griffin_manager_name";

let current = DEFAULT_MANAGER_NAME;
let loaded = false;
let inflight: Promise<void> | null = null;
const listeners = new Set<() => void>();

function readCache(): string {
  try {
    return window.localStorage.getItem(KEY) || DEFAULT_MANAGER_NAME;
  } catch {
    return DEFAULT_MANAGER_NAME;
  }
}

function set(name: string) {
  if (name === current) return;
  current = name;
  try {
    window.localStorage.setItem(KEY, name);
  } catch {
    /* private mode or blocked storage: the server copy still wins on the next load */
  }
  listeners.forEach((l) => l());
}

/** Fetches the saved name once per page load. Failure keeps whatever is shown. */
export function refreshManagerName(): Promise<void> {
  if (loaded) return Promise.resolve();
  inflight ??= api
    .getManagerName()
    .then((r) => {
      loaded = true;
      set(r.manager_name);
    })
    .catch(() => {})
    .finally(() => {
      inflight = null;
    });
  return inflight;
}

/** Saves a new name and shows it everywhere at once. Throws the server's message when it is refused. */
export async function saveManagerName(name: string): Promise<string> {
  const r = await api.putManagerName(name);
  loaded = true;
  set(r.manager_name);
  return r.manager_name;
}

export function useManagerName(): string {
  useEffect(() => {
    if (typeof window !== "undefined" && !loaded && current === DEFAULT_MANAGER_NAME) current = readCache();
    void refreshManagerName();
  }, []);
  return useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
    () => current,
    () => DEFAULT_MANAGER_NAME,
  );
}

/** For tests: forget everything. */
export function resetManagerNameForTests() {
  current = DEFAULT_MANAGER_NAME;
  loaded = false;
  inflight = null;
  listeners.clear();
}
