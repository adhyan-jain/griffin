"use client";

import { useRef, type ReactNode } from "react";
import { cn } from "@/lib/utils";
import { useReducedMotion } from "@/lib/fx";

/**
 * Glass card with a brass specular highlight that follows the pointer and, optionally, a gentle 3D
 * tilt. Mouse and pen only (touch has no hover), rAF-throttled, and inert under reduced motion.
 */
export function Spotlight({
  children,
  className,
  tilt = 0,
}: {
  children: ReactNode;
  className?: string;
  /** Maximum tilt in degrees; 0 disables it. */
  tilt?: number;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const raf = useRef(0);
  const reduced = useReducedMotion();

  const move = (e: React.PointerEvent<HTMLDivElement>) => {
    if (reduced || e.pointerType === "touch") return;
    const el = ref.current;
    if (!el) return;
    const { clientX, clientY } = e;
    cancelAnimationFrame(raf.current);
    raf.current = requestAnimationFrame(() => {
      const r = el.getBoundingClientRect();
      const x = clientX - r.left;
      const y = clientY - r.top;
      el.style.setProperty("--mx", `${x}px`);
      el.style.setProperty("--my", `${y}px`);
      if (tilt > 0) {
        const px = x / r.width - 0.5;
        const py = y / r.height - 0.5;
        el.style.transform = `perspective(900px) rotateX(${(-py * tilt).toFixed(2)}deg) rotateY(${(px * tilt).toFixed(2)}deg) translateZ(0)`;
      }
    });
  };
  const leave = () => {
    cancelAnimationFrame(raf.current);
    if (ref.current && tilt > 0) ref.current.style.transform = "";
  };

  return (
    <div
      ref={ref}
      onPointerMove={move}
      onPointerLeave={leave}
      className={cn("group/spot relative [transform-style:preserve-3d] transition-transform duration-200 ease-out will-change-transform", className)}
    >
      {children}
      <span
        aria-hidden
        className="pointer-events-none absolute inset-0 rounded-[inherit] opacity-0 transition-opacity duration-300 group-hover/spot:opacity-100"
        style={{ background: "radial-gradient(240px circle at var(--mx, 50%) var(--my, 50%), color-mix(in oklab, var(--primary) 20%, transparent), transparent 70%)" }}
      />
    </div>
  );
}
