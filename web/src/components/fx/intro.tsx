"use client";

import { useRef } from "react";
import { useGSAP } from "@gsap/react";
import { EASE, SplitText, gsap, useMotionAllowed } from "@/lib/motion";

const KEY = "griffin_intro";

/**
 * The boot sequence: the wordmark rises, a brass line draws, then the curtain lifts off the live
 * scene. Plays once per browser session. Skipped with reduced motion, Effects Off, and under browser
 * automation (so tests and screenshots are never held behind it). It never waits on data.
 */
export function Intro() {
  const root = useRef<HTMLDivElement>(null);
  const word = useRef<HTMLDivElement>(null);
  const line = useRef<HTMLDivElement>(null);
  const allowed = useMotionAllowed();

  useGSAP(
    () => {
      if (!allowed || navigator.webdriver) return;
      try {
        if (sessionStorage.getItem(KEY)) return;
        sessionStorage.setItem(KEY, "1");
      } catch {
        return; // storage blocked: skip rather than replay on every navigation
      }
      const split = SplitText.create(word.current!, { type: "chars", mask: "chars" });
      gsap.set(root.current, { autoAlpha: 1, clipPath: "inset(0% 0% 0% 0%)" });
      gsap
        .timeline({ defaults: { ease: EASE.out } })
        .from(split.chars, { yPercent: 120, duration: 1, stagger: 0.055 })
        .from(line.current, { scaleX: 0, transformOrigin: "left center", duration: 1.1, ease: EASE.inOut }, "-=0.7")
        .to(split.chars, { yPercent: -120, duration: 0.7, stagger: 0.03, ease: "expo.in" }, "+=0.25")
        .to(line.current, { scaleX: 0, transformOrigin: "right center", duration: 0.5, ease: "expo.in" }, "<")
        .to(root.current, { clipPath: "inset(0% 0% 100% 0%)", duration: 0.95, ease: EASE.inOut }, "-=0.25")
        .set(root.current, { autoAlpha: 0 });
    },
    { dependencies: [allowed], scope: root },
  );

  return (
    <div ref={root} aria-hidden className="pointer-events-none invisible fixed inset-0 z-[100] flex flex-col items-center justify-center gap-5 bg-background">
      <div ref={word} className="font-display text-[clamp(3rem,13vw,9rem)] font-extrabold leading-none tracking-[-0.05em]">
        Griffin
      </div>
      <div ref={line} className="h-px w-[min(60vw,22rem)] bg-primary" />
    </div>
  );
}
