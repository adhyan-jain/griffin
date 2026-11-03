import Link from "next/link";
import type { Task } from "@/lib/api";
import { StatusBadge } from "@/components/StatusBadge";
import { timeAgo } from "@/components/page";

const PRIORITY_NOTE: Record<string, string> = { CRITICAL_P0: "Critical", HIGH_P1: "High priority" };

/** One task as a single tappable row: title first, one quiet meta line, status on the right. */
export function TaskRow({ task, subtasks = 0 }: { task: Task; subtasks?: number }) {
  const meta = [
    task.manager_domain,
    timeAgo(task.created_at),
    subtasks > 0 ? `${subtasks} subtask${subtasks === 1 ? "" : "s"}` : null,
    PRIORITY_NOTE[task.priority] ?? null,
  ].filter(Boolean);
  return (
    <Link
      href={`/tasks/${task.id}`}
      className="flex min-h-16 items-start gap-3 px-4 py-3.5 transition-colors hover:bg-muted/40 active:bg-muted/60"
    >
      <div className="min-w-0 flex-1">
        <p className="line-clamp-2 text-sm font-medium leading-snug">{task.title || task.goal}</p>
        <p className="mt-1 truncate text-xs text-muted-foreground">{meta.join(" · ")}</p>
      </div>
      <StatusBadge status={task.status} />
    </Link>
  );
}
