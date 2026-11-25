"use client";

import { useState } from "react";
import {
  Compass,
  Search,
  Sparkles,
  Play,
  Pause,
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
  type EvidenceItem,
} from "@/lib/api";
import { usePolling } from "@/lib/use-polling";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { EmptyState, ErrorNote, PageContainer, PageHeader, timeAgo } from "@/components/page";

const DEPTH_OPTIONS: { id: ResearchDepth; label: string; desc: string }[] = [
  { id: "quick", label: "Quick", desc: "1-2 cycles, ~5 pages" },
  { id: "standard", label: "Standard", desc: "3-4 cycles, ~15 pages" },
  { id: "deep", label: "Deep", desc: "Iterative query expansion, ~30 pages" },
  { id: "comprehensive", label: "Comprehensive", desc: "Exhaustive multi-source validation" },
];

const PLAYBOOK_OPTIONS = [
  { id: "general_research", label: "General Research", desc: "Open-ended exploratory investigation" },
  { id: "internship_watch", label: "Internship Watch", desc: "Multi-market role discovery & ATS verification" },
  { id: "company_intelligence", label: "Company Intelligence", desc: "Tech stacks, funding, traction & headcount" },
  { id: "alumni_intelligence", label: "Alumni Intelligence", desc: "Graduation year <= 2022, leadership & outreach" },
];

const PHASES = [
  "RESEARCH_PLANNING",
  "SOURCE_DISCOVERY",
  "INFORMATION_RETRIEVAL",
  "EXTRACTION",
  "EVIDENCE_VALIDATION",
  "SYNTHESIS",
  "COMPLETED",
];

export default function ResearchWorkspacePage() {
  const [runs, setRuns] = useState<ResearchRun[]>([]);
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);
  const [evidence, setEvidence] = useState<EvidenceItem[]>([]);
  const [objective, setObjective] = useState("");
  const [depth, setDepth] = useState<ResearchDepth>("standard");
  const [playbook, setPlaybook] = useState("general_research");
  const [starting, setStarting] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Poll research runs and active evidence
  usePolling(
    async (signal) => {
      try {
        const list = await api.listResearchRuns({ signal });
        setRuns(list);
        if (!selectedRunId && list.length > 0) {
          setSelectedRunId(list[0].id);
        }
        if (selectedRunId) {
          const ev = await api.getResearchEvidence(selectedRunId, { signal });
          setEvidence(ev);
        }
        setError(null);
      } catch (e) {
        if (!signal.aborted) {
          setError(e instanceof Error ? e.message : "Failed to load research runs");
        }
      }
    },
    { intervalMs: 4000 },
  );

  const selectedRun = runs.find((r) => r.id === selectedRunId) ?? runs[0] ?? null;

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
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to resume run");
    } finally {
      setActionBusy(false);
    }
  }

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
      {runs.length === 0 ? (
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
                      <Badge
                        className={cn(
                          "capitalize text-[11px]",
                          selectedRun.status === "COMPLETED"
                            ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
                            : selectedRun.status === "PAUSED"
                            ? "bg-amber-500/15 text-amber-600 dark:text-amber-400"
                            : "bg-blue-500/15 text-blue-600 dark:text-blue-400",
                        )}
                      >
                        {selectedRun.status.replace(/_/g, " ").toLowerCase()}
                      </Badge>
                      <span className="text-xs text-muted-foreground">
                        {timeAgo(selectedRun.started_at)}
                      </span>
                    </div>
                    <h3 className="mt-2 text-xl font-bold tracking-tight text-foreground">
                      {selectedRun.objective}
                    </h3>
                  </div>

                  <div className="flex shrink-0 items-center gap-2">
                    {selectedRun.status === "PAUSED" ? (
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => handleResume(selectedRun.id)}
                        disabled={actionBusy}
                      >
                        <Play className="mr-1.5 size-3.5" /> Resume
                      </Button>
                    ) : selectedRun.status !== "COMPLETED" && selectedRun.status !== "CANCELLED" ? (
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => handlePause(selectedRun.id)}
                        disabled={actionBusy}
                      >
                        <Pause className="mr-1.5 size-3.5" /> Pause
                      </Button>
                    ) : null}
                  </div>
                </div>

                {/* Stepper Progress */}
                <div className="mt-6 border-t pt-4">
                  <div className="mb-2 flex items-center justify-between text-xs text-muted-foreground">
                    <span className="font-medium">Lifecycle Stepper</span>
                    <span>
                      {selectedRun.progress_metrics?.searches_done ?? 0} searches ·{" "}
                      {selectedRun.progress_metrics?.pages_inspected ?? 0} pages ·{" "}
                      {selectedRun.findings?.length ?? 0} verified claims
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
                    <ShieldCheck className="size-4 text-emerald-500" /> Grounded Findings & Verified Claims
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
                              {f.is_contradiction && (
                                <Badge variant="destructive" className="text-[10px]">
                                  Contradiction Flagged
                                </Badge>
                              )}
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
                  <Database className="size-4 text-primary" /> Atomic Evidence Vault
                </h3>
                <span className="text-xs text-muted-foreground">{evidence.length} items</span>
              </div>
              <p className="mb-3 text-xs text-muted-foreground">
                All claims are grounded in verifiable source excerpts stored with temporal metadata.
              </p>

              {evidence.length > 0 ? (
                <div className="max-h-[500px] space-y-3 overflow-y-auto pr-1">
                  {evidence.map((ev) => (
                    <div key={ev.id} className="rounded-lg border p-3 text-xs">
                      <div className="flex items-center justify-between font-mono text-[10px] text-muted-foreground">
                        <span className="truncate">{ev.source_domain}</span>
                        <Badge variant="outline" className="text-[10px]">
                          {ev.status}
                        </Badge>
                      </div>
                      <p className="mt-1 font-medium text-foreground">{ev.claim}</p>
                      {ev.excerpt_quote && (
                        <p className="mt-1.5 border-l border-muted-foreground/30 pl-2 text-muted-foreground italic">
                          "{ev.excerpt_quote}"
                        </p>
                      )}
                      <div className="mt-2 flex items-center justify-between text-[10px] text-muted-foreground">
                        <span>Confidence: {ev.confidence}%</span>
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
                        <span>{timeAgo(r.started_at)}</span>
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
