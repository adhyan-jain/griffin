"use client";

import dynamic from "next/dynamic";
import { useManagerName } from "@/lib/manager-name";
import Link from "next/link";
import { useState } from "react";
import { usePolling } from "@/lib/use-polling";
import { useRouter } from "next/navigation";
import { ChevronRight, MessageSquare, Plus, ShieldCheck } from "lucide-react";
import { api, type Task } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ACTIVE_STATUSES, WAITING_STATUSES } from "@/components/StatusBadge";
import { EmptyState, ErrorNote, PageContainer } from "@/components/page";
import { NewTaskDialog } from "@/components/new-task-dialog";
import { TaskRow } from "@/components/task-row";
import { Spotlight } from "@/components/fx/spotlight";
import { Counter, Kinetic } from "@/components/fx/kinetic";
import { Magnetic } from "@/components/fx/magnetic";
import { useFxActivity } from "@/lib/fx";

const Hero3D = dynamic(() => import("@/components/hero-3d"), { ssr: false });

export default function DashboardPage() {
  const managerName = useManagerName();
  const router = useRouter();
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [pending, setPending] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [newOpen, setNewOpen] = useState(false);

  usePolling(
    async (signal) => {
      const [t, a] = await Promise.all([api.listTasks({ limit: 100 }, { signal }), api.listApprovals("PENDING", { signal })]);
      setTasks(t);
      setPending(a.length);
      setError(null);
    },
    {
      intervalMs: 10000,
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

  useFxActivity(tasks ? running : null);

  const headline = running > 0 ? `${running} running now` : queued > 0 ? `${queued} waiting to start` : list.length ? "All quiet" : "Ready when you are";
  const sub = list.length
    ? `${done} finished${failed ? `, ${failed} failed` : ""}. Hand ${managerName} something new.`
    : `Ask ${managerName} to plan some work, or create a task yourself.`;

  return (
    <>
      <section data-reveal className="relative isolate min-h-[34rem] overflow-hidden md:min-h-[40rem]">
        <Hero3D
          nodes={list.map((t) => ({ id: t.id, status: t.status, title: t.title || t.goal }))}
          onSelect={(id) => router.push(`/tasks/${id}`)}
          className="absolute inset-x-0 top-0 z-0 h-[58%] [mask-image:linear-gradient(to_bottom,#000_78%,transparent)] md:inset-y-0 md:left-auto md:right-0 md:h-full md:w-[58%]"
        />
        {/* A soft pool of page colour behind the type, fading to nothing, so text stays legible without a visible box. */}
        <div aria-hidden className="hero-scrim pointer-events-none absolute inset-0 z-[1]" />
        <div className="pointer-events-none relative z-10 flex min-h-[34rem] flex-col justify-end gap-6 px-5 pb-8 md:min-h-[40rem] md:mx-auto md:max-w-5xl md:justify-center md:px-8 md:pb-0">
          <div>
            <Kinetic
              as="h1"
              text={headline}
              className="font-display max-w-[30rem] text-[clamp(2.25rem,5vw,4.5rem)] font-bold leading-[0.98] tracking-[-0.045em] text-balance md:max-w-[34rem]"
            />
            <p className="mt-4 max-w-md text-base text-muted-foreground md:text-lg">{sub}</p>
          </div>
          <div className="pointer-events-auto flex flex-wrap gap-3">
            <Magnetic>
              <Button asChild size="lg" className="h-12 rounded-full px-6 text-base">
                <Link href="/chat">
                  <MessageSquare /> Ask {managerName}
                </Link>
              </Button>
            </Magnetic>
            <Magnetic>
              <Button variant="outline" size="lg" className="h-12 rounded-full px-6 text-base backdrop-blur" onClick={() => setNewOpen(true)}>
                <Plus /> New task
              </Button>
            </Magnetic>
          </div>
        </div>
      </section>

      <PageContainer className="pt-2 md:pt-4">
      {pending > 0 && (
        <Link
          href="/approvals"
          data-reveal
          className="mt-4 flex min-h-14 items-center gap-3 rounded-xl border border-primary/40 bg-accent px-4 text-accent-foreground transition-colors active:bg-accent/70"
        >
          <ShieldCheck className="size-5 shrink-0" />
          <span className="flex-1 text-sm font-medium">
            {pending} approval{pending === 1 ? "" : "s"} waiting for you
          </span>
          <ChevronRight className="size-4" />
        </Link>
      )}

      <dl data-reveal className="mt-2 grid grid-cols-4 divide-x border-y">
        {[
          ["Running", running],
          ["Queued", queued],
          ["Done", done],
          ["Failed", failed],
        ].map(([label, n]) => (
          <div key={label} className="px-3 py-5 first:pl-0 md:px-8 md:py-7 md:first:pl-0">
            <dd>
              <Counter value={tasks ? Number(n) : null} className="font-display text-[clamp(1.75rem,6vw,3.75rem)] font-bold leading-none tracking-[-0.04em] tabular-nums" />
            </dd>
            <dt className="mt-2 text-xs text-muted-foreground md:text-sm">{label}</dt>
          </div>
        ))}
      </dl>

      <section data-reveal className="mt-10" aria-labelledby="recent">
        <div className="mb-2 flex items-center justify-between">
          <h2 id="recent" className="font-display text-xl font-bold tracking-tight">
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
            description={`Describe an outcome and ${managerName} will break it into steps.`}
            action={
              <Button onClick={() => router.push("/chat")}>
                <MessageSquare /> Ask {managerName}
              </Button>
            }
          />
        ) : (
          <Spotlight className="divide-y overflow-hidden rounded-2xl border bg-card">
            {recent.map((t, i) => (
              <div
                key={t.id}
                className="animate-in fade-in-0 slide-in-from-bottom-3 fill-mode-backwards duration-700"
                style={{ animationDelay: `${300 + i * 70}ms` }}
              >
                <TaskRow task={t} />
              </div>
            ))}
          </Spotlight>
        )}
      </section>

      <NewTaskDialog open={newOpen} onOpenChange={setNewOpen} onCreated={(t) => router.push(`/tasks/${t.id}`)} />
    </PageContainer>
    </>
  );
}
