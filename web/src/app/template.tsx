import type { ReactNode } from "react";

export default function Template({ children }: { children: ReactNode }) {
  return <div className="animate-in fade-in-50 duration-200">{children}</div>;
}
