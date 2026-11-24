// Pure helpers for the Research page: status wording, budgets and run controls. Names and values mirror the Go
// domain (internal/domain/research.go, evidence.go) and the report in internal/research/report.go.
import type { ResearchPhase, ResearchRun, ResearchDepth } from "@/lib/api";

export const TERMINAL_PHASES: ResearchPhase[] = ["COMPLETED", "CANCELLED", "FAILED"];

export const isTerminalPhase = (s: ResearchPhase) => TERMINAL_PHASES.includes(s);
export const isActivePhase = (s: ResearchPhase) => !isTerminalPhase(s) && s !== "PAUSED";

export interface RunControls {
  pause: boolean;
  resume: boolean;
  cancel: boolean;
}

/** Pause only for active runs, resume only for paused ones, cancel for anything not terminal. */
export function runControls(status: ResearchPhase): RunControls {
  return { pause: isActivePhase(status), resume: status === "PAUSED", cancel: !isTerminalPhase(status) };
}

// Mirrors domain.BudgetDefaults.
export const BUDGET_DEFAULTS: Record<ResearchDepth, { searches: number; pages: number; modelCalls: number; seconds: number }> = {
  quick: { searches: 3, pages: 4, modelCalls: 4, seconds: 60 },
  standard: { searches: 6, pages: 10, modelCalls: 12, seconds: 120 },
  deep: { searches: 12, pages: 20, modelCalls: 25, seconds: 300 },
  comprehensive: { searches: 25, pages: 45, modelCalls: 50, seconds: 600 },
};

export function depthDescription(d: ResearchDepth): string {
  const b = BUDGET_DEFAULTS[d];
  return `${b.searches} searches, ${b.pages} pages, ${b.modelCalls} model calls, ${b.seconds}s`;
}

export type StatusTone = "ok" | "active" | "paused" | "partial" | "failed" | "cancelled";

export interface RunStatusView {
  label: string;
  tone: StatusTone;
  /** True when the run ended without answering every question; coverage is incomplete. */
  partial: boolean;
  detail: string;
}

const STOP_TEXT: Record<string, string> = {
  answered: "every question has a supported finding",
  exhausted: "no new queries left for the open questions",
  budget_searches: "search budget used up",
  budget_pages: "page budget used up",
  budget_model_calls: "model-call budget used up",
  budget_time: "time budget used up",
  cancelled: "cancelled",
  failed: "failed",
};

/** Words for a run's status. A run that stopped on a budget (or ran dry) reads "partial coverage", never complete. */
export function runStatusView(run: Pick<ResearchRun, "status" | "stop_reason" | "error_message">): RunStatusView {
  const reason = run.stop_reason ?? "";
  const why = STOP_TEXT[reason] ?? reason.replace(/_/g, " ");
  if (run.status === "COMPLETED") {
    if (reason === "answered" || reason === "") {
      return { label: "Completed", tone: "ok", partial: false, detail: why };
    }
    const kind = reason.startsWith("budget_") ? "budget" : reason;
    return { label: `Stopped: ${kind} - partial coverage`, tone: "partial", partial: true, detail: why };
  }
  if (run.status === "FAILED") {
    return { label: "Failed", tone: "failed", partial: true, detail: run.error_message || why };
  }
  if (run.status === "CANCELLED") return { label: "Cancelled", tone: "cancelled", partial: true, detail: why };
  if (run.status === "PAUSED") return { label: "Paused", tone: "paused", partial: false, detail: "" };
  return { label: run.status.replace(/_/g, " ").toLowerCase(), tone: "active", partial: false, detail: "" };
}

export interface BudgetRow {
  key: "searches" | "pages" | "model_calls" | "time";
  label: string;
  used: number;
  max: number;
  unit: string;
  exhausted: boolean;
}

/** Used vs max for each budget, from the run's progress_metrics and budget_config. Time is shown in seconds. */
export function budgetRows(run: Pick<ResearchRun, "progress_metrics" | "budget_config">): BudgetRow[] {
  const m = run.progress_metrics ?? {};
  const c = run.budget_config ?? {};
  const row = (key: BudgetRow["key"], label: string, used: number, max: number, unit: string): BudgetRow => ({
    key,
    label,
    used,
    max,
    unit,
    exhausted: max > 0 && used >= max,
  });
  return [
    row("searches", "Searches", m.searches_done ?? 0, c.max_searches ?? 0, ""),
    row("pages", "Pages", m.pages_inspected ?? 0, c.max_pages ?? 0, ""),
    row("model_calls", "Model calls", m.model_calls ?? 0, c.max_model_calls ?? 0, ""),
    row("time", "Time", Math.round((m.time_ms ?? 0) / 1000), c.max_time_seconds ?? 0, "s"),
  ];
}

export type ClaimTone = "supported" | "unverified" | "conflicting" | "other";

/** Evidence and finding statuses, kept distinct: only "supported" and "cross_verified" mean a verified quote. */
export function claimStatusView(status: string): { label: string; tone: ClaimTone; verified: boolean } {
  switch (status) {
    case "supported":
      return { label: "Supported", tone: "supported", verified: true };
    case "cross_verified":
      return { label: "Cross-verified", tone: "supported", verified: true };
    case "unverified":
      return { label: "Unverified", tone: "unverified", verified: false };
    case "conflicting":
      return { label: "Conflicting", tone: "conflicting", verified: false };
    default:
      return { label: status.replace(/_/g, " "), tone: "other", verified: false };
  }
}

export interface ReportConflict {
  subject: string;
  property: string;
  values: { value: string; source_url: string; finding_id: string; evidence_id?: string }[];
}

/** Conflicts and open questions from the run's structured report (written by code, not a model). */
export function reportExtras(run: Pick<ResearchRun, "report_structured" | "unresolved_questions">) {
  const s = (run.report_structured ?? {}) as Record<string, unknown>;
  const conflicts = Array.isArray(s.conflicts) ? (s.conflicts as ReportConflict[]) : [];
  const fromReport = Array.isArray(s.open_questions) ? (s.open_questions as string[]) : [];
  const openQuestions = fromReport.length ? fromReport : (run.unresolved_questions ?? []);
  return { conflicts, openQuestions };
}
