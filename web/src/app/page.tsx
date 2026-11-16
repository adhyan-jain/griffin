"use client";

import { useManagerName } from "@/lib/manager-name";
import Link from "next/link";
import { useState } from "react";
import { usePolling } from "@/lib/use-polling";
import { useRouter } from "next/navigation";
import { ChevronRight, MessageSquare, Plus, ShieldCheck, Table2, ListChecks } from "lucide-react";
import { api, type Task } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ACTIVE_STATUSES, WAITING_STATUSES } from "@/components/StatusBadge";
import { EmptyState, ErrorNote, PageContainer } from "@/components/page";
import { NewTaskDialog } from "@/components/new-task-dialog";
import { TaskRow } from "@/components/task-row";

export default function DashboardPage() {
  const managerName = useManagerName();
  const router = useRouter();
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [pending, setPending] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [newOpen, setNewOpen] = useState(false);

  usePolling(
    async (signal) => {
      const [t, a] = await Promise.all([
        api.listTasks({ limit: 100 }, { signal }),
        api.listApprovals("PENDING", { signal }),
      ]);
      setTasks(t);
      setPending(a.length);
      setError(null);
    },
    {
      intervalMs: 15000,
      onError: (e) => {
        setError(e instanceof Error ? e.message : "Could not load tasks.");
        setTasks((prev) => prev ?? []);
      },
    },
  );

  const list = tasks ?? [];
  const running = list.filter((t) => ACTIVE_STATUSES.includes(t.status)).length;
  const queued = list.filter((t) => WAITING_STATUSES.includes(t.status)).length;
  const done = list.filter((t) => t.status === "COMPLETED").length;
  const failed = list.filter((t) => t.status === "FAILED").length;
  const recent = list.filter((t) => !t.parent_id).slice(0, 6);

  const headline =
    running > 0
      ? `${running} running now`
      : queued > 0
      ? `${queued} waiting to start`
      : list.length
      ? "All quiet"
      : "Ready when you are";

  const sub = list.length
    ? `${done} finished${failed ? `, ${failed} failed` : ""}. Hand ${managerName} something new.`
    : `Ask ${managerName} to plan some work, or create a task yourself.`;

  return (
    <PageContainer className="pt-6 md:pt-10 space-y-8">
      {/* Hero Banner */}
      <section className="relative overflow-hidden rounded-3xl border bg-gradient-to-br from-card via-card/80 to-primary/5 p-6 md:p-10 shadow-xs">
        <div className="max-w-2xl space-y-4">
          <h1 className="font-display text-3xl font-bold tracking-tight text-foreground md:text-5xl">
            {headline}
          </h1>
          <p className="text-base text-muted-foreground md:text-lg leading-relaxed">{sub}</p>
          <div className="flex flex-wrap items-center gap-3 pt-2">
            <Button asChild size="lg" className="h-11 rounded-xl px-5 text-sm font-medium shadow-xs">
              <Link href="/chat">
                <MessageSquare className="size-4" /> Ask {managerName}
              </Link>
            </Button>
            <Button asChild variant="outline" size="lg" className="h-11 rounded-xl px-5 text-sm font-medium">
              <Link href="/trackers">
                <Table2 className="size-4" /> View Trackers
              </Link>
            </Button>
            <Button
              variant="outline"
              size="lg"
              className="h-11 rounded-xl px-5 text-sm font-medium"
              onClick={() => setNewOpen(true)}
            >
              <Plus className="size-4" /> New task
            </Button>
          </div>
        </div>
      </section>

      {/* Approvals banner if pending */}
      {pending > 0 && (
        <Link
          href="/approvals"
          className="flex min-h-14 items-center gap-3 rounded-2xl border border-primary/40 bg-accent px-5 text-accent-foreground transition-colors hover:bg-accent/80 shadow-xs"
        >
          <ShieldCheck className="size-5 shrink-0 text-primary" />
          <span className="flex-1 text-sm font-medium">
            {pending} approval{pending === 1 ? "" : "s"} waiting for your review
          </span>
          <ChevronRight className="size-4" />
        </Link>
      )}

      {/* Metric Counters Grid */}
      <dl className="grid grid-cols-2 md:grid-cols-4 gap-3 sm:gap-4">
        {[
          ["Running", running, "text-primary"],
          ["Queued", queued, "text-muted-foreground"],
          ["Done", done, "text-foreground"],
          ["Failed", failed, failed > 0 ? "text-destructive" : "text-muted-foreground"],
        ].map(([label, n, colorClass]) => (
          <div key={label} className="rounded-2xl border bg-card/60 p-4 md:p-6 shadow-xs">
            <dd className={`font-display text-3xl md:text-5xl font-bold tracking-tight tabular-nums ${colorClass}`}>
              {tasks ? Number(n) : "-"}
            </dd>
            <dt className="mt-1 text-xs md:text-sm font-medium text-muted-foreground">{label}</dt>
          </div>
        ))}
      </dl>

      {/* Recent Tasks */}
      <section aria-labelledby="recent" className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <ListChecks className="size-5 text-primary" />
            <h2 id="recent" className="font-display text-lg font-bold tracking-tight text-foreground">
              Recent Tasks
            </h2>
          </div>
          <Link href="/tasks" className="text-xs md:text-sm font-medium text-primary hover:underline">
            View all tasks
          </Link>
        </div>

        {error && <ErrorNote message={error} />}

        {tasks === null ? (
          <div className="space-y-2">
            <Skeleton className="h-16 w-full rounded-2xl" />
            <Skeleton className="h-16 w-full rounded-2xl" />
            <Skeleton className="h-16 w-full rounded-2xl" />
          </div>
        ) : recent.length === 0 ? (
          <EmptyState
            icon={<MessageSquare />}
            title="No tasks yet"
            description={`Describe an outcome and ${managerName} will break it into steps.`}
            action={
              <Button onClick={() => router.push("/chat")}>
                <MessageSquare className="size-4" /> Ask {managerName}
              </Button>
            }
          />
        ) : (
          <div className="divide-y overflow-hidden rounded-2xl border bg-card shadow-xs">
            {recent.map((t) => (
              <TaskRow key={t.id} task={t} />
            ))}
          </div>
        )}
      </section>

      <NewTaskDialog open={newOpen} onOpenChange={setNewOpen} onCreated={(t) => router.push(`/tasks/${t.id}`)} />
    </PageContainer>
  );
}
