import Link from "next/link";
import { Archive, ArchiveRestore, MoreVertical, Play, RotateCw, Trash2 } from "lucide-react";
import type { Task } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { StatusBadge } from "@/components/StatusBadge";
import { timeAgo } from "@/components/page";
import { canArchive, canDelete, canResume, canRunAgain } from "@/lib/task-actions";

const PRIORITY_NOTE: Record<string, string> = { CRITICAL_P0: "Critical", HIGH_P1: "High priority" };

export type RowActions = {
  onResume: (t: Task) => void;
  onRunAgain: (t: Task) => void;
  onDelete: (t: Task) => void;
  onArchive: (t: Task) => void;
  onUnarchive: (t: Task) => void;
};

/**
 * One task as a single tappable row: title first, one quiet meta line, status on the right.
 * Finished work gets a quiet overflow menu (resume / run again / archive / delete); an archived task's menu
 * restores or deletes it. In selection mode the
 * whole row toggles a checkbox instead of navigating.
 */
export function TaskRow({
  task,
  subtasks = 0,
  actions,
  selecting = false,
  selected = false,
  archived = false,
  onToggle,
}: {
  task: Task;
  subtasks?: number;
  actions?: RowActions;
  selecting?: boolean;
  selected?: boolean;
  /** The task is in the archive, so its menu offers Unarchive instead of Archive. */
  archived?: boolean;
  onToggle?: (t: Task) => void;
}) {
  const meta = [
    task.manager_domain,
    timeAgo(task.completed_at && ["COMPLETED", "FAILED", "CANCELLED"].includes(task.status) ? task.completed_at : task.created_at),
    subtasks > 0 ? `${subtasks} subtask${subtasks === 1 ? "" : "s"}` : null,
    PRIORITY_NOTE[task.priority] ?? null,
  ].filter(Boolean);
  const body = (
    <>
      <div className="min-w-0 flex-1">
        <p className="line-clamp-2 text-sm font-medium leading-snug">{task.title || task.goal}</p>
        <p className="mt-1 truncate text-xs text-muted-foreground">{meta.join(" · ")}</p>
      </div>
      <StatusBadge status={task.status} />
    </>
  );
  const rowCls = "flex min-h-16 min-w-0 flex-1 items-start gap-3 py-3.5 transition-colors";
  const hasMenu = actions && (archived || canResume(task) || canRunAgain(task) || canArchive(task) || canDelete(task));

  if (selecting) {
    return (
      <label className={`${rowCls} cursor-pointer px-4 hover:bg-muted/40 ${selected ? "bg-primary/5" : ""}`}>
        <Checkbox checked={selected} onCheckedChange={() => onToggle?.(task)} aria-label={`Select ${task.title || task.goal}`} className="mt-0.5" />
        {body}
      </label>
    );
  }
  return (
    <div className="flex items-start hover:bg-muted/40">
      <Link href={`/tasks/${task.id}`} className={`${rowCls} pl-4 pr-2 active:bg-muted/60`}>
        {body}
      </Link>
      {hasMenu ? (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="mt-2 size-11 shrink-0 text-muted-foreground" aria-label={`Actions for ${task.title || task.goal}`}>
              <MoreVertical />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="min-w-44">
            {archived && (
              <DropdownMenuItem className="min-h-11" onSelect={() => actions.onUnarchive(task)}>
                <ArchiveRestore /> Unarchive
              </DropdownMenuItem>
            )}
            {!archived && canResume(task) && (
              <DropdownMenuItem className="min-h-11" onSelect={() => actions.onResume(task)}>
                <Play /> Resume
              </DropdownMenuItem>
            )}
            {!archived && canRunAgain(task) && (
              <DropdownMenuItem className="min-h-11" onSelect={() => actions.onRunAgain(task)}>
                <RotateCw /> Run again
              </DropdownMenuItem>
            )}
            {!archived && canArchive(task) && (
              <DropdownMenuItem className="min-h-11" onSelect={() => actions.onArchive(task)}>
                <Archive /> Archive
              </DropdownMenuItem>
            )}
            {canDelete(task) && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" className="min-h-11" onSelect={() => actions.onDelete(task)}>
                  <Trash2 /> Delete
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      ) : (
        <span className="w-2 shrink-0" />
      )}
    </div>
  );
}
