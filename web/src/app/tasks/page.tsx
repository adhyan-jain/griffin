"use client";

import { useMemo, useRef, useState } from "react";
import { isFollowUp } from "@/lib/threads";
import { usePolling } from "@/lib/use-polling";
import { useRouter } from "next/navigation";
import { Clock, ListChecks, Play, Plus, RefreshCw, Search, ShieldAlert, Sparkles, Square, Trash2, X } from "lucide-react";
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
import { ACTIVE_STATUSES, WAITING_STATUSES, isDone } from "@/components/StatusBadge";
import { EmptyState, ErrorNote, PageContainer, PageHeader } from "@/components/page";
import { NewTaskDialog } from "@/components/new-task-dialog";
import { TaskRow } from "@/components/task-row";

const FILTERS = [
  { value: "all", label: "All" },
  { value: "active", label: "Active" },
  { value: "approval", label: "Approval" },
  { value: "done", label: "Done" },
  { value: "pipelines", label: "Pipelines" },
] as const;
type Filter = (typeof FILTERS)[number]["value"];

interface PipelineInfo {
  id: string;
  number: number;
  name: string;
  schedule: string;
  frequency: string;
  tierOrCap: string;
  description: string;
  groundingRule: string;
  command: string;
}

const PIPELINES: PipelineInfo[] = [
  {
    id: "internships",
    number: 1,
    name: "Internship Tracker",
    schedule: "Daily 08:00 IST",
    frequency: "Daily",
    tierOrCap: "Tier A (≥ ₹30k/mo) / Tier B (₹15k-30k) / Tier C",
    description: "Monitors Unstop, Internshala, Interndock, Wellfound, LinkedIn Jobs, and targets.json for Summer 2027.",
    groundingRule: "Alerts on Tier A and <72h deadline. Syncs open_tracker.json & tracker.md.",
    command: "internships",
  },
  {
    id: "tailor",
    number: 2,
    name: "Application + Resume Tailoring",
    schedule: "Triggered after Task 1",
    frequency: "On Discovery",
    tierOrCap: "Daily Cap: 8 Applications (Weekdays 09:30-19:00 IST)",
    description: "Selects base resume, invokes Claude CLI exclusively with AJ_Resume_Context.md to tailor 1-page PDF.",
    groundingRule: "Claude CLI only. Approval card with bullet diff, 90-word sourced cover note, screening Q&A.",
    command: "internships",
  },
  {
    id: "linkedin",
    number: 3,
    name: "Alumni LinkedIn Outreach",
    schedule: "Weekdays 10:30 IST",
    frequency: "Daily (Paced)",
    tierOrCap: "Daily Cap: 10 Connection Notes (4-15 min random gap)",
    description: "Reaches out to VIT Vellore alumni (ACM-VIT first, then CSE/IT, 2019+ batches).",
    groundingRule: "Note ≤ 280 chars with verified source URL. 1 question. 1 follow-up draft after 4 days if no reply.",
    command: "linkedin",
  },
  {
    id: "email",
    number: 4,
    name: "Startup Cold Email Outreach",
    schedule: "Weekdays 10:45 IST",
    frequency: "Daily (Paced)",
    tierOrCap: "Daily Cap: 12 Cold Emails (4-15 min random gap)",
    description: "Founders & EMs at seed/Series A startups. Plain text ≤ 110 words, 3 lines, subject ≤ 6 words.",
    groundingRule: "Cites source URL + proof from LabTutor / EchoTales / ComputeHive. 1 follow-up after 5 days.",
    command: "email",
  },
  {
    id: "inbox",
    number: 5,
    name: "Inbox Monitor",
    schedule: "Continuous (Read-Only)",
    frequency: "Realtime",
    tierOrCap: "Read-only: adhyanjain2006@gmail.com",
    description: "Monitors for OAs, interviews, offers, and rejections. Validates SPF/DKIM against applied tracker.",
    groundingRule: "Never auto-replies, opens links, or downloads attachments. Instant OA alert + 24h & 3h reminders.",
    command: "inbox",
  },
  {
    id: "hackathons",
    number: 6,
    name: "Hackathon Tracker",
    schedule: "Daily 08:30 IST",
    frequency: "Daily",
    tierOrCap: "Devpost, Devfolio, Unstop",
    description: "Tracks online hackathons open to Indian students with cash prizes in AI/systems/dev-tools closing in 3+ days.",
    groundingRule: "Deduplicated alerts with prize amount and registration deadline.",
    command: "hackathons",
  },
  {
    id: "oss",
    number: 7,
    name: "Open-Source Watch",
    schedule: "Monday 09:00 IST",
    frequency: "Weekly (Monday)",
    tierOrCap: "Go & Python repos from targets.json",
    description: "Finds top 5 open, unassigned good-first-issues created in the last 30 days with no open PRs.",
    groundingRule: "Reports official 2027 program application dates for GSoC, LFX, and MLH Fellowship.",
    command: "oss",
  },
  {
    id: "dsa",
    number: 8,
    name: "DSA Coach",
    schedule: "Daily 07:30 & 21:30 IST",
    frequency: "Twice Daily",
    tierOrCap: "150 Curated Problems in Pattern Order",
    description: "07:30 assigns 2 problems. 21:30 verifies public LeetCode profile submissions, streak, and 150 progress.",
    groundingRule: "7-day rest on missed/failed problems. Never reveals solutions or spoilers.",
    command: "dsa-morning",
  },
  {
    id: "brief",
    number: 9,
    name: "Morning Brief",
    schedule: "Daily 07:45 IST",
    frequency: "Daily",
    tierOrCap: "Consolidated Notification",
    description: "Single comprehensive alert: deadlines within 72h, pending OAs, replies waiting, approvals, today's DSA.",
    groundingRule: "Incorporates weekly priority from plan/week.md.",
    command: "brief",
  },
  {
    id: "post",
    number: 10,
    name: "Weekly Post Draft",
    schedule: "Saturday 10:00 IST",
    frequency: "Weekly (Saturday)",
    tierOrCap: "Outlines Only",
    description: "Reads local git commits and merged PRs across repos to draft 3 post angles with measured metrics.",
    groundingRule: "Every angle must cite a source commit hash or merged PR. Phone approval required to schedule.",
    command: "post",
  },
  {
    id: "threads",
    number: 11,
    name: "Community Thread Alerts",
    schedule: "Daily 18:00 IST",
    frequency: "Daily",
    tierOrCap: "Max 3 Links / Day",
    description: "Searches r/LocalLLaMA, web novel subreddits, and HN for threads where Adhyan's work directly applies.",
    groundingRule: "Sends 1-line reason + link for manual review. Griffin never posts or replies automatically.",
    command: "threads",
  },
  {
    id: "hygiene",
    number: 12,
    name: "Repo Hygiene",
    schedule: "Monthly / On-Demand",
    frequency: "Monthly",
    tierOrCap: "Public Repositories",
    description: "Ensures 1-line descriptions, 3-5 topics, and PRs for README updates. Flags repos inactive >12 months.",
    groundingRule: "Never pushes directly to default branch (main/master). All changes as GitHub PRs.",
    command: "hygiene",
  },
  {
    id: "report",
    number: 13,
    name: "Sunday Report",
    schedule: "Sunday 19:00 IST",
    frequency: "Weekly (Sunday)",
    tierOrCap: "Full Weekly Audit Scorecard",
    description: "Weekly metrics: sent, replied, interviews, offers, template reply rates, DSA solved, stop rules hit.",
    groundingRule: "Dual-saved to /home/adhyan/Documents/Griffin/reports/ and S3.",
    command: "report",
  },
];

const matches = (t: Task, f: Filter) =>
  f === "all" ||
  (f === "active" && (ACTIVE_STATUSES.includes(t.status) || WAITING_STATUSES.includes(t.status))) ||
  (f === "approval" && t.status === "AWAITING_APPROVAL") ||
  (f === "done" && isDone(t.status));

export default function TasksPage() {
  const router = useRouter();
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<Filter>("all");
  const [query, setQuery] = useState("");
  const [newOpen, setNewOpen] = useState(false);
  const [period, setPeriod] = useState<Period>("all");
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [pendingDelete, setPendingDelete] = useState<Task[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [triggeringId, setTriggeringId] = useState<string | null>(null);

  const triggerPipeline = async (p: PipelineInfo) => {
    setTriggeringId(p.id);
    try {
      await api.createTask({
        title: `[Pipeline #${p.number}] ${p.name}`,
        goal: `Execute pipeline #${p.number} (${p.name}) adhering to Rule 0 safety constraints.`,
        manager_domain: "AUTOMATION",
        priority: "HIGH_P1",
      });
      toast.success(`Pipeline #${p.number} (${p.name}) scheduled! Check task runner.`);
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to trigger pipeline");
    } finally {
      setTriggeringId(null);
    }
  };

  const filteredPipelines = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return PIPELINES;
    return PIPELINES.filter(
      (p) =>
        p.name.toLowerCase().includes(q) ||
        p.description.toLowerCase().includes(q) ||
        p.schedule.toLowerCase().includes(q) ||
        p.groundingRule.toLowerCase().includes(q),
    );
  }, [query]);

  const { refresh } = usePolling(
    async (signal) => {
      setTasks(await api.listTasks({ limit: 200 }, { signal }));
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

  useFxActivity(tasks ? tasks.filter((t) => ACTIVE_STATUSES.includes(t.status)).length : null);

  const { rows, subtaskCount } = useMemo(() => {
    const all = tasks ?? [];
    const counts = new Map<string, number>();
    all.forEach((t) => t.parent_id && counts.set(t.parent_id, (counts.get(t.parent_id) ?? 0) + 1));
    const q = query.trim().toLowerCase();
    const rows = all
      .filter((t) => !t.parent_id)
      .filter((t) => !isFollowUp(t)) // continued tasks live inside the thread of the task they continue
      .filter((t) => matches(t, filter))
      .filter((t) => filter !== "done" || inPeriod(t, period))
      .filter((t) => !q || `${t.title} ${t.goal} ${t.manager_domain} ${t.id}`.toLowerCase().includes(q));
    return { rows, subtaskCount: counts };
  }, [tasks, filter, query, period]);

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
  const rowActions = { onResume: (t: Task) => doResume([t]), onRunAgain: doRunAgain, onDelete: (t: Task) => setPendingDelete([t]) };

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
          <TabsList className="grid h-auto group-data-[orientation=horizontal]/tabs:h-auto w-full grid-cols-5 gap-1 bg-muted/60 p-1">
            {FILTERS.map((f) => (
              <TabsTrigger
                key={f.value}
                value={f.value}
                className="h-10 rounded-lg px-1 text-[13px] sm:text-sm data-[state=active]:bg-background data-[state=active]:shadow-sm"
              >
                {f.label}
              </TabsTrigger>
            ))}
          </TabsList>
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
          {filter === "pipelines" ? (
            <span className="text-xs text-muted-foreground tabular-nums">
              {filteredPipelines.length} automated pipeline{filteredPipelines.length === 1 ? "" : "s"}
            </span>
          ) : filter === "done" ? (
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
          {filter !== "pipelines" && rows.length > 0 &&
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

        {filter === "pipelines" ? (
          <div className="space-y-4">
            {/* Safety Rules Banner */}
            <div className="rounded-xl border border-primary/20 bg-primary/5 p-4 text-xs space-y-2">
              <div className="flex items-center gap-2 font-semibold text-primary">
                <ShieldAlert className="size-4" />
                0. GLOBAL RULES & Safety Engine Constraints
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 text-muted-foreground">
                <div>• <strong className="text-foreground">Operating Window:</strong> Mon-Fri 09:30-19:00 IST (random 4-15m gaps)</div>
                <div>• <strong className="text-foreground">Daily Caps:</strong> 10 LinkedIn · 12 Cold Emails · 8 Applications</div>
                <div>• <strong className="text-foreground">Circuit Breaker:</strong> 24h halt on CAPTCHA, 2FA, or rate limits</div>
                <div>• <strong className="text-foreground">Strict Grounding:</strong> Strictly verified against AJ_Resume_Context.md</div>
                <div className="sm:col-span-2">• <strong className="text-foreground">Dual-Copy Mirror:</strong> Local ~/Documents/Griffin/ & AWS S3</div>
              </div>
            </div>

            {/* Pipeline Cards Grid */}
            <div className="grid grid-cols-1 gap-3">
              {filteredPipelines.map((p) => (
                <div key={p.id} className="rounded-xl border bg-card p-4 space-y-2 shadow-sm">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span className="flex size-6 items-center justify-center rounded-full bg-primary/10 text-primary font-mono text-xs font-semibold">
                        #{p.number}
                      </span>
                      <h4 className="text-sm font-semibold text-foreground">{p.name}</h4>
                    </div>
                    <div className="flex items-center gap-2">
                      <span className="inline-flex items-center gap-1 rounded-full border bg-muted/40 px-2 py-0.5 text-[11px] text-muted-foreground">
                        <Clock className="size-3" />
                        {p.schedule}
                      </span>
                      <Button
                        size="sm"
                        className="h-8 gap-1.5 text-xs font-medium"
                        disabled={triggeringId === p.id}
                        onClick={() => triggerPipeline(p)}
                      >
                        {triggeringId === p.id ? (
                          <RefreshCw className="size-3 animate-spin" />
                        ) : (
                          <Play className="size-3" />
                        )}
                        Run Now
                      </Button>
                    </div>
                  </div>

                  <p className="text-xs text-muted-foreground leading-relaxed">{p.description}</p>

                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted-foreground pt-1 border-t border-border/40">
                    <span>
                      <strong className="text-foreground font-medium">Constraints:</strong> {p.tierOrCap}
                    </span>
                    <span>
                      <strong className="text-foreground font-medium">Grounding:</strong> {p.groundingRule}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          </div>
        ) : tasks === null ? (
          <div className="space-y-2">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-16 w-full rounded-xl" />
            ))}
          </div>
        ) : rows.length === 0 ? (
          <EmptyState
            icon={<ListChecks />}
            title={tasks.length ? "Nothing matches" : "No tasks yet"}
            description={
              tasks.length
                ? filter === "done" && period !== "all"
                  ? "Nothing finished in that window. Try a longer one."
                  : "Try another filter or search."
                : "Create a task and it will show up here."
            }
            action={
              !tasks.length && (
                <Button onClick={() => setNewOpen(true)}>
                  <Plus /> New task
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
                selecting={selecting}
                selected={selected.has(t.id)}
                onToggle={toggle}
              />
            ))}
          </div>
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

      <NewTaskDialog open={newOpen} onOpenChange={setNewOpen} onCreated={(t) => router.push(`/tasks/${t.id}`)} />
    </PageContainer>
  );
}
