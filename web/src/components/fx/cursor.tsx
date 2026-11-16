"use client";

import { useRef } from "react";
import { useGSAP } from "@gsap/react";
import { gsap, useFinePointer, useMotionAllowed } from "@/lib/motion";

const INTERACTIVE = 'a,button,[role="button"],[role="tab"],[role="menuitem"],input,textarea,select,summary,[data-cursor]';

/**
 * A brass ring that trails the pointer and swells over anything clickable. It sits beside the
 * system cursor rather than replacing it, so precision and accessibility are untouched. Mouse and
 * pen only; not rendered on touch, with reduced motion, or with Effects Off.
 */
export function Cursor() {
  const allowed = useMotionAllowed();
  const fine = useFinePointer();
  const root = useRef<HTMLDivElement>(null);
  const ring = useRef<HTMLDivElement>(null);
  const dot = useRef<HTMLDivElement>(null);
  const on = allowed && fine;

  useGSAP(
    () => {
      if (!on || !ring.current || !dot.current) return;
      gsap.set([ring.current, dot.current], { xPercent: -50, yPercent: -50 });
      const rx = gsap.quickTo(ring.current, "x", { duration: 0.5, ease: "power3" });
      const ry = gsap.quickTo(ring.current, "y", { duration: 0.5, ease: "power3" });
      const dx = gsap.quickTo(dot.current, "x", { duration: 0.08, ease: "none" });
      const dy = gsap.quickTo(dot.current, "y", { duration: 0.08, ease: "none" });
      const show = (v: number) => gsap.to(root.current, { opacity: v, duration: 0.3 });

      const move = (e: PointerEvent) => {
        if (e.pointerType === "touch") return;
        rx(e.clientX);
        ry(e.clientY);
        dx(e.clientX);
        dy(e.clientY);
        show(1);
      };
      const over = (e: PointerEvent) => {
        const hot = (e.target as Element | null)?.closest?.(INTERACTIVE);
        gsap.to(ring.current, { scale: hot ? 1.9 : 1, duration: 0.45, ease: "expo.out" });
        gsap.to(ring.current, { backgroundColor: hot ? "color-mix(in oklab, var(--primary) 16%, transparent)" : "transparent", duration: 0.3 });
        gsap.to(dot.current, { scale: hot ? 0 : 1, duration: 0.25 });
      };
      const down = () => gsap.to(ring.current, { scale: 0.75, duration: 0.2, ease: "power2.out" });
      const up = (e: PointerEvent) => over(e);
      const leave = () => show(0);

      window.addEventListener("pointermove", move, { passive: true });
      window.addEventListener("pointerover", over, { passive: true });
      window.addEventListener("pointerdown", down);
      window.addEventListener("pointerup", up);
      document.documentElement.addEventListener("pointerleave", leave);
      return () => {
        window.removeEventListener("pointermove", move);
        window.removeEventListener("pointerover", over);
        window.removeEventListener("pointerdown", down);
        window.removeEventListener("pointerup", up);
        document.documentElement.removeEventListener("pointerleave", leave);
      };
    },
    { dependencies: [on], scope: root },
  );

  if (!on) return null;
  return (
    <div ref={root} aria-hidden className="pointer-events-none fixed left-0 top-0 z-[90] opacity-0">
      <div ref={ring} className="absolute left-0 top-0 size-9 rounded-full border border-primary/70" />
      <div ref={dot} className="absolute left-0 top-0 size-1.5 rounded-full bg-primary" />
    </div>
  );
}
