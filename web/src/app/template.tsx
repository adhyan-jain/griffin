"use client";

import { useRef, type ReactNode } from "react";
import { useGSAP } from "@gsap/react";
import { EASE, gsap, useMotionAllowed } from "@/lib/motion";

/**
 * Next re-mounts a template on every navigation, so this is the route transition: elements tagged
 * `data-reveal`, and the top-level blocks of every page container, rise into place in a short stagger. Anything that arrives later (fetched lists)
 * animates itself; this only choreographs what is on screen the moment the route opens.
 */
export default function Template({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const allowed = useMotionAllowed();

  useGSAP(
    () => {
      if (!allowed) return;
      const items = ref.current?.querySelectorAll("[data-reveal], [data-page] > *");
      if (!items?.length) return;
      gsap.fromTo(items, { y: 34, opacity: 0 }, { y: 0, opacity: 1, duration: 1, ease: EASE.out, stagger: 0.08, clearProps: "transform,opacity" });
    },
    { dependencies: [allowed], scope: ref },
  );

  return <div ref={ref}>{children}</div>;
}
