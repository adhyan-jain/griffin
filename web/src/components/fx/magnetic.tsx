"use client";

import { useRef, type ReactNode } from "react";
import { useGSAP } from "@gsap/react";
import { cn } from "@/lib/utils";
import { gsap, useFinePointer, useMotionAllowed } from "@/lib/motion";

/**
 * Pulls its child toward the pointer when the pointer comes near, then springs back. Mouse and pen
 * only: touch has no hover, and reduced motion or Effects Off leaves the child where it is.
 */
export function Magnetic({ children, strength = 0.32, className }: { children: ReactNode; strength?: number; className?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const allowed = useMotionAllowed();
  const fine = useFinePointer();

  useGSAP(
    () => {
      const el = ref.current;
      if (!el || !allowed || !fine) return;
      const x = gsap.quickTo(el, "x", { duration: 0.7, ease: "elastic.out(1, 0.45)" });
      const y = gsap.quickTo(el, "y", { duration: 0.7, ease: "elastic.out(1, 0.45)" });
      const move = (e: PointerEvent) => {
        const r = el.getBoundingClientRect();
        const dx = e.clientX - (r.left + r.width / 2);
        const dy = e.clientY - (r.top + r.height / 2);
        const reach = Math.max(r.width, r.height) * 0.9;
        if (Math.hypot(dx, dy) < reach) {
          x(dx * strength);
          y(dy * strength);
        } else {
          x(0);
          y(0);
        }
      };
      window.addEventListener("pointermove", move, { passive: true });
      return () => window.removeEventListener("pointermove", move);
    },
    { dependencies: [allowed, fine, strength], scope: ref },
  );

  return (
    <div ref={ref} className={cn("inline-block will-change-transform", className)}>
      {children}
    </div>
  );
}
