"use client";

import Link from "next/link";
import { useState } from "react";
import { Check, Clock, Undo2, Wrench } from "lucide-react";
import { api, type JJAction } from "@/lib/api";
import { Button } from "@/components/ui/button";

type UndoState = { phase: "idle" } | { phase: "busy" } | { phase: "done" } | { phase: "error"; message: string };

function ActionRow({ action }: { action: JJAction }) {
  const [undo, setUndo] = useState<UndoState>({ phase: "idle" });

  const run = async () => {
    if (!action.undo_id) return;
    setUndo({ phase: "busy" });
    try {
      await api.undoAgentAction(action.undo_id);
      setUndo({ phase: "done" });
    } catch (e) {
      setUndo({ phase: "error", message: e instanceof Error && e.message ? e.message : "Could not undo that." });
    }
  };

  return (
    <li className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground" data-testid="jj-action">
      <Wrench className="size-3 shrink-0" aria-hidden />
      <span className="min-w-0 break-words">{action.summary}</span>
      {action.pending && (
        <Link
          href="/approvals"
          className="inline-flex items-center gap-1 rounded-full border border-warning/40 bg-warning/10 px-2 py-0.5 font-medium text-warning outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
        >
          <Clock className="size-3" aria-hidden /> Waiting for approval
        </Link>
      )}
      {action.undo_id && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-7 px-2 text-xs"
          disabled={undo.phase === "busy" || undo.phase === "done"}
          aria-label={`Undo: ${action.summary}`}
          onClick={() => void run()}
        >
          {undo.phase === "done" ? <Check aria-hidden /> : <Undo2 aria-hidden />}
          {undo.phase === "done" ? "Undone" : undo.phase === "busy" ? "Undoing" : "Undo"}
        </Button>
      )}
      {undo.phase === "error" && (
        <span role="alert" className="text-destructive">
          {undo.message}
        </span>
      )}
    </li>
  );
}

/** What JJ did during an agent turn, one quiet row each. */
export function ActionRows({ actions }: { actions: JJAction[] }) {
  if (actions.length === 0) return null;
  return (
    <ul aria-label="What JJ did" className="space-y-0.5 border-t pt-2">
      {actions.map((a, i) => (
        <ActionRow key={`${a.undo_id ?? a.tool}-${i}`} action={a} />
      ))}
    </ul>
  );
}
