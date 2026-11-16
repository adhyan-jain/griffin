"use client";

import { useRef, type ElementType } from "react";
import { useGSAP } from "@gsap/react";
import { EASE, SplitText, gsap, useMotionAllowed } from "@/lib/motion";

/**
 * Headline whose letters rise out of a mask. It replays whenever `text` changes (the element is
 * re-keyed so React never edits DOM that SplitText has rewritten). With motion off it is plain text.
 * SplitText keeps the readable string on aria-label, so screen readers hear the sentence, not letters.
 */
export function Kinetic({
  text,
  as: Tag = "span",
  className,
  delay = 0,
}: {
  text: string;
  as?: ElementType;
  className?: string;
  delay?: number;
}) {
  const ref = useRef<HTMLElement>(null);
  const allowed = useMotionAllowed();

  useGSAP(
    () => {
      const el = ref.current;
      if (!el || !allowed) return;
      const split = SplitText.create(el, { type: "words,chars", mask: "words" });
      gsap.from(split.chars, {
        yPercent: 118,
        rotate: 7,
        duration: 1.15,
        delay,
        ease: EASE.out,
        stagger: { each: 0.03 },
        // The reveal mask crops descenders on tightly set lines, so drop it the moment the motion ends.
        onComplete: () => split.revert(),
      });
    },
    { dependencies: [text, allowed, delay], scope: ref, revertOnUpdate: true },
  );

  return (
    <Tag key={text} ref={ref} className={className}>
      {text}
    </Tag>
  );
}

/**
 * A number that tweens to its new value (digits roll up on load and on every change). `null` shows a
 * dash while data is loading. With motion off it just shows the value.
 */
export function Counter({ value, className }: { value: number | null; className?: string }) {
  const ref = useRef<HTMLSpanElement>(null);
  const shown = useRef(0);
  const allowed = useMotionAllowed();

  useGSAP(
    () => {
      const el = ref.current;
      if (!el || value === null) return;
      if (!allowed) {
        el.textContent = String(value);
        shown.current = value;
        return;
      }
      const state = { n: shown.current };
      gsap.to(state, {
        n: value,
        duration: 1.4,
        ease: EASE.out,
        onUpdate: () => {
          shown.current = state.n;
          el.textContent = String(Math.round(state.n));
        },
      });
    },
    { dependencies: [value, allowed], scope: ref },
  );

  return (
    <span key={value === null ? "loading" : "ready"} ref={ref} className={className}>
      {value === null ? "–" : null}
    </span>
  );
}
