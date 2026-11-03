import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { Approval, TaskPriority, TaskStatus } from "@/lib/api";

const STATUS: Record<string, { label: string; className: string; pulse?: boolean }> = {
  COMPLETED: { label: "Completed", className: "bg-success/15 text-success" },
  RUNNING: { label: "Running", className: "bg-info/15 text-info", pulse: true },
  VERIFYING: { label: "Verifying", className: "bg-info/15 text-info", pulse: true },
  QUEUED: { label: "Queued", className: "bg-warning/15 text-warning" },
  CREATED: { label: "Created", className: "bg-warning/15 text-warning" },
  ROUTED: { label: "Routed", className: "bg-warning/15 text-warning" },
  RETRYING: { label: "Retrying", className: "bg-warning/15 text-warning", pulse: true },
  WAITING_FOR_DEPENDENCY: { label: "Waiting", className: "bg-muted text-muted-foreground" },
  AWAITING_APPROVAL: { label: "Needs approval", className: "bg-primary/20 text-accent-foreground" },
  FAILED: { label: "Failed", className: "bg-destructive/15 text-destructive" },
  CANCELLED: { label: "Cancelled", className: "bg-muted text-muted-foreground" },
  PENDING: { label: "Pending", className: "bg-primary/20 text-accent-foreground" },
  APPROVED: { label: "Approved", className: "bg-success/15 text-success" },
  REJECTED: { label: "Rejected", className: "bg-destructive/15 text-destructive" },
  EXPIRED: { label: "Expired", className: "bg-muted text-muted-foreground" },
};

export function StatusBadge({ status }: { status: TaskStatus | Approval["status"] | string }) {
  const s = STATUS[status] ?? { label: status, className: "bg-muted text-muted-foreground" };
  return (
    <Badge variant="outline" className={cn("border-transparent font-medium", s.className)}>
      {s.pulse && <span className="size-1.5 animate-pulse rounded-full bg-current" aria-hidden />}
      {s.label}
    </Badge>
  );
}

const PRIORITY: Record<TaskPriority, { label: string; className: string }> = {
  CRITICAL_P0: { label: "Critical", className: "text-destructive" },
  HIGH_P1: { label: "High", className: "text-warning" },
  NORMAL_P2: { label: "Normal", className: "text-muted-foreground" },
  BACKGROUND_P3: { label: "Background", className: "text-muted-foreground/70" },
};

export function PriorityBadge({ priority }: { priority: TaskPriority }) {
  const p = PRIORITY[priority] ?? PRIORITY.NORMAL_P2;
  return <span className={cn("text-xs font-medium", p.className)}>{p.label}</span>;
}

const RISK: Record<string, string> = {
  LOW: "bg-success/15 text-success",
  MEDIUM: "bg-warning/15 text-warning",
  HIGH: "bg-destructive/15 text-destructive",
  CRITICAL: "bg-destructive text-white",
};

export function RiskBadge({ risk }: { risk: Approval["risk_level"] }) {
  return (
    <Badge variant="outline" className={cn("border-transparent font-medium", RISK[risk])}>
      {risk.charAt(0) + risk.slice(1).toLowerCase()} risk
    </Badge>
  );
}

export const ACTIVE_STATUSES: string[] = ["RUNNING", "VERIFYING", "RETRYING"];
export const WAITING_STATUSES: string[] = ["QUEUED", "CREATED", "ROUTED", "WAITING_FOR_DEPENDENCY"];
export const isDone = (s: string) => s === "COMPLETED" || s === "FAILED" || s === "CANCELLED";
