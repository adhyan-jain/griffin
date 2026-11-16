"use client";

import { useSyncExternalStore } from "react";
import gsap from "gsap";
import { ScrollTrigger } from "gsap/ScrollTrigger";
import { SplitText } from "gsap/SplitText";
import { useFxMode, useReducedMotion } from "@/lib/fx";

/** Plugins are registered once, here, so every component imports the same configured gsap. */
if (typeof window !== "undefined") gsap.registerPlugin(ScrollTrigger, SplitText);

export { gsap, ScrollTrigger, SplitText };

/** Easing vocabulary shared by every animation so the whole app moves with one voice. */
export const EASE = { out: "expo.out", inOut: "expo.inOut", soft: "power3.out" } as const;

/**
 * Whether decorative motion may run: the user has not turned effects off and the OS does not ask for
 * reduced motion. Server renders and the first client pass say false, so nothing animates before hydration.
 */
export function useMotionAllowed(): boolean {
  const [mode] = useFxMode();
  const reduced = useReducedMotion();
  return mode !== "off" && !reduced;
}

/** Mouse or pen with hover. Touch screens get none of the pointer-driven effects. Live, like reduced motion. */
export function useFinePointer(): boolean {
  return useSyncExternalStore(
    (cb) => {
      const mq = window.matchMedia("(hover: hover) and (pointer: fine)");
      mq.addEventListener("change", cb);
      return () => mq.removeEventListener("change", cb);
    },
    () => window.matchMedia("(hover: hover) and (pointer: fine)").matches,
    () => false,
  );
}
