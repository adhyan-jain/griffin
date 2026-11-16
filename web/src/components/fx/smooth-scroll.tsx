"use client";

import { useEffect } from "react";
import Lenis from "lenis";
import "lenis/dist/lenis.css";
import { ScrollTrigger, gsap, useFinePointer, useMotionAllowed } from "@/lib/motion";

/**
 * Inertial smooth scrolling for mouse and trackpad users. Touch keeps native momentum scrolling
 * (it is already smooth and Lenis would only fight it). Dialogs, selects and anything marked
 * `data-lenis-prevent` keep their own wheel scrolling. Lenis is driven by GSAP's ticker so
 * ScrollTrigger and the WebGL field read one consistent scroll position.
 */
export function SmoothScroll() {
  const allowed = useMotionAllowed();
  const fine = useFinePointer();

  useEffect(() => {
    if (!allowed || !fine) return;
    const lenis = new Lenis({
      lerp: 0.11,
      wheelMultiplier: 0.9,
      prevent: (node) => !!node.closest('[role="dialog"],[role="listbox"],[data-radix-popper-content-wrapper],[data-lenis-prevent]'),
    });
    lenis.on("scroll", ScrollTrigger.update);
    const tick = (t: number) => lenis.raf(t * 1000);
    gsap.ticker.add(tick);
    gsap.ticker.lagSmoothing(0);
    return () => {
      gsap.ticker.remove(tick);
      lenis.destroy();
    };
  }, [allowed, fine]);

  return null;
}
