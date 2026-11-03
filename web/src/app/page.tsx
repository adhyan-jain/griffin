"use client";

import dynamic from "next/dynamic";
import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { ChevronRight, MessageSquare, Plus, ShieldCheck } from "lucide-react";
import { api, type Task } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ACTIVE_STATUSES, WAITING_STATUSES } from "@/components/StatusBadge";
import { EmptyState, ErrorNote, PageContainer } from "@/components/page";
import { NewTaskDialog } from "@/components/new-task-dialog";
import { TaskRow } from "@/components/task-row";

const Hero3D = dynamic(() => import("@/components/hero-3d"), { ssr: false });

export default function DashboardPage() {
  const router = useRouter();
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [pending, setPending] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [newOpen, setNewOpen] = useState(false);

  const load = useCallback(async () => {
    try {
      const [t, a] = await Promise.all([api.listTasks({ limit: 100 }), api.listApprovals("PENDING")]);
      setTasks(t);
      setPending(a.length);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not load tasks.");
      setTasks((prev) => prev ?? []);
    }
  }, []);

  useEffect(() => {
    void load();
    const id = setInterval(load, 10000);
    return () => clearInterval(id);
  }, [load]);

  const list = tasks ?? [];
  const running = list.filter((t) => ACTIVE_STATUSES.includes(t.status)).length;
  const queued = list.filter((t) => WAITING_STATUSES.includes(t.status)).length;
  const done = list.filter((t) => t.status === "COMPLETED").length;
  const failed = list.filter((t) => t.status === "FAILED").length;
  const recent = list.filter((t) => !t.parent_id).slice(0, 6);

  const headline = running > 0 ? `${running} running now` : queued > 0 ? `${queued} waiting to start` : list.length ? "All quiet" : "Ready when you are";
  const sub = list.length
    ? `${done} finished${failed ? `, ${failed} failed` : ""}. Hand JJ something new.`
    : "Ask JJ to plan some work, or create a task yourself.";

  return (
    <PageContainer>
      <section className="grid overflow-hidden rounded-2xl border bg-card md:grid-cols-2">
        <Hero3D nodes={list.map((t) => ({ id: t.id, status: t.status }))} className="h-44 md:order-2 md:h-72" />
        <div className="flex flex-col justify-center gap-4 p-5 md:p-8">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight md:text-3xl">{headline}</h1>
            <p className="mt-1.5 text-sm text-muted-foreground">{sub}</p>
          </div>
          <div className="flex gap-2">
            <Button asChild className="h-11 flex-1 md:flex-none">
              <Link href="/chat">
                <MessageSquare /> Ask JJ
              </Link>
            </Button>
            <Button variant="outline" className="h-11 flex-1 md:flex-none" onClick={() => setNewOpen(true)}>
              <Plus /> New task
            </Button>
          </div>
        </div>
      </section>

      {pending > 0 && (
        <Link
          href="/approvals"
          className="mt-3 flex min-h-14 items-center gap-3 rounded-xl border border-primary/40 bg-accent px-4 text-accent-foreground transition-colors active:bg-accent/70"
        >
          <ShieldCheck className="size-5 shrink-0" />
          <span className="flex-1 text-sm font-medium">
            {pending} approval{pending === 1 ? "" : "s"} waiting for you
          </span>
          <ChevronRight className="size-4" />
        </Link>
      )}

      <dl className="mt-3 grid grid-cols-4 gap-2">
        {[
          ["Running", running],
          ["Queued", queued],
          ["Done", done],
          ["Failed", failed],
        ].map(([label, n]) => (
          <div key={label} className="rounded-xl border bg-card px-3 py-2.5">
            <dd className="text-xl font-semibold tabular-nums">{tasks ? n : "–"}</dd>
            <dt className="text-xs text-muted-foreground">{label}</dt>
          </div>
        ))}
      </dl>

      <section className="mt-6" aria-labelledby="recent">
        <div className="mb-2 flex items-center justify-between">
          <h2 id="recent" className="text-base font-semibold">
            Recent tasks
          </h2>
          <Link href="/tasks" className="text-sm font-medium text-primary">
            See all
          </Link>
        </div>
        {error && <ErrorNote message={error} />}
        {tasks === null ? (
          <div className="space-y-2">
            <Skeleton className="h-16 w-full rounded-xl" />
            <Skeleton className="h-16 w-full rounded-xl" />
            <Skeleton className="h-16 w-full rounded-xl" />
          </div>
        ) : recent.length === 0 ? (
          <EmptyState
            icon={<MessageSquare />}
            title="No tasks yet"
            description="Describe an outcome and JJ will break it into steps."
            action={
              <Button onClick={() => router.push("/chat")}>
                <MessageSquare /> Ask JJ
              </Button>
            }
          />
        ) : (
          <div className="divide-y overflow-hidden rounded-xl border bg-card">
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
