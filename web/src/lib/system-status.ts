import { SYSTEM_COMPONENTS, type ComponentState, type SystemStatus } from "@/lib/api";

export type OverallState = ComponentState | "unavailable";

/**
 * Collapses the component map into one indicator. Green requires every known component to report
 * "ok"; anything missing or unknown stays neutral so the dot never claims more than was observed.
 */
export function overallState(status: SystemStatus | null): OverallState {
  if (!status) return "unavailable";
  const states = SYSTEM_COMPONENTS.map((n) => status.components[n]?.state ?? "unknown");
  if (states.includes("down")) return "down";
  if (states.includes("degraded")) return "degraded";
  if (states.every((s) => s === "ok")) return "ok";
  return "unknown";
}

export const STATE_LABEL: Record<OverallState, string> = {
  ok: "Healthy",
  degraded: "Degraded",
  down: "Down",
  unknown: "Unknown",
  unavailable: "Status unavailable",
};
