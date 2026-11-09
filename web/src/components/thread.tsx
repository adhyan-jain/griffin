"use client";

import { useState } from "react";
import Link from "next/link";
import { Loader2, Send } from "lucide-react";
import { toast } from "sonner";
import { api, type Task } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ErrorNote } from "@/components/page";
import { ResultView } from "@/components/result-view";
import { StatusBadge } from "@/components/StatusBadge";
import { useManagerName } from "@/lib/manager-name";
import { usePolling } from "@/lib/use-polling";
import { canFollowUp, followUpsOf, isFollowUp, threadRootId } from "@/lib/threads";

const ACTIVE = new Set(["CREATED", "ROUTED", "QUEUED", "RUNNING", "WAITING_FOR_DEPENDENCY", "VERIFYING", "AWAITING_APPROVAL", "RETRYING"]);

/**
 * The conversation around a task: what was asked next, what came back, and a box to keep going. A follow-up
 * joins this task's thread instead of becoming a separate task, and starts from everything said so far.
 */
export function Thread({ task }: { task: Task }) {
  const managerName = useManagerName();
  const rootId = threadRootId(task);
  const [followUps, setFollowUps] = useState<Task[]>([]);
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);

  const load = async (signal?: AbortSignal) => {
    const all = await api.listTasks({ limit: 200 }, { signal });
    setFollowUps(followUpsOf(all, rootId));
  };
  const busy = followUps.some((t) => ACTIVE.has(t.status));
  const { refresh } = usePolling(load, { intervalMs: busy ? 4000 : 120000 });

  // The thread's latest task is what a new message continues, so context chains in order.
  const rootDone = canFollowUp(task) || isFollowUp(task);
  const latest = followUps.length ? followUps[followUps.length - 1] : task;
  const ready = canFollowUp(latest);

  const send = async (e: React.FormEvent) => {
    e.preventDefault();
    const message = text.trim();
    if (!message || !ready) return;
    setSending(true);
    try {
      await api.followUp(latest.id, message);
      setText("");
      await refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not send that.");
    } finally {
      setSending(false);
    }
  };

  if (!rootDone && followUps.length === 0) return null; // nothing to continue yet

  return (
    <section className="space-y-3" aria-label="Follow-ups">
      <h2 className="text-sm font-semibold">Follow-ups</h2>
      {isFollowUp(task) && (
        <p className="text-sm text-muted-foreground">
          This is part of a thread.{" "}
          <Link href={`/tasks/${rootId}`} className="underline underline-offset-2">
            View the original task
          </Link>
        </p>
      )}
      <ul className="space-y-3">
        {followUps.map((f) => (
          <li key={f.id} className="space-y-2">
            <div className="ml-auto max-w-[85%] rounded-2xl rounded-br-md bg-primary/10 px-4 py-2.5 text-sm whitespace-pre-wrap">{f.goal}</div>
            {f.status === "COMPLETED" ? (
              <ResultView payload={f.output_payload} title={f.title || f.goal} />
            ) : f.status === "FAILED" || f.status === "CANCELLED" ? (
              <ErrorNote message={f.failure_reason || (f.status === "CANCELLED" ? "This was cancelled." : "This did not finish.")} />
            ) : (
              <div className="flex items-center gap-2 rounded-xl border bg-card px-4 py-3 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin motion-reduce:animate-none" /> {managerName} is working on it
                <span className="ml-auto">
                  <StatusBadge status={f.status} />
                </span>
              </div>
            )}
            <Link href={`/tasks/${f.id}`} className="block text-xs text-muted-foreground underline underline-offset-2">
              Open this step
            </Link>
          </li>
        ))}
      </ul>
      <form onSubmit={send} className="space-y-2">
        <Textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          maxLength={8000}
          rows={2}
          aria-label={`Follow up with ${managerName}`}
          placeholder={ready ? `Follow up: "make it shorter", "now send it"…` : "Wait for the current step to finish"}
          disabled={!ready || sending}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) (e.currentTarget.form as HTMLFormElement | null)?.requestSubmit();
          }}
        />
        <Button type="submit" className="h-11 w-full sm:w-auto" disabled={!ready || sending || text.trim() === ""}>
          {sending ? <Loader2 className="animate-spin" /> : <Send />} Send
        </Button>
      </form>
    </section>
  );
}
