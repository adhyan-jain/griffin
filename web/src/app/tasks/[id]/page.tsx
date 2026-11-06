"use client";

import { use, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ChevronLeft, Download, FileText, Loader2, Play, RotateCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, type Artifact, type Task, type TaskAttempt, type TaskEvent } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { PriorityBadge, StatusBadge, isDone } from "@/components/StatusBadge";
import { EmptyState, ErrorNote, PageContainer, formatBytes, timeAgo } from "@/components/page";
import { ResponsiveDialog } from "@/components/responsive-dialog";
import { TaskRow } from "@/components/task-row";
import { deriveNow } from "@/lib/task-now";
import { usePolling } from "@/lib/use-polling";
import { canDelete, canResume, canRunAgain, isPlan, runAgain } from "@/lib/task-actions";

/**
 * Refresh cadence follows what the task is doing. Terminal tasks are not polled at all. A task that is
 * only waiting (queued, retrying, awaiting approval) cannot change on its own faster than the worker
 * picks it up, so it is polled slowly: every request is billed API Gateway + Lambda + DB time.
 */
const BUSY = new Set(["RUNNING", "VERIFYING"]);
const summaryEvery = (status?: string) => (status && BUSY.has(status) ? 5000 : 20000);
const detailEvery = (status?: string) => (status && BUSY.has(status) ? 15000 : 40000);

const hasJSON = (v: unknown) => v != null && !(typeof v === "object" && Object.keys(v as object).length === 0);

function Json({ value }: { value: unknown }) {
  return (
    <pre className="max-h-80 overflow-auto rounded-lg border bg-muted/50 p-3 font-mono text-xs leading-relaxed">
      {typeof value === "string" ? value : JSON.stringify(value, null, 2)}
    </pre>
  );
}

export default function TaskDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const [task, setTask] = useState<Task | null>(null);
  const [attempts, setAttempts] = useState<TaskAttempt[]>([]);
  const [events, setEvents] = useState<TaskEvent[]>([]);
  const [files, setFiles] = useState<Artifact[]>([]);
  const [children, setChildren] = useState<Task[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [cancelling, setCancelling] = useState(false);

  const [tab, setTab] = useState("overview");

  const terminal = task !== null && isDone(task.status);
  const status = task?.status;

  // Three bounded loops, each created once per (task id, active/terminal) and never restarted by
  // the data they fetch. Each stops by itself when the task reaches a terminal state.

  // 1. Summary: the task and its subtasks.
  const { refresh: refreshSummary } = usePolling(
    async (signal) => {
      const t = await api.getTask(id, { signal });
      // Only a plan has subtasks; skip the second request for everything else.
      const kids = isPlan(t) ? await api.listTasks({ parent_id: id, limit: 200 }, { signal }) : [];
      setTask(t);
      setChildren(kids);
      setError(null);
    },
    {
      intervalMs: summaryEvery(status),
      enabled: !terminal,
      onError: (err) => setError(err instanceof Error ? err.message : "Could not load this task."),
    },
  );

  // 2. Attempts and events (the "Now" card and Activity/Attempts tabs): slower while active.
  const { refresh: refreshDetail } = usePolling(
    async (signal) => {
      // Apply each result independently: one failing endpoint must not hide the other's data.
      const [a, e] = await Promise.allSettled([api.getTaskAttempts(id, { signal }), api.getTaskEvents(id, { signal })]);
      if (a.status === "fulfilled") setAttempts(a.value);
      if (e.status === "fulfilled") setEvents(e.value);
      if (a.status === "rejected" && e.status === "rejected") throw a.reason; // let the poller back off
    },
    { intervalMs: detailEvery(status), enabled: !terminal },
  );
  useEffect(() => {
    if (status) refreshDetail(); // a status change is the moment new events/attempts exist
  }, [status, refreshDetail]);

  // 3. Files: only while that tab is open on an active task.
  usePolling(
    async (signal) => {
      setFiles(await api.listArtifacts(id, { signal }));
    },
    { intervalMs: detailEvery(status), enabled: tab === "files" && !terminal },
  );

  // Once the task is terminal nothing polls; fetch the final attempts, events and files exactly once.
  useEffect(() => {
    if (!terminal) return;
    const c = new AbortController();
    Promise.allSettled([
      api.getTaskAttempts(id, { signal: c.signal }),
      api.getTaskEvents(id, { signal: c.signal }),
      api.listArtifacts(id, { signal: c.signal }),
    ]).then(([a, e, f]) => {
      // Each result stands alone: a failure in one must never blank the others.
      if (a.status === "fulfilled") setAttempts(a.value);
      if (e.status === "fulfilled") setEvents(e.value);
      if (f.status === "fulfilled") setFiles(f.value);
    });
    return () => c.abort();
  }, [id, terminal]);

  const cancel = async () => {
    setCancelling(true);
    try {
      await api.cancelTask(id);
      toast.success("Task cancelled");
      setCancelOpen(false);
      refreshSummary();
      refreshDetail();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not cancel the task.");
    } finally {
      setCancelling(false);
    }
  };

  const router = useRouter();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [acting, setActing] = useState(false);
  const act = async (fn: () => Promise<void>, fail: string) => {
    setActing(true);
    try {
      await fn();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : fail);
    } finally {
      setActing(false);
    }
  };
  const resume = () =>
    act(async () => {
      await api.resumeTask(id);
      toast.success("Task resumed");
      refreshSummary();
      refreshDetail();
    }, "Could not resume the task.");
  const again = (t: Task) =>
    act(async () => {
      const created = await runAgain(t);
      toast.success("Started again");
      router.push(created ? `/tasks/${created.id}` : "/tasks");
    }, "Could not start it again.");
  const remove = () =>
    act(async () => {
      await api.deleteTask(id);
      toast.success("Task deleted");
      router.replace("/tasks");
    }, "Could not delete the task.");

  const download = async (a: Artifact) => {
    try {
      const { url } = await api.getPresignedUrl(a.id);
      window.open(url, "_blank", "noopener");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not create a download link.");
    }
  };

  if (error && !task) {
    return (
      <PageContainer>
        <ErrorNote message={error} />
        <Button asChild variant="outline" className="mt-4">
          <Link href="/tasks">Back to tasks</Link>
        </Button>
      </PageContainer>
    );
  }
  if (!task) {
    return (
      <PageContainer className="space-y-4">
        <Skeleton className="h-6 w-20" />
        <Skeleton className="h-10 w-3/4" />
        <Skeleton className="h-40 w-full rounded-xl" />
      </PageContainer>
    );
  }

  const now = deriveNow(task, events, attempts, children);

  return (
    <PageContainer>
      <Link href="/tasks" className="-ml-1 mb-3 inline-flex min-h-10 items-center gap-1 text-sm text-muted-foreground">
        <ChevronLeft className="size-4" /> Tasks
      </Link>

      <header className="mb-5 space-y-3">
        <h1 className="text-xl font-semibold leading-snug tracking-tight md:text-2xl">{task.title || task.goal}</h1>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <StatusBadge status={task.status} />
          <PriorityBadge priority={task.priority} />
          <span className="text-xs text-muted-foreground">
            {task.manager_domain} · started {timeAgo(task.started_at ?? task.created_at)}
            {task.retry_count > 0 ? ` · retry ${task.retry_count}/${task.max_retries}` : ""}
          </span>
        </div>
        <div className="flex flex-wrap gap-2">
          {!isDone(task.status) && (
            <Button variant="outline" className="h-11 text-destructive" onClick={() => setCancelOpen(true)}>
              Cancel task
            </Button>
          )}
          {canResume(task) && (
            <Button className="h-11" disabled={acting} onClick={resume}>
              <Play /> Resume
            </Button>
          )}
          {canRunAgain(task) && (
            <Button variant="outline" className="h-11" disabled={acting} onClick={() => again(task)}>
              <RotateCw /> Run again
            </Button>
          )}
          {canDelete(task) && (
            <Button variant="outline" className="h-11 text-destructive" disabled={acting} onClick={() => setDeleteOpen(true)}>
              <Trash2 /> Delete
            </Button>
          )}
        </div>
      </header>

      <section data-testid="task-now" aria-label="Now" className="mb-5 rounded-xl border bg-card p-4">
        <h2 className="text-xs font-medium text-muted-foreground">Now</h2>
        <p className="mt-1 text-base font-medium">{now.stage}</p>
        <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
          <div>
            <dt className="text-xs text-muted-foreground">Last activity</dt>
            <dd>{timeAgo(now.lastActivityAt)}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">Manager</dt>
            <dd className="break-words">{now.manager}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">Worker</dt>
            <dd className="break-words">{now.worker ?? "Not assigned yet"}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">Retries</dt>
            <dd className="tabular-nums">{now.retry ? `${now.retry.count} of ${now.retry.max}` : `None (up to ${task.max_retries})`}</dd>
          </div>
        </dl>
        {now.failureReason && (
          <p className="mt-3 break-words rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">{now.failureReason}</p>
        )}
      </section>

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList className="grid h-auto group-data-[orientation=horizontal]/tabs:h-auto w-full grid-cols-5 gap-0.5 bg-muted/60 p-1">
          {(
            [
              ["overview", "Overview", 0],
              ["activity", "Activity", events.length],
              ["attempts", "Attempts", attempts.length],
              ["files", "Files", files.length],
              ["subtasks", "Subtasks", children.length],
            ] as const
          ).map(([v, label, n]) => (
            <TabsTrigger
              key={v}
              value={v}
              className="h-11 min-w-0 flex-col gap-0 rounded-lg px-0.5 text-[11px] leading-tight sm:flex-row sm:gap-1.5 sm:px-3 sm:text-sm data-[state=active]:bg-background data-[state=active]:shadow-sm"
            >
              <span className="truncate">{label}</span>
              {n > 0 && <span className="text-[10px] tabular-nums text-muted-foreground sm:text-xs">{n}</span>}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="overview" className="mt-4 space-y-5">
          <section>
            <h2 className="mb-1.5 text-sm font-semibold">Goal</h2>
            <p className="whitespace-pre-wrap text-sm leading-relaxed text-muted-foreground">{task.goal}</p>
          </section>
          {task.failure_reason && <ErrorNote message={task.failure_reason} />}
          {hasJSON(task.output_payload) && (
            <section>
              <h2 className="mb-1.5 text-sm font-semibold">Result</h2>
              <Json value={task.output_payload} />
            </section>
          )}
          {hasJSON(task.input_payload) && (
            <section>
              <h2 className="mb-1.5 text-sm font-semibold">Input</h2>
              <Json value={task.input_payload} />
            </section>
          )}
        </TabsContent>

        <TabsContent value="activity" className="mt-4">
          {events.length === 0 ? (
            <EmptyState icon={<FileText />} title="No activity yet" description="Status changes and tool calls appear here." />
          ) : (
            <ol className="divide-y rounded-xl border bg-card px-4">
              {events.map((e) => (
                <li key={e.id} className="flex gap-3 py-3">
                  <span className="mt-1.5 size-2 shrink-0 rounded-full bg-primary" aria-hidden />
                  <div className="min-w-0">
                    <p className="text-sm">{e.message}</p>
                    <p className="text-xs text-muted-foreground">
                      {e.event_type.replaceAll("_", " ").toLowerCase()} · {timeAgo(e.created_at)}
                    </p>
                  </div>
                </li>
              ))}
            </ol>
          )}
        </TabsContent>

        <TabsContent value="attempts" className="mt-4">
          {attempts.length === 0 ? (
            <EmptyState icon={<Loader2 />} title="No attempts yet" description="A worker records each run here." />
          ) : (
            <ul className="divide-y rounded-xl border bg-card">
              {attempts.map((a) => (
                <li key={a.id} className="space-y-1 px-4 py-3">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-sm font-medium">Attempt {a.attempt_number}</span>
                    <StatusBadge status={a.status} />
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {(a.duration_ms / 1000).toFixed(1)}s · {(a.prompt_tokens + a.completion_tokens).toLocaleString()} tokens · {a.worker_id}
                  </p>
                  {a.error_message && <p className="text-xs text-destructive">{a.error_message}</p>}
                </li>
              ))}
            </ul>
          )}
        </TabsContent>

        <TabsContent value="files" className="mt-4">
          {files.length === 0 ? (
            <EmptyState icon={<FileText />} title="No files" description="Anything this task produces is saved here." />
          ) : (
            <ul className="divide-y rounded-xl border bg-card">
              {files.map((f) => (
                <li key={f.id} className="flex items-center gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">{f.title}</p>
                    <p className="text-xs text-muted-foreground">
                      {f.artifact_type.toLowerCase()} · {formatBytes(f.file_size_bytes)}
                    </p>
                  </div>
                  <Button variant="outline" size="icon" className="size-10" aria-label={`Download ${f.title}`} onClick={() => download(f)}>
                    <Download />
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </TabsContent>

        <TabsContent value="subtasks" className="mt-4">
          {children.length === 0 ? (
            <EmptyState icon={<FileText />} title="No subtasks" description="When JJ splits this work up, the steps show here." />
          ) : (
            <div className="divide-y overflow-hidden rounded-xl border bg-card">
              {children.map((c) => (
                <TaskRow key={c.id} task={c} />
              ))}
            </div>
          )}
        </TabsContent>
      </Tabs>

      <ResponsiveDialog open={cancelOpen} onOpenChange={setCancelOpen} title="Cancel this task?" description="Work in progress stops.">
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button variant="outline" className="h-11" onClick={() => setCancelOpen(false)}>
            Keep running
          </Button>
          <Button variant="destructive" className="h-11" onClick={cancel} disabled={cancelling}>
            {cancelling && <Loader2 className="animate-spin" />}
            Cancel task
          </Button>
        </div>
      </ResponsiveDialog>

      <ResponsiveDialog
        open={deleteOpen}
        onOpenChange={(o) => !acting && setDeleteOpen(o)}
        title="Delete this task?"
        description="This permanently removes the task with its subtasks, activity and attempts. Files already saved stay in storage."
      >
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button variant="outline" className="h-11" disabled={acting} onClick={() => setDeleteOpen(false)}>
            Keep
          </Button>
          <Button variant="destructive" className="h-11" disabled={acting} onClick={remove}>
            {acting && <Loader2 className="animate-spin" />}
            Delete
          </Button>
        </div>
      </ResponsiveDialog>
    </PageContainer>
  );
}
