"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { usePolling } from "@/lib/use-polling";
import { AlertCircle, ChevronDown, ListChecks, ShieldCheck } from "lucide-react";
import { stepResult } from "@/lib/jj-chat";
import { ChatText } from "@/components/chat-text";
import { api, type ChildTaskPlan, type SubmitResult, type Task } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { StatusBadge, isDone } from "@/components/StatusBadge";

const POLL_MS = 5000;
const label = (s: string) => s.replaceAll("_", " ");

/** Steps that must finish before `key` can start, as human titles. */
export function dependencyTitles(key: string, steps: ChildTaskPlan[], deps: { task_key: string; depends_on_key: string }[]): string[] {
  return deps
    .filter((d) => d.task_key === key)
    .map((d) => steps.find((s) => s.key === d.depends_on_key)?.title ?? d.depends_on_key);
}

/** One finished step's outcome: its text (collapsed to the first lines), its failure, or an honest "no result". */
function StepOutcome({ task }: { task: Task }) {
  const [open, setOpen] = useState(false);
  const r = stepResult(task);
  if (!r) return null;
  return (
    <li data-testid="step-result" className="rounded-xl border bg-background p-3">
      <p className="text-sm font-medium">{task.title}</p>
      {r.kind === "failed" ? (
        <p role="alert" className="mt-1.5 flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <AlertCircle className="mt-0.5 size-4 shrink-0" /> {r.reason}
        </p>
      ) : r.kind === "empty" ? (
        <p className="mt-1.5 flex items-start gap-2 rounded-lg border border-warning/40 bg-warning/10 px-3 py-2 text-sm text-warning">
          <AlertCircle className="mt-0.5 size-4 shrink-0" /> This step finished without a result
        </p>
      ) : (
        <div className="mt-1.5">
          <div className={open ? "" : "line-clamp-3"}>
            <ChatText text={r.text} />
          </div>
          <button
            type="button"
            aria-expanded={open}
            onClick={() => setOpen(!open)}
            className="mt-1 flex min-h-11 items-center gap-1 text-xs text-muted-foreground"
          >
            <ChevronDown className={`size-4 transition-transform ${open ? "rotate-180" : ""}`} /> {open ? "Show less" : "Show more"}
          </button>
        </div>
      )}
    </li>
  );
}

export function PlanCard({ result }: { result: SubmitResult }) {
  const { orchestration, plan } = result;
  const steps = useMemo(() => plan.plan_payload.child_tasks ?? [], [plan]);
  const deps = plan.plan_payload.dependencies ?? [];
  const rootId = orchestration.root_task_id;
  const [children, setChildren] = useState<Task[]>([]);
  const [pollError, setPollError] = useState(false);

  const total = steps.length || children.length;
  const finished = children.filter((c) => isDone(c.status));
  const empty = children.filter((c) => stepResult(c)?.kind === "empty").length;
  const done = children.filter((c) => c.status === "COMPLETED").length - empty; // empty output is not success
  const failed = children.filter((c) => c.status === "FAILED" || c.status === "CANCELLED").length;
  const active = total === 0 ? false : finished.length < total;
  const awaiting = children.filter((c) => c.status === "AWAITING_APPROVAL").length;

  // One bounded loop per card: fetch once, keep polling only while steps are unfinished.
  const [loaded, setLoaded] = useState(false);
  usePolling(
    async (signal) => {
      const c = await api.listTasks({ parent_id: rootId, limit: 100 }, { signal });
      setChildren(c);
      setPollError(false);
      setLoaded(true);
    },
    { intervalMs: POLL_MS, enabled: active || !loaded, onError: () => setPollError(true) },
  );

  const statusFor = (s: ChildTaskPlan) => children.find((c) => c.title === s.title)?.status;
  const noResult = (s: ChildTaskPlan) => {
    const c = children.find((x) => x.title === s.title);
    return c ? stepResult(c)?.kind === "empty" : false;
  };
  const pct =total ? Math.round((done / total) * 100) : 0;
  const manager = plan.manager_id.replaceAll("_", " ");

  return (
    <div data-testid="plan-card" className="w-full max-w-[96%] space-y-3 rounded-2xl rounded-bl-md border bg-card px-4 py-3">
      <p className="text-sm font-medium">
        {total || 1} step{total === 1 ? "" : "s"} led by {manager}
      </p>

      {steps.length > 0 && (
        <div>
          <h3 className="mb-1.5 text-xs font-medium text-muted-foreground">Delegation</h3>
          <ul className="space-y-2">
            {steps.map((s) => {
              const st = statusFor(s);
              return (
                <li key={s.key} className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
                  <span className="min-w-0 flex-1 basis-40">{s.title}</span>
                  <span className="text-xs text-muted-foreground">
                    {label(s.manager_domain)}
                    {s.worker_key ? ` / ${s.worker_key}` : ""}
                  </span>
                  {s.approval_required && (
                    <Badge variant="outline" className="border-primary/40 bg-primary/15 text-accent-foreground">
                      needs your OK
                    </Badge>
                  )}
                  {st &&
                    (noResult(s) ? (
                      <Badge variant="outline" className="border-transparent bg-warning/15 font-medium text-warning">
                        No result
                      </Badge>
                    ) : (
                      <StatusBadge status={st} />
                    ))}
                </li>
              );
            })}
          </ul>
        </div>
      )}

      {loaded && !active && children.length > 0 && (
        <div data-testid="plan-result">
          <h3 className="mb-1.5 text-xs font-medium text-muted-foreground">Result</h3>
          <ul className="space-y-2">
            {children.map((c) => (
              <StepOutcome key={c.id} task={c} />
            ))}
          </ul>
        </div>
      )}

      <div className="space-y-1.5">
        <Progress value={pct} aria-label="Plan progress" />
        <p className="text-xs tabular-nums text-muted-foreground" aria-live="polite">
          {done} of {total} done
          {empty > 0 ? `, ${empty} without a result` : ""}
          {failed > 0 ? `, ${failed} stopped` : ""}
          {pollError ? " (progress may be out of date)" : ""}
        </p>
      </div>

      <div className="flex flex-wrap gap-2">
        {awaiting > 0 && (
          <Button asChild size="sm" className="h-11 flex-1 sm:flex-none">
            <Link href="/approvals">
              <ShieldCheck /> Review {awaiting} approval{awaiting === 1 ? "" : "s"}
            </Link>
          </Button>
        )}
        <Sheet>
          <SheetTrigger asChild>
            <Button variant="outline" size="sm" className="h-11 flex-1 sm:flex-none">
              <ListChecks /> View plan
            </Button>
          </SheetTrigger>
          <SheetContent side="bottom" className="max-h-[85dvh] overflow-y-auto rounded-t-2xl pb-[max(1rem,env(safe-area-inset-bottom))]">
            <SheetHeader>
              <SheetTitle>{plan.objective || "Plan"}</SheetTitle>
              <SheetDescription>Led by {manager}. Each step starts once the steps it depends on are done.</SheetDescription>
            </SheetHeader>
            <ol className="space-y-3 px-4 pb-4">
              {steps.map((s, i) => {
                const after = dependencyTitles(s.key, steps, deps);
                const st = statusFor(s);
                return (
                  <li key={s.key} className="rounded-xl border bg-background p-3">
                    <div className="flex items-start justify-between gap-2">
                      <p className="text-sm font-medium">
                        <span className="tabular-nums text-muted-foreground">{i + 1}. </span>
                        {s.title}
                      </p>
                      {st && <StatusBadge status={st} />}
                    </div>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {label(s.manager_domain)}
                      {s.worker_key ? ` / ${s.worker_key}` : ""}
                      {s.approval_required ? " / needs your OK" : ""}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">{after.length ? `Starts after: ${after.join(", ")}` : "Starts right away"}</p>
                  </li>
                );
              })}
              {steps.length === 0 && <li className="text-sm text-muted-foreground">JJ is handling this as a single task.</li>}
            </ol>
          </SheetContent>
        </Sheet>
        <Button asChild variant="ghost" size="sm" className="h-11">
          <Link href={`/tasks/${rootId}`}>Open task</Link>
        </Button>
      </div>
    </div>
  );
}
