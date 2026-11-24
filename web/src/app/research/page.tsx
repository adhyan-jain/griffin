"use client";

import { useEffect, useRef, useState } from "react";
import {
  Compass,
  Search,
  Sparkles,
  Play,
  Pause,
  XCircle,
  Loader2,
  ExternalLink,
  ShieldCheck,
  AlertTriangle,
  FileText,
  Clock,
  Layers,
  CheckCircle2,
  ListOrdered,
  ChevronRight,
  Database,
} from "lucide-react";
import { toast } from "sonner";
import {
  api,
  type ResearchRun,
  type ResearchDepth,
  type ResearchPlaybook,
  type EvidenceItem,
} from "@/lib/api";
import {
  budgetRows,
  claimStatusView,
  depthDescription,
  reportExtras,
  runControls,
  runStatusView,
  type ClaimTone,
  type StatusTone,
} from "@/lib/research";
import { usePolling } from "@/lib/use-polling";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { EmptyState, ErrorNote, PageContainer, PageHeader, timeAgo } from "@/components/page";

const DEPTH_OPTIONS: { id: ResearchDepth; label: string; desc: string }[] = (
  [
    ["quick", "Quick"],
    ["standard", "Standard"],
    ["deep", "Deep"],
    ["comprehensive", "Comprehensive"],
  ] as const
).map(([id, label]) => ({ id, label, desc: depthDescription(id) }));

const PLAYBOOK_OPTIONS: { id: ResearchPlaybook; label: string; desc: string }[] = [
  { id: "generic", label: "General Research", desc: "Open-ended exploratory investigation" },
  { id: "internship_watch", label: "Internship Watch", desc: "Multi-market role discovery & ATS verification" },
  { id: "company_intelligence", label: "Company Intelligence", desc: "Tech stacks, funding, traction & headcount" },
  { id: "alumni_intelligence", label: "Alumni Intelligence", desc: "Graduation year <= 2022, leadership & outreach" },
];

const STATUS_TONE: Record<StatusTone, string> = {
  ok: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  active: "bg-blue-500/15 text-blue-600 dark:text-blue-400",
  paused: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  partial: "bg-amber-500/15 text-amber-700 dark:text-amber-400",
  failed: "bg-destructive/15 text-destructive",
  cancelled: "bg-muted text-muted-foreground",
};

const CLAIM_TONE: Record<ClaimTone, string> = {
  supported: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  unverified: "bg-amber-500/15 text-amber-700 dark:text-amber-400",
  conflicting: "bg-destructive/15 text-destructive",
  other: "bg-muted text-muted-foreground",
};

function ClaimBadge({ status }: { status: string }) {
  const v = claimStatusView(status);
  return (
    <Badge className={cn("text-[10px]", CLAIM_TONE[v.tone])}>
      {v.verified ? <ShieldCheck className="mr-1 size-3" /> : <AlertTriangle className="mr-1 size-3" />}
      {v.label}
    </Badge>
  );
}

// Every phase the backend can report (domain.ResearchPhase), in lifecycle order. PAUSED, CANCELLED and FAILED
// are not steps; they are shown by the status badge.
const PHASES = [
  "REQUEST_RECEIVED",
  "CONTEXT_LOADING",
  "OBJECTIVE_ANALYSIS",
  "RESEARCH_PLANNING",
  "SOURCE_DISCOVERY",
  "INFORMATION_RETRIEVAL",
  "CONTENT_INSPECTION",
  "EXTRACTION",
  "EVIDENCE_VALIDATION",
  "GAP_ANALYSIS",
  "FOLLOW_UP_RESEARCH",
  "SYNTHESIS",
  "PERSISTENCE",
  "COMPLETED",
];

export default function ResearchWorkspacePage() {
  const [runs, setRuns] = useState<ResearchRun[]>([]);
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const [evidence, setEvidence] = useState<EvidenceItem[]>([]);
  const [objective, setObjective] = useState("");
  const [depth, setDepth] = useState<ResearchDepth>("standard");
  const [playbook, setPlaybook] = useState<ResearchPlaybook>("generic");
  const [starting, setStarting] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [runsLoaded, setRunsLoaded] = useState(false);
  const [evidenceLoading, setEvidenceLoading] = useState(false);
  const [evidenceError, setEvidenceError] = useState<string | null>(null);

  // The run whose evidence is on screen; a response for any other run is stale and dropped.
  const activeIdRef = useRef<string | null>(null);

  const { refresh } = usePolling(
    async (signal) => {
      try {
        const list = await api.listResearchRuns({ signal });
        setRuns(list);
        setRunsLoaded(true);
        setSelectedRunId((cur) => cur ?? list[0]?.id ?? null);
        setError(null);
        const id = activeIdRef.current;
        const current = list.find((r) => r.id === id);
        // Keep evidence fresh while the run can still gather more.
        if (id && current && runControls(current.status).cancel) {
          const ev = await api.getResearchEvidence(id, { signal });
          if (activeIdRef.current === id) {
            setEvidence(ev);
            setEvidenceError(null);
          }
        }
      } catch (e) {
        if (!signal.aborted) {
          setError(e instanceof Error ? e.message : "Failed to load research runs");
        }
      }
    },
    { intervalMs: 4000 },
  );

  const selectedRun = runs.find((r) => r.id === selectedRunId) ?? runs[0] ?? null;
  const effectiveId = selectedRun?.id ?? null;

  // Fetch evidence as soon as a run is selected, and clear the previous run's evidence.
  useEffect(() => {
    activeIdRef.current = effectiveId;
    setEvidence([]);
    setEvidenceError(null);
    if (!effectiveId) {
      setEvidenceLoading(false);
      return;
    }
    const ctrl = new AbortController();
    setEvidenceLoading(true);
    api
      .getResearchEvidence(effectiveId, { signal: ctrl.signal })
      .then((ev) => {
        if (activeIdRef.current === effectiveId) setEvidence(ev);
      })
      .catch((e) => {
        if (!ctrl.signal.aborted && activeIdRef.current === effectiveId) {
          setEvidenceError(e instanceof Error ? e.message : "Failed to load evidence");
        }
      })
      .finally(() => {
        if (activeIdRef.current === effectiveId) setEvidenceLoading(false);
      });
    return () => ctrl.abort();
  }, [effectiveId]);

  async function handleStart() {
    if (!objective.trim()) {
      toast.error("Please enter a research objective.");
      return;
    }
    setStarting(true);
    try {
      const created = await api.createResearchRun({
        objective: objective.trim(),
        depth,
        playbook,
      });
      toast.success("Research run started");
      setObjective("");
      setSelectedRunId(created.id);
      setRuns((prev) => [created, ...prev]);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not launch research run");
    } finally {
      setStarting(false);
    }
  }

  async function handlePause(id: string) {
    setActionBusy(true);
    try {
      await api.pauseResearchRun(id);
      toast.info("Research run paused");
      refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to pause run");
    } finally {
      setActionBusy(false);
    }
  }

  async function handleResume(id: string) {
    setActionBusy(true);
    try {
      await api.resumeResearchRun(id);
      toast.success("Research run resumed");
      refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to resume run");
    } finally {
      setActionBusy(false);
    }
  }

  async function handleCancel(id: string) {
    if (!window.confirm("Cancel this research run? It cannot be resumed.")) return;
    setActionBusy(true);
    try {
      await api.cancelResearchRun(id);
      toast.info("Research run cancelled");
      refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to cancel run");
    } finally {
      setActionBusy(false);
    }
  }

  const status = selectedRun ? runStatusView(selectedRun) : null;
  const controls = selectedRun ? runControls(selectedRun.status) : null;
  const extras = selectedRun ? reportExtras(selectedRun) : null;

  return (
    <PageContainer className="max-w-7xl">
      <PageHeader
        title="Research Engine"
        description="Iterative, evidence-grounded research workspace. Decomposes objectives, inspects primary sources, validates factual claims, and synthesizes structured intelligence."
      />

      {error && <div className="mb-6"><ErrorNote message={error} /></div>}

      {/* New Research Form */}
      <section className="mb-10 rounded-2xl border bg-card p-6 shadow-sm">
        <h2 className="mb-2 text-lg font-semibold tracking-tight">New Research Run</h2>
        <p className="mb-4 text-sm text-muted-foreground">
          Enter an open objective. Griffin will generate initial search queries, discover sources, validate claims, and persist findings to the knowledge graph.
        </p>

        <div className="space-y-4">
          <div className="flex flex-col gap-2 md:flex-row">
            <Input
              placeholder="e.g. Map all Series A AI infrastructure companies with engineering offices in Bengaluru"
              value={objective}
              onChange={(e) => setObjective(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && !starting && handleStart()}
              className="h-11 text-base md:flex-1"
            />
            <Button
              onClick={handleStart}
              disabled={starting || !objective.trim()}
              className="h-11 px-6 font-medium"
            >
              <Sparkles className="mr-2 size-4" />
              {starting ? "Initializing..." : "Launch Research"}
            </Button>
          </div>

          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <label className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Research Depth
              </label>
              <div className="grid grid-cols-2 gap-2">
                {DEPTH_OPTIONS.map((opt) => (
                  <button
                    key={opt.id}
                    type="button"
                    onClick={() => setDepth(opt.id)}
                    className={cn(
                      "flex flex-col items-start rounded-lg border p-2.5 text-left text-xs transition-colors",
                      depth === opt.id
                        ? "border-primary bg-primary/10 text-primary font-medium"
                        : "border-border hover:bg-accent/50 text-muted-foreground",
                    )}
                  >
                    <span className="font-semibold text-foreground">{opt.label}</span>
                    <span className="line-clamp-1 opacity-75">{opt.desc}</span>
                  </button>
                ))}
              </div>
            </div>

            <div>
              <label className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Specialized Playbook
              </label>
              <div className="grid grid-cols-2 gap-2">
                {PLAYBOOK_OPTIONS.map((pb) => (
                  <button
                    key={pb.id}
                    type="button"
                    onClick={() => setPlaybook(pb.id)}
                    className={cn(
                      "flex flex-col items-start rounded-lg border p-2.5 text-left text-xs transition-colors",
                      playbook === pb.id
                        ? "border-primary bg-primary/10 text-primary font-medium"
                        : "border-border hover:bg-accent/50 text-muted-foreground",
                    )}
                  >
                    <span className="font-semibold text-foreground">{pb.label}</span>
                    <span className="line-clamp-1 opacity-75">{pb.desc}</span>
                  </button>
                ))}
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* Main Workspace Layout */}
      {!runsLoaded && !error ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> Loading research runs...
        </p>
      ) : runs.length === 0 ? (
        <EmptyState
          icon={<Compass className="size-5" />}
          title="No research runs yet"
          description="Launch a research run above to begin autonomous intelligence gathering."
        />
      ) : (
        <div className="grid grid-cols-1 gap-8 lg:grid-cols-12">
          {/* Left Column: Runs list & Active Run Inspection */}
          <div className="space-y-6 lg:col-span-8">
            {/* Active Run Header */}
            {selectedRun && (
              <div className="rounded-2xl border bg-card p-6 shadow-sm">
                <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-start">
                  <div>
                    <div className="flex items-center gap-2">
                      <Badge variant="outline" className="font-mono uppercase text-[11px]">
                        {selectedRun.playbook}
                      </Badge>
                      <Badge className={cn("text-[11px]", STATUS_TONE[status!.tone])}>
                        {status!.label}
                      </Badge>
                      <span className="text-xs text-muted-foreground">
                        {timeAgo(selectedRun.created_at)}
                      </span>
                    </div>
                    <h3 className="mt-2 text-xl font-bold tracking-tight text-foreground">
                      {selectedRun.objective}
                    </h3>
                  </div>

                  <div className="flex shrink-0 items-center gap-2">
                    {controls!.resume && (
                      <Button size="sm" variant="outline" onClick={() => handleResume(selectedRun.id)} disabled={actionBusy}>
                        <Play className="mr-1.5 size-3.5" /> Resume
                      </Button>
                    )}
                    {controls!.pause && (
                      <Button size="sm" variant="outline" onClick={() => handlePause(selectedRun.id)} disabled={actionBusy}>
                        <Pause className="mr-1.5 size-3.5" /> Pause
                      </Button>
                    )}
                    {controls!.cancel && (
                      <Button size="sm" variant="outline" onClick={() => handleCancel(selectedRun.id)} disabled={actionBusy}>
                        <XCircle className="mr-1.5 size-3.5" /> Cancel
                      </Button>
                    )}
                  </div>
                </div>

                {(status!.partial || status!.tone === "failed" || selectedRun.error_message) && (
                  <div
                    role="status"
                    className={cn(
                      "mt-4 rounded-lg border p-3 text-sm",
                      status!.tone === "failed"
                        ? "border-destructive/40 bg-destructive/10 text-destructive"
                        : "border-amber-500/40 bg-amber-500/10 text-amber-800 dark:text-amber-300",
                    )}
                  >
                    <p className="flex items-center gap-2 font-semibold">
                      <AlertTriangle className="size-4" /> {status!.label}
                    </p>
                    {status!.detail && <p className="mt-1 text-xs">{status!.detail}</p>}
                    {selectedRun.error_message && status!.detail !== selectedRun.error_message && (
                      <p className="mt-1 text-xs">Error: {selectedRun.error_message}</p>
                    )}
                  </div>
                )}
                {selectedRun.stop_reason && (
                  <p className="mt-3 text-xs text-muted-foreground">
                    Stop reason: <span className="font-mono">{selectedRun.stop_reason}</span>
                  </p>
                )}

                {/* Budgets */}
                <div className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
                  {budgetRows(selectedRun).map((b) => (
                    <div
                      key={b.key}
                      className={cn("rounded-lg border p-2.5 text-xs", b.exhausted && "border-amber-500/50 bg-amber-500/10")}
                    >
                      <p className="text-muted-foreground">{b.label}</p>
                      <p className="mt-0.5 font-mono text-sm font-semibold text-foreground">
                        {b.used}
                        {b.unit} / {b.max || "?"}
                        {b.max ? b.unit : ""}
                      </p>
                    </div>
                  ))}
                </div>

                {/* Stepper Progress */}
                <div className="mt-6 border-t pt-4">
                  <div className="mb-2 flex items-center justify-between text-xs text-muted-foreground">
                    <span className="font-medium">Lifecycle Stepper</span>
                    <span>
                      round {selectedRun.round ?? 0} · {selectedRun.findings?.length ?? 0} findings
                    </span>
                  </div>
                  <div className="flex items-center gap-1.5 overflow-x-auto pb-2">
                    {PHASES.map((p, idx) => {
                      const isCurrent = selectedRun.status === p;
                      const isDone =
                        selectedRun.status === "COMPLETED" ||
                        PHASES.indexOf(selectedRun.status) > idx;
                      return (
                        <div
                          key={p}
                          className={cn(
                            "flex items-center gap-1 rounded-full px-2.5 py-1 text-[11px] font-medium whitespace-nowrap",
                            isCurrent
                              ? "bg-primary text-primary-foreground font-semibold"
                              : isDone
                              ? "bg-muted text-foreground"
                              : "bg-muted/50 text-muted-foreground opacity-50",
                          )}
                        >
                          {isDone ? (
                            <CheckCircle2 className="size-3 text-emerald-500" />
                          ) : (
                            <span className="size-1.5 rounded-full bg-current" />
                          )}
                          <span>{p.replace(/_/g, " ").toLowerCase()}</span>
                        </div>
                      );
                    })}
                  </div>
                </div>

                {/* Findings Table */}
                <div className="mt-6 border-t pt-5">
                  <h4 className="mb-3 flex items-center gap-2 text-sm font-semibold tracking-tight">
                    <ShieldCheck className="size-4 text-primary" /> Findings (check each status)
                  </h4>
                  {selectedRun.findings && selectedRun.findings.length > 0 ? (
                    <div className="space-y-3">
                      {selectedRun.findings.map((f, i) => (
                        <div
                          key={f.id || i}
                          className="rounded-xl border bg-muted/30 p-3.5 text-sm transition-colors hover:bg-muted/50"
                        >
                          <div className="flex items-start justify-between gap-2">
                            <span className="font-semibold text-foreground">{f.topic}</span>
                            <div className="flex items-center gap-1.5">
                              <ClaimBadge status={f.status} />
                              <Badge variant="secondary" className="font-mono text-[10px]">
                                {f.confidence}% conf
                              </Badge>
                            </div>
                          </div>
                          <p className="mt-1 text-foreground/90">{f.claim}</p>
                          {f.quote && (
                            <blockquote className="mt-2 border-l-2 border-primary/40 pl-2.5 text-xs italic text-muted-foreground">
                              “{f.quote}”
                            </blockquote>
                          )}
                          {f.source_url && (
                            <a
                              href={f.source_url}
                              target="_blank"
                              rel="noreferrer"
                              className="mt-2 inline-flex items-center gap-1 text-xs text-primary hover:underline"
                            >
                              <ExternalLink className="size-3" /> {f.source_url}
                            </a>
                          )}
                        </div>
                      ))}
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground">
                      No findings extracted yet for this run.
                    </p>
                  )}
                </div>

                {/* Open questions and conflicts from the report */}
                {extras!.openQuestions.length > 0 && (
                  <div className="mt-6 border-t pt-5">
                    <h4 className="mb-2 flex items-center gap-2 text-sm font-semibold tracking-tight">
                      <ListOrdered className="size-4 text-primary" /> Open questions
                    </h4>
                    <ul className="list-disc space-y-1 pl-5 text-sm text-foreground/90">
                      {extras!.openQuestions.map((q) => (
                        <li key={q}>{q}</li>
                      ))}
                    </ul>
                  </div>
                )}
                {extras!.conflicts.length > 0 && (
                  <div className="mt-6 border-t pt-5">
                    <h4 className="mb-2 flex items-center gap-2 text-sm font-semibold tracking-tight">
                      <AlertTriangle className="size-4 text-destructive" /> Conflicting evidence
                    </h4>
                    <p className="mb-2 text-xs text-muted-foreground">
                      Sources disagree. No value has been chosen.
                    </p>
                    <div className="space-y-2">
                      {extras!.conflicts.map((c) => (
                        <div key={`${c.subject}/${c.property}`} className="rounded-lg border border-destructive/30 p-3 text-xs">
                          <p className="font-semibold text-foreground">
                            {c.subject} / {c.property}
                          </p>
                          <ul className="mt-1 space-y-1">
                            {(c.values ?? []).map((v) => (
                              <li key={v.finding_id} className="flex flex-wrap items-center gap-1.5">
                                <span className="text-foreground">{v.value}</span>
                                <a href={v.source_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-0.5 text-primary hover:underline">
                                  source <ExternalLink className="size-2.5" />
                                </a>
                              </li>
                            ))}
                          </ul>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {/* Synthesis Report */}
                {selectedRun.report_markdown && (
                  <div className="mt-6 border-t pt-5">
                    <h4 className="mb-3 flex items-center gap-2 text-sm font-semibold tracking-tight">
                      <FileText className="size-4 text-primary" /> Synthesis Report
                    </h4>
                    <div className="prose prose-sm dark:prose-invert max-w-none rounded-xl border bg-card p-4">
                      <pre className="font-sans whitespace-pre-wrap text-sm leading-relaxed text-foreground">
                        {selectedRun.report_markdown}
                      </pre>
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>

          {/* Right Column: Evidence Drawer & Historical Runs */}
          <div className="space-y-6 lg:col-span-4">
            {/* Evidence Drawer */}
            <div className="rounded-2xl border bg-card p-5 shadow-sm">
              <div className="mb-3 flex items-center justify-between">
                <h3 className="flex items-center gap-2 text-sm font-semibold tracking-tight">
                  <Database className="size-4 text-primary" /> Evidence
                </h3>
                <span className="text-xs text-muted-foreground">{evidence.length} items</span>
              </div>
              <p className="mb-3 text-xs text-muted-foreground">
                Source excerpts gathered for this run. Only items marked Supported had their quote found in the page.
              </p>

              {evidenceError ? (
                <ErrorNote message={evidenceError} />
              ) : evidenceLoading ? (
                <p className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Loader2 className="size-3.5 animate-spin" /> Loading evidence...
                </p>
              ) : evidence.length > 0 ? (
                <div className="max-h-[500px] space-y-3 overflow-y-auto pr-1">
                  {evidence.map((ev) => (
                    <div key={ev.id} className="rounded-lg border p-3 text-xs">
                      <div className="flex items-center justify-between font-mono text-[10px] text-muted-foreground">
                        <span className="truncate">{ev.source_domain}</span>
                        <ClaimBadge status={ev.status} />
                      </div>
                      <p className="mt-1 font-medium text-foreground">{ev.claim}</p>
                      {ev.excerpt_quote && (
                        <p className="mt-1.5 border-l border-muted-foreground/30 pl-2 text-muted-foreground italic">
                          &ldquo;{ev.excerpt_quote}&rdquo;
                        </p>
                      )}
                      <div className="mt-2 flex items-center justify-between text-[10px] text-muted-foreground">
                        <span>
                          Confidence: {ev.confidence}%
                          {ev.retrieved_at && <> · retrieved {timeAgo(ev.retrieved_at)}</>}
                        </span>
                        <a
                          href={ev.source_url}
                          target="_blank"
                          rel="noreferrer"
                          className="text-primary hover:underline inline-flex items-center gap-0.5"
                        >
                          Source <ExternalLink className="size-2.5" />
                        </a>
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-xs text-muted-foreground">
                  No evidence gathered yet for selected run.
                </p>
              )}
            </div>

            {/* Recent Runs List */}
            <div className="rounded-2xl border bg-card p-5 shadow-sm">
              <h3 className="mb-3 text-sm font-semibold tracking-tight">Research History</h3>
              <div className="space-y-2">
                {runs.map((r) => (
                  <button
                    key={r.id}
                    onClick={() => setSelectedRunId(r.id)}
                    className={cn(
                      "w-full rounded-xl border p-3 text-left transition-colors flex items-center justify-between",
                      selectedRunId === r.id
                        ? "border-primary bg-primary/10"
                        : "hover:bg-accent/40",
                    )}
                  >
                    <div className="min-w-0 pr-2">
                      <p className="line-clamp-1 text-xs font-semibold text-foreground">
                        {r.objective}
                      </p>
                      <div className="mt-1 flex items-center gap-1.5 text-[10px] text-muted-foreground">
                        <span>{r.depth}</span>
                        <span>·</span>
                        <span>{timeAgo(r.created_at)}</span>
                      </div>
                    </div>
                    <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
                  </button>
                ))}
              </div>
            </div>
          </div>
        </div>
      )}
    </PageContainer>
  );
}
