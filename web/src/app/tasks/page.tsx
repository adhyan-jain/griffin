"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { isFollowUp } from "@/lib/threads";
import { usePolling } from "@/lib/use-polling";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { Archive, BellRing, ChevronDown, ListChecks, Play, Plus, Search, Sparkles, Square, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { api, type Task } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ResponsiveDialog } from "@/components/responsive-dialog";
import { useFxActivity } from "@/lib/fx";
import { PERIODS, canCancel, canDelete, canResume, inPeriod, runAgain, runBulk, type Period } from "@/lib/task-actions";
import { ACTIVE_STATUSES, WAITING_STATUSES, StatusBadge, isDone } from "@/components/StatusBadge";
import { taskKind } from "@/lib/task-kinds";
import { EmptyState, ErrorNote, PageContainer, PageHeader } from "@/components/page";
import { NewTaskDialog } from "@/components/new-task-dialog";
import { TaskRow } from "@/components/task-row";

const FILTERS = [
  { value: "all", label: "All" },
  { value: "active", label: "Active" },
  { value: "approval", label: "Approval" },
  { value: "done", label: "Done" },
  { value: "archived", label: "Archived" },
] as const;
type Filter = (typeof FILTERS)[number]["value"];



const when = (iso: string) => {
  const d = new Date(iso);
  const day = d.toLocaleDateString("en-GB", { weekday: "short", day: "numeric", month: "short" });
  const time = d.toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit" }).toLowerCase();
  return `${day}, ${time}`;
};
const newest = (a: Task, b: Task) => Date.parse(b.created_at) - Date.parse(a.created_at);

const matches = (t: Task, f: Filter) =>
  f === "all" ||
  f === "archived" ||
  (f === "active" && (ACTIVE_STATUSES.includes(t.status) || WAITING_STATUSES.includes(t.status))) ||
  (f === "approval" && t.status === "AWAITING_APPROVAL") ||
  (f === "done" && isDone(t.status));

export default function TasksPage() {
  const router = useRouter();
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [archivedTasks, setArchivedTasks] = useState<Task[] | null>(null);
  const [confirmClear, setConfirmClear] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<Filter>("all");
  const [query, setQuery] = useState("");
  const [newOpen, setNewOpen] = useState(false);
  const [period, setPeriod] = useState<Period>("all");
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [pendingDelete, setPendingDelete] = useState<Task[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [showSystem, setShowSystem] = useState(false);
  const [routinesOpen, setRoutinesOpen] = useState(false);


  const { refresh } = usePolling(
    async (signal) => {
      setTasks(await api.listTasks({ limit: 200 }, { signal }));
      // The archive is only fetched while its tab is open.
      if (filter === "archived") setArchivedTasks(await api.listTasks({ archived: true, limit: 200 }, { signal }));
      setError(null);
    },
    {
      intervalMs: 10000,
      onError: (e) => {
        setError(e instanceof Error ? e.message : "Could not load tasks.");
        setTasks((prev) => prev ?? []);
        setArchivedTasks((prev) => prev ?? []);
      },
    },
  );
  useEffect(() => {
    if (filter === "archived") refresh();
  }, [filter, refresh]);

  useFxActivity(tasks ? tasks.filter((t) => ACTIVE_STATUSES.includes(t.status)).length : null);

  const showingArchive = filter === "archived";
  const loaded = showingArchive ? archivedTasks : tasks;
  const { rows, subtaskCount, systemCount } = useMemo(() => {
    const all = (showingArchive ? archivedTasks : tasks) ?? [];
    const counts = new Map<string, number>();
    all.forEach((t) => t.parent_id && counts.set(t.parent_id, (counts.get(t.parent_id) ?? 0) + 1));
    const q = query.trim().toLowerCase();
    const roots = all.filter((t) => !t.parent_id).filter((t) => !isFollowUp(t)); // continued tasks live inside their thread
    const systemCount = roots.filter((t) => taskKind(t) === "system").length;
    const rows = roots
      .filter((t) => {
        const k = taskKind(t);
        return k === "request" || (showSystem && k === "system");
      })
      .sort(newest)
      .filter((t) => matches(t, filter))
      .filter((t) => filter !== "done" || inPeriod(t, period))
      .filter((t) => !q || `${t.title} ${t.goal} ${t.manager_domain} ${t.id}`.toLowerCase().includes(q));
    return { rows, subtaskCount: counts, systemCount };
  }, [tasks, archivedTasks, showingArchive, filter, query, period, showSystem]);

  const reminders = useMemo(
    () =>
      (tasks ?? [])
        .filter((t) => taskKind(t) === "reminder" && !isDone(t.status) && t.scheduled_for)
        .sort((a, b) => Date.parse(a.scheduled_for!) - Date.parse(b.scheduled_for!)),
    [tasks],
  );
  // One row per routine: its latest run, plus the next queued run's time.
  const routines = useMemo(() => {
    const byTitle = new Map<string, { last: Task; next: string | null }>();
    for (const t of (tasks ?? []).filter((x) => !x.parent_id && taskKind(x) === "routine").sort(newest)) {
      const g = byTitle.get(t.title) ?? { last: t, next: null };
      if (!g.next && t.scheduled_for && !isDone(t.status) && Date.parse(t.scheduled_for) > Date.now()) g.next = t.scheduled_for;
      byTitle.set(t.title, g);
    }
    return [...byTitle.values()];
  }, [tasks]);

  const hasAny = (loaded ?? []).some((t) => taskKind(t) === "request");
  const chosen = rows.filter((t) => selected.has(t.id));
  const exitSelect = () => {
    setSelecting(false);
    setSelected(new Set());
  };
  const toggle = (t: Task) =>
    setSelected((prev) => {
      const n = new Set(prev);
      if (!n.delete(t.id)) n.add(t.id);
      return n;
    });
  const report = (verb: string, o: Awaited<ReturnType<typeof runBulk>>) => {
    if (o.ok) toast.success(`${verb} ${o.ok} task${o.ok === 1 ? "" : "s"}`);
    if (o.failed) toast.error(`${o.failed} failed: ${o.firstError}`);
    else if (!o.ok && o.skipped) toast.info("Nothing to do for the selected tasks.");
  };

  const doResume = async (list: Task[]) => {
    setBusy(true);
    const o = await runBulk(list, canResume, (t) => api.resumeTask(t.id));
    report("Resumed", o);
    setBusy(false);
    exitSelect();
    refresh();
  };
  // One submit per task at a time: submitting a plan takes a few seconds, and a second tap while it is
  // pending used to queue a second paid run (the 2026-11-06 duplicate plans).
  const starting = useRef(new Set<string>());
  const doCancel = async (list: Task[]) => {
    setBusy(true);
    const o = await runBulk(list, canCancel, (t) => api.cancelTask(t.id));
    report("Cancelled", o);
    setBusy(false);
    exitSelect();
    refresh();
  };
  const doRunAgain = async (t: Task) => {
    if (starting.current.has(t.id)) return;
    starting.current.add(t.id);
    const toastId = toast.loading("Starting again...");
    try {
      const created = await runAgain(t);
      toast.success("Started again", { id: toastId });
      refresh();
      if (created) router.push(`/tasks/${created.id}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not start it again.", { id: toastId });
    } finally {
      starting.current.delete(t.id);
    }
  };
  const doDelete = async () => {
    if (!pendingDelete) return;
    setBusy(true);
    const o = await runBulk(pendingDelete, canDelete, (t) => api.deleteTask(t.id));
    report("Deleted", o);
    setBusy(false);
    setPendingDelete(null);
    exitSelect();
    refresh();
  };
  const doArchive = async (t: Task) => {
    try {
      await api.archiveTask(t.id);
      toast.success("Archived");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not archive it.");
    }
    refresh();
  };
  const doUnarchive = async (t: Task) => {
    try {
      await api.unarchiveTask(t.id);
      toast.success("Moved back to your tasks");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not restore it.");
    }
    refresh();
  };
  const doClearArchive = async () => {
    setBusy(true);
    try {
      const { deleted } = await api.deleteArchivedTasks();
      toast.success(`Deleted ${deleted} archived task${deleted === 1 ? "" : "s"}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not delete the archive.");
    }
    setBusy(false);
    setConfirmClear(false);
    refresh();
  };
  const rowActions = {
    onResume: (t: Task) => doResume([t]),
    onRunAgain: doRunAgain,
    onDelete: (t: Task) => setPendingDelete([t]),
    onArchive: doArchive,
    onUnarchive: doUnarchive,
  };

  return (
    <PageContainer className={selecting ? "pb-28" : undefined}>
      <PageHeader
        title="Tasks"
        description="Everything JJ and your team are working on."
        actions={
          <Button className="h-10" onClick={() => setNewOpen(true)}>
            <Plus /> New
          </Button>
        }
      />

      <div className="space-y-3">
        <Tabs value={filter} onValueChange={(v) => setFilter(v as Filter)}>
          <div className="flex w-full items-center gap-1.5 overflow-x-auto no-scrollbar py-0.5">
            <TabsList className="inline-flex h-auto w-auto gap-1 bg-muted/60 p-1 rounded-xl">
              {FILTERS.map((f) => (
                <TabsTrigger
                  key={f.value}
                  value={f.value}
                  className="shrink-0 h-9 rounded-lg px-3.5 text-xs sm:text-sm font-medium data-[state=active]:bg-background data-[state=active]:shadow-sm"
                >
                  {f.label}
                </TabsTrigger>
              ))}
            </TabsList>
          </div>
        </Tabs>

        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            aria-label="Search tasks"
            placeholder="Search tasks"
            className="h-11 pl-9"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>

        <div className="flex min-h-11 items-center justify-between gap-2">
          {filter === "done" ? (
            <Select value={period} onValueChange={(v) => setPeriod(v as Period)}>
              <SelectTrigger aria-label="Finished within" className="h-11 w-44">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PERIODS.map((p) => (
                  <SelectItem key={p.value} value={p.value}>
                    {p.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <span className="text-xs text-muted-foreground tabular-nums">{rows.length} task{rows.length === 1 ? "" : "s"}</span>
          )}
          {showingArchive && rows.length > 0 && !selecting && (
            <Button variant="outline" className="h-11 text-destructive" disabled={busy} onClick={() => setConfirmClear(true)}>
              <Trash2 /> Delete all archived
            </Button>
          )}
          {rows.length > 0 &&
            (selecting ? (
              <div className="flex items-center gap-1">
                <Button variant="ghost" className="h-11" onClick={() => setSelected(new Set(rows.map((t) => t.id)))}>
                  Select all
                </Button>
                <Button variant="ghost" size="icon" className="size-11" aria-label="Exit selection" onClick={exitSelect}>
                  <X />
                </Button>
              </div>
            ) : (
              <Button variant="ghost" className="h-11" onClick={() => setSelecting(true)}>
                Select
              </Button>
            ))}
        </div>

        {error && <ErrorNote message={error} />}

        {!showingArchive && reminders.length > 0 && (
          <section aria-label="Upcoming reminders" className="rounded-xl border bg-card p-3">
            <h2 className="mb-1.5 flex items-center gap-2 text-sm font-semibold">
              <BellRing className="size-4 text-muted-foreground" aria-hidden /> Upcoming reminders
            </h2>
            <ul className="space-y-1 text-sm">
              {reminders.map((t) => (
                <li key={t.id}>
                  <Link href={`/tasks/${t.id}`} className="block rounded py-1 hover:underline">
                    {t.title.replace(/^REMINDER - /i, "")} <span className="text-muted-foreground">— {when(t.scheduled_for!)}</span>
                  </Link>
                </li>
              ))}
            </ul>
          </section>
        )}

        {loaded === null ? (
          <div className="space-y-2">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-16 w-full rounded-xl" />
            ))}
          </div>
        ) : rows.length === 0 ? (
          <EmptyState
            icon={showingArchive ? <Archive /> : <ListChecks />}
            title={showingArchive && !hasAny ? "Nothing archived" : hasAny ? "Nothing matches" : "No tasks yet"}
            description={
              showingArchive && !hasAny
                ? "Finished tasks you archive, and old minor ones, are kept here until you delete them."
                : hasAny
                ? filter === "done" && period !== "all"
                  ? "Nothing finished in that window. Try a longer one."
                  : "Try another filter or search."
                : "Nothing yet. Ask JJ for something."
            }
            action={
              !showingArchive && !hasAny && (
                <Button asChild>
                  <Link href="/chat">Ask JJ</Link>
                </Button>
              )
            }
          />
        ) : (
          <div className="divide-y overflow-hidden rounded-xl border bg-card">
            {rows.map((t) => (
              <TaskRow
                key={t.id}
                task={t}
                subtasks={subtaskCount.get(t.id) ?? 0}
                actions={rowActions}
                archived={showingArchive}
                selecting={selecting}
                selected={selected.has(t.id)}
                onToggle={toggle}
              />
            ))}
          </div>
        )}

        {!showingArchive && routines.length > 0 && (
          <section aria-label="Routines">
            <button
              type="button"
              className="flex min-h-11 w-full items-center justify-between rounded-lg px-1 text-sm font-medium text-muted-foreground"
              aria-expanded={routinesOpen}
              onClick={() => setRoutinesOpen((o) => !o)}
            >
              Routines ({routines.length})
              <ChevronDown className={routinesOpen ? "size-4 rotate-180" : "size-4"} aria-hidden />
            </button>
            {routinesOpen && (
              <ul className="divide-y overflow-hidden rounded-xl border bg-card">
                {routines.map(({ last, next }) => (
                  <li key={last.id}>
                    <Link href={`/tasks/${last.id}`} className="flex min-h-12 items-center gap-3 px-3 py-2 text-sm hover:bg-muted/50">
                      <span className="min-w-0 flex-1 truncate">{last.title}</span>
                      <StatusBadge status={last.status} />
                      <span className="hidden shrink-0 text-xs text-muted-foreground sm:inline">{next ? `Next ${when(next)}` : "Not scheduled"}</span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </section>
        )}

        {!showingArchive && systemCount > 0 && (
          <Button variant="ghost" className="h-10 text-xs text-muted-foreground" onClick={() => setShowSystem((v) => !v)}>
            {showSystem ? "Hide" : "Show"} system tasks ({systemCount})
          </Button>
        )}
      </div>

      {selecting && (
        <div className="fixed inset-x-0 bottom-[calc(4.5rem+env(safe-area-inset-bottom))] z-40 border-t bg-background/95 px-4 py-2.5 backdrop-blur md:bottom-0 md:left-60">
          <div className="mx-auto flex max-w-3xl flex-col gap-2 sm:flex-row sm:items-center">
            <span className="text-center text-xs text-muted-foreground tabular-nums sm:mr-auto sm:text-left sm:text-sm sm:text-foreground">{chosen.length} selected</span>
            <div className="grid grid-cols-3 gap-2 sm:flex">
              <Button variant="outline" className="h-11 px-2" disabled={busy || !chosen.some(canCancel)} onClick={() => doCancel(chosen)}>
                <Square /> Cancel
              </Button>
              <Button variant="outline" className="h-11 px-2" disabled={busy || !chosen.some(canResume)} onClick={() => doResume(chosen)}>
                <Play /> Resume
              </Button>
              <Button variant="destructive" className="h-11 px-2" disabled={busy || !chosen.some(canDelete)} onClick={() => setPendingDelete(chosen)}>
                <Trash2 /> Delete
              </Button>
            </div>
          </div>
        </div>
      )}
      {/* Room under the list so the last rows are never hidden behind the selection bar. */}
      {selecting && <div aria-hidden className="h-32 md:h-24" />}

      <ResponsiveDialog
        open={pendingDelete !== null}
        onOpenChange={(o) => !o && !busy && setPendingDelete(null)}
        title={pendingDelete && pendingDelete.length > 1 ? `Delete ${pendingDelete.length} tasks?` : "Delete this task?"}
        description="This permanently removes the task with its subtasks, activity and attempts. Files already saved stay in storage."
      >
        {pendingDelete && pendingDelete.some((t) => !canDelete(t)) && (
          <p className="mb-3 text-sm text-muted-foreground">Tasks that are still running are skipped. Cancel them first.</p>
        )}
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button variant="outline" className="h-11" disabled={busy} onClick={() => setPendingDelete(null)}>
            Keep
          </Button>
          <Button variant="destructive" className="h-11" disabled={busy} onClick={doDelete}>
            {busy ? "Deleting..." : "Delete"}
          </Button>
        </div>
      </ResponsiveDialog>

      <ResponsiveDialog
        open={confirmClear}
        onOpenChange={(o) => !o && !busy && setConfirmClear(false)}
        title="Delete all archived tasks?"
        description="This permanently removes every archived task with its subtasks, activity and attempts. Files already saved stay in storage. It cannot be undone."
      >
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button variant="outline" className="h-11" disabled={busy} onClick={() => setConfirmClear(false)}>
            Keep them
          </Button>
          <Button variant="destructive" className="h-11" disabled={busy} onClick={doClearArchive}>
            {busy ? "Deleting..." : "Delete all"}
          </Button>
        </div>
      </ResponsiveDialog>

      <NewTaskDialog open={newOpen} onOpenChange={setNewOpen} onCreated={(t) => router.push(`/tasks/${t.id}`)} />
    </PageContainer>
  );
}
