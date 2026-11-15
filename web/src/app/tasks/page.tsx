"use client";

import { useMemo, useRef, useState } from "react";
import { isFollowUp } from "@/lib/threads";
import { usePolling } from "@/lib/use-polling";
import { useRouter } from "next/navigation";
import { Clock, ListChecks, Play, Plus, RefreshCw, Search, ShieldAlert, Sparkles, Square, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { api, type Task, type TaskPriority } from "@/lib/api";
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
  { value: "operations", label: "Operations" },
] as const;
type Filter = (typeof FILTERS)[number]["value"];

interface OperationalTask {
  id: string;
  number: number;
  name: string;
  schedule: string;
  frequency: string;
  managerDomain: string;
  priority: TaskPriority;
  tierOrCap: string;
  description: string;
  goal: string;
}

const OPERATIONAL_TASKS: OperationalTask[] = [
  {
    id: "internships",
    number: 1,
    name: "Internship Tracker",
    schedule: "Daily 08:00 IST",
    frequency: "Daily",
    managerDomain: "CAREER",
    priority: "HIGH_P1",
    tierOrCap: "Tier A (≥ ₹30k/mo) / Tier B (₹15k-30k) / Tier C",
    description: "Monitors Unstop, Internshala, Interndock, Wellfound, LinkedIn Jobs, and targets.json for Summer 2027.",
    goal: "Scan Unstop, Internshala, Interndock, Wellfound, LinkedIn Jobs, and targets.json for Summer 2027 SWE/AI internships. Filter Tier A (≥ ₹30k/mo) and Tier B. Flag deadlines <72h.",
  },
  {
    id: "tailor",
    number: 2,
    name: "Application + Resume Tailoring",
    schedule: "Triggered after Discovery",
    frequency: "On Discovery",
    managerDomain: "CAREER",
    priority: "HIGH_P1",
    tierOrCap: "Daily Cap: 8 Applications (Weekdays 09:30-19:00 IST)",
    description: "Selects base resume, invokes Claude CLI exclusively with AJ_Resume_Context.md to tailor 1-page PDF.",
    goal: "Tailor single-page PDF resume using AJ_Resume_Context.md facts only. Prepare 90-word cover note and approval card with bullet diff.",
  },
  {
    id: "linkedin",
    number: 3,
    name: "Alumni LinkedIn Outreach",
    schedule: "Weekdays 10:30 IST",
    frequency: "Daily (Paced)",
    managerDomain: "OUTREACH",
    priority: "NORMAL_P2",
    tierOrCap: "Daily Cap: 10 Connection Notes (4-15 min random gap)",
    description: "Reaches out to VIT Vellore alumni (ACM-VIT first, then CSE/IT, 2019+ batches).",
    goal: "Search VIT Vellore alumni (ACM-VIT, CSE/IT, 2019+). Draft personalized connection notes ≤280 chars with verified source URL.",
  },
  {
    id: "email",
    number: 4,
    name: "Startup Cold Email Outreach",
    schedule: "Weekdays 10:45 IST",
    frequency: "Daily (Paced)",
    managerDomain: "OUTREACH",
    priority: "NORMAL_P2",
    tierOrCap: "Daily Cap: 12 Cold Emails (4-15 min random gap)",
    description: "Founders & EMs at seed/Series A startups. Plain text ≤ 110 words, 3 lines, subject ≤ 6 words.",
    goal: "Draft 3-line plain-text cold email ≤110 words with portfolio proof links to founders at seed/Series A startups.",
  },
  {
    id: "inbox",
    number: 5,
    name: "Application Inbox Monitor",
    schedule: "Continuous (Read-Only)",
    frequency: "Realtime",
    managerDomain: "PERSONAL_OPS",
    priority: "HIGH_P1",
    tierOrCap: "Read-only: adhyanjain2006@gmail.com",
    description: "Monitors for OAs, interviews, offers, and rejections. Validates SPF/DKIM against applied tracker.",
    goal: "Check inbox adhyanjain2006@gmail.com for OAs, interviews, offers, and rejections. Validate SPF/DKIM against applied tracker.",
  },
  {
    id: "hackathons",
    number: 6,
    name: "Hackathon Tracker",
    schedule: "Daily 08:30 IST",
    frequency: "Daily",
    managerDomain: "CAREER",
    priority: "NORMAL_P2",
    tierOrCap: "Devpost, Devfolio, Unstop",
    description: "Tracks online hackathons open to Indian students with cash prizes in AI/systems/dev-tools closing in 3+ days.",
    goal: "Inspect Devpost, Devfolio, and Unstop for online hackathons open to Indian students with cash prizes in AI/systems/dev-tools closing in 3+ days.",
  },
  {
    id: "oss",
    number: 7,
    name: "GitHub Opportunity Radar",
    schedule: "Monday 09:00 IST",
    frequency: "Weekly (Monday)",
    managerDomain: "ENGINEERING",
    priority: "NORMAL_P2",
    tierOrCap: "Go & Python repos from targets.json",
    description: "Finds top 5 open, unassigned good-first-issues created in the last 30 days with no open PRs.",
    goal: "Scan Go and Python repos in targets.json for top 5 unassigned good-first-issues. Verify 2027 application dates for GSoC, LFX, MLH.",
  },
  {
    id: "dsa",
    number: 8,
    name: "LeetCode & DSA Coach",
    schedule: "Daily 07:30 & 21:30 IST",
    frequency: "Twice Daily",
    managerDomain: "CAREER",
    priority: "HIGH_P1",
    tierOrCap: "150 Curated Problems (Profile: AdhyanJain)",
    description: "07:30 assigns 2 problems. 21:30 verifies public LeetCode profile submissions, streak, and 150 progress.",
    goal: "Assign 2 curated pattern problems for LeetCode user AdhyanJain. Sync solved problems from public profile, track 7-day streak, ensure zero duplicate problems.",
  },
  {
    id: "brief",
    number: 9,
    name: "Daily Executive Briefing",
    schedule: "Daily 07:45 IST",
    frequency: "Daily",
    managerDomain: "PERSONAL_OPS",
    priority: "HIGH_P1",
    tierOrCap: "Consolidated Notification",
    description: "Single comprehensive alert: deadlines within 72h, pending OAs, replies waiting, approvals, today's DSA.",
    goal: "Compile single comprehensive morning briefing: deadlines within 72h, pending OAs, replies waiting, approvals, and today's DSA problems.",
  },
  {
    id: "post",
    number: 10,
    name: "Weekly Engineering Post Draft",
    schedule: "Saturday 10:00 IST",
    frequency: "Weekly (Saturday)",
    managerDomain: "ENGINEERING",
    priority: "NORMAL_P2",
    tierOrCap: "Outlines Only",
    description: "Reads local git commits and merged PRs across repos to draft 3 post angles with measured metrics.",
    goal: "Review local git commits and merged PRs across repos to draft 3 technical post outlines with measured metrics.",
  },
  {
    id: "threads",
    number: 11,
    name: "Community Thread Alerts",
    schedule: "Daily 18:00 IST",
    frequency: "Daily",
    managerDomain: "RESEARCH",
    priority: "NORMAL_P2",
    tierOrCap: "Max 3 Links / Day",
    description: "Searches r/LocalLLaMA, web novel subreddits, and HN for threads where Adhyan's work directly applies.",
    goal: "Search r/LocalLLaMA, subreddits, and HN for threads where Adhyan's work applies. Provide 1-line reason + link for manual review.",
  },
  {
    id: "hygiene",
    number: 12,
    name: "Repo Hygiene & Maintenance",
    schedule: "Monthly / On-Demand",
    frequency: "Monthly",
    managerDomain: "ENGINEERING",
    priority: "NORMAL_P2",
    tierOrCap: "Public Repositories",
    description: "Ensures 1-line descriptions, 3-5 topics, and PRs for README updates. Flags repos inactive >12 months.",
    goal: "Check public GitHub repositories for updated READMEs, topics, and descriptions. Open PRs for any necessary maintenance.",
  },
  {
    id: "report",
    number: 13,
    name: "Weekly Sunday Audit Scorecard",
    schedule: "Sunday 19:00 IST",
    frequency: "Weekly (Sunday)",
    managerDomain: "PERSONAL_OPS",
    priority: "HIGH_P1",
    tierOrCap: "Full Weekly Audit Scorecard",
    description: "Weekly metrics: sent, replied, interviews, offers, template reply rates, DSA solved, stop rules hit.",
    goal: "Compile weekly performance scorecard: outreach sent, replies, interviews, offers, DSA problems completed, and safety stops.",
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

  const triggerOperation = async (op: OperationalTask) => {
    setTriggeringId(op.id);
    try {
      await api.createTask({
        title: op.name,
        goal: op.goal,
        manager_domain: op.managerDomain,
        priority: op.priority,
      });
      toast.success(`${op.name} created! Running via ${op.managerDomain.toLowerCase()} manager.`);
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to create task");
    } finally {
      setTriggeringId(null);
    }
  };

  const filteredOperations = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return OPERATIONAL_TASKS;
    return OPERATIONAL_TASKS.filter(
      (p) =>
        p.name.toLowerCase().includes(q) ||
        p.description.toLowerCase().includes(q) ||
        p.schedule.toLowerCase().includes(q) ||
        p.managerDomain.toLowerCase().includes(q),
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
            placeholder="Search tasks or operations"
            className="h-11 pl-9"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>

        <div className="flex min-h-11 items-center justify-between gap-2">
          {filter === "operations" ? (
            <span className="text-xs text-muted-foreground tabular-nums">
              {filteredOperations.length} operational task{filteredOperations.length === 1 ? "" : "s"}
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
          {filter !== "operations" && rows.length > 0 &&
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

        {filter === "operations" ? (
          <div className="space-y-4">
            {/* Operational Rules Banner */}
            <div className="rounded-2xl border border-primary/20 bg-primary/5 p-4 text-xs space-y-2">
              <div className="flex items-center gap-2 font-semibold text-primary">
                <ShieldAlert className="size-4" />
                0. GLOBAL RULES & Safety Engine
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 text-muted-foreground">
                <div>• <strong className="text-foreground">Operating Window:</strong> Mon-Fri 09:30-19:00 IST (random 4-15m gaps)</div>
                <div>• <strong className="text-foreground">Daily Caps:</strong> 10 LinkedIn · 12 Cold Emails · 8 Applications</div>
                <div>• <strong className="text-foreground">Circuit Breaker:</strong> 24h halt on CAPTCHA, 2FA, or rate limits</div>
                <div>• <strong className="text-foreground">Strict Grounding:</strong> Strictly verified against AJ_Resume_Context.md</div>
                <div className="sm:col-span-2">• <strong className="text-foreground">Dual-Copy Mirror:</strong> Local ~/Documents/Griffin/ & AWS S3</div>
              </div>
            </div>

            {/* Operational Task Cards Grid */}
            <div className="grid grid-cols-1 gap-3">
              {filteredOperations.map((p) => (
                <div key={p.id} className="rounded-2xl border bg-card p-4 sm:p-5 space-y-3 shadow-sm hover:border-primary/40 transition-colors">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span className="flex size-6 items-center justify-center rounded-full bg-primary/10 text-primary font-mono text-xs font-semibold">
                        #{p.number}
                      </span>
                      <h4 className="text-sm font-semibold text-foreground">{p.name}</h4>
                      <span className="rounded-md border bg-muted/30 px-1.5 py-0.5 text-[10px] font-mono text-muted-foreground uppercase">
                        {p.managerDomain}
                      </span>
                    </div>
                    <div className="flex items-center gap-2">
                      <span className="inline-flex items-center gap-1 rounded-full border bg-muted/40 px-2.5 py-1 text-[11px] text-muted-foreground">
                        <Clock className="size-3" />
                        {p.schedule}
                      </span>
                      <Button
                        size="sm"
                        className="h-9 px-3 gap-1.5 text-xs font-medium rounded-xl"
                        disabled={triggeringId === p.id}
                        onClick={() => triggerOperation(p)}
                      >
                        {triggeringId === p.id ? (
                          <RefreshCw className="size-3.5 animate-spin" />
                        ) : (
                          <Play className="size-3.5" />
                        )}
                        Run Task
                      </Button>
                    </div>
                  </div>

                  <p className="text-xs text-muted-foreground leading-relaxed">{p.description}</p>

                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted-foreground pt-2 border-t border-border/40">
                    <span>
                      <strong className="text-foreground font-medium">Constraints:</strong> {p.tierOrCap}
                    </span>
                    <span>
                      <strong className="text-foreground font-medium">Target:</strong> {p.goal.slice(0, 100)}...
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
