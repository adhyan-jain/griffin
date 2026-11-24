import { describe, expect, it } from "vitest";
import {
  budgetRows,
  claimStatusView,
  depthDescription,
  reportExtras,
  runControls,
  runStatusView,
} from "@/lib/research";
import type { ResearchPhase } from "@/lib/api";

describe("runControls", () => {
  it("pause only for active, resume only for paused, cancel for non-terminal", () => {
    expect(runControls("SOURCE_DISCOVERY")).toEqual({ pause: true, resume: false, cancel: true });
    expect(runControls("PAUSED")).toEqual({ pause: false, resume: true, cancel: true });
    for (const t of ["COMPLETED", "CANCELLED", "FAILED"] as ResearchPhase[]) {
      expect(runControls(t)).toEqual({ pause: false, resume: false, cancel: false });
    }
  });
});

describe("runStatusView", () => {
  it("reads a budget stop as partial coverage, not complete", () => {
    const v = runStatusView({ status: "COMPLETED", stop_reason: "budget_pages" });
    expect(v.partial).toBe(true);
    expect(v.label).toBe("Stopped: budget - partial coverage");
    expect(v.label).not.toMatch(/^completed/i);
  });
  it("reads an answered run as complete", () => {
    const v = runStatusView({ status: "COMPLETED", stop_reason: "answered" });
    expect(v).toMatchObject({ label: "Completed", partial: false, tone: "ok" });
  });
  it("treats exhausted as partial and surfaces failure errors", () => {
    expect(runStatusView({ status: "COMPLETED", stop_reason: "exhausted" }).partial).toBe(true);
    const f = runStatusView({ status: "FAILED", stop_reason: "failed", error_message: "boom" });
    expect(f.tone).toBe("failed");
    expect(f.detail).toBe("boom");
  });
});

describe("budgetRows", () => {
  it("shows used vs max and flags exhausted budgets", () => {
    const rows = budgetRows({
      budget_config: { max_searches: 6, max_pages: 10, max_model_calls: 12, max_time_seconds: 120 },
      progress_metrics: { searches_done: 6, pages_inspected: 4, model_calls: 7, time_ms: 31000 },
    });
    expect(rows.map((r) => [r.key, r.used, r.max, r.exhausted])).toEqual([
      ["searches", 6, 6, true],
      ["pages", 4, 10, false],
      ["model_calls", 7, 12, false],
      ["time", 31, 120, false],
    ]);
  });
});

describe("claimStatusView", () => {
  it("keeps unverified distinct from supported", () => {
    const s = claimStatusView("supported");
    const u = claimStatusView("unverified");
    expect(s.verified).toBe(true);
    expect(u.verified).toBe(false);
    expect(u.tone).not.toBe(s.tone);
    expect(claimStatusView("conflicting").verified).toBe(false);
  });
});

describe("depthDescription", () => {
  it("matches domain.BudgetDefaults", () => {
    expect(depthDescription("quick")).toBe("3 searches, 4 pages, 4 model calls, 60s");
    expect(depthDescription("comprehensive")).toBe("25 searches, 45 pages, 50 model calls, 600s");
  });
});

describe("reportExtras", () => {
  it("reads conflicts and open questions, falling back to unresolved_questions", () => {
    const conflicts = [{ subject: "A", property: "p", values: [{ value: "1", source_url: "u", finding_id: "f" }] }];
    expect(reportExtras({ report_structured: { conflicts } }).conflicts).toEqual(conflicts);
    expect(reportExtras({ report_structured: {}, unresolved_questions: ["q"] }).openQuestions).toEqual(["q"]);
    expect(reportExtras({}).conflicts).toEqual([]);
  });
});
