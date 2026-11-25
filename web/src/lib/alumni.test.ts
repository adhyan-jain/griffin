import { describe, expect, it } from "vitest";
import {
  COLLEGE_PLACEHOLDER,
  EMPTY_FILTERS,
  MAX_DRAFT_CHARS,
  NAME_PLACEHOLDER,
  draftLength,
  draftProblem,
  draftTemplate,
  gradYearView,
  importSummary,
  outreachErrorMessage,
  outreachGate,
  priorityTierBadge,
  gradEligibilityBadge,
  roleFamilyText,
  toQuery,
} from "@/lib/alumni";

describe("gradYearView", () => {
  it("says Unknown when there is no year", () => {
    expect(gradYearView({ graduation_year: null })).toEqual({ text: "Unknown", note: null, known: false });
    expect(gradYearView({ graduation_year: 0 }).text).toBe("Unknown");
  });
  it("labels a tracker_batch year as an unverified LinkedIn headline", () => {
    const v = gradYearView({ graduation_year: 2021, graduation_year_source: "tracker_batch" });
    expect(v.text).toBe("2021");
    expect(v.note).toBe("from LinkedIn headline, unverified");
  });
  it("adds no caveat to an evidence-backed year", () => {
    expect(gradYearView({ graduation_year: 2020, graduation_year_source: "evidence" }).note).toBeNull();
  });
});

describe("roleFamilyText", () => {
  it("marks an inferred family", () => {
    expect(roleFamilyText({ role_family: "tech", role_family_inferred: true })).toBe("Tech (inferred)");
    expect(roleFamilyText({ role_family: "operations", role_family_inferred: false })).toBe("Operations");
    expect(roleFamilyText({ role_family: undefined, role_family_inferred: false })).toBe("Unknown");
  });
});

describe("outreachGate", () => {
  it("disables outreach for do-not-contact and opted-out people", () => {
    expect(outreachGate({ do_not_contact: true, contact_state: "not_contacted" }).disabled).toBe(true);
    expect(outreachGate({ do_not_contact: false, contact_state: "opted_out" }).disabled).toBe(true);
    expect(outreachGate({ do_not_contact: false, contact_state: "replied" })).toEqual({ disabled: false, reason: null });
  });
});

describe("draft limits", () => {
  it("counts characters after trimming, like the server", () => {
    expect(draftLength("  hi  ")).toBe(2);
    expect(draftLength("\u{1F600}".repeat(3))).toBe(3);
  });
  it("flags empty and too long drafts", () => {
    expect(draftProblem("   ")).toBe("empty");
    expect(draftProblem("a".repeat(MAX_DRAFT_CHARS))).toBeNull();
    expect(draftProblem("a".repeat(MAX_DRAFT_CHARS + 1))).toBe("too_long");
  });
});

describe("draftTemplate", () => {
  it("uses placeholders, never a hardcoded owner", () => {
    const t = draftTemplate({ name: "Alum", company: "Acme", title: "SDE" });
    expect(t).toContain(NAME_PLACEHOLDER);
    expect(t).toContain(COLLEGE_PLACEHOLDER);
    expect(t).not.toMatch(/Adhyan|VIT/);
  });
  it("uses the owner's details when the profile has them", () => {
    const t = draftTemplate({ name: "Alum", company: "Acme", title: "SDE" }, { name: "Sam", college: "State U" });
    expect(t).toContain("State U");
    expect(t.trim().endsWith("Sam")).toBe(true);
  });
});

describe("toQuery", () => {
  it("sends nothing but a limit by default", () => {
    expect(toQuery(EMPTY_FILTERS)).toEqual({ limit: 200 });
  });
  it("maps each filter to the server's params", () => {
    expect(
      toQuery({ gradYearMax: "2022", roleFamily: "operations", company: " Acme ", country: "India", dnc: "hide" }),
    ).toEqual({ limit: 200, grad_year_max: 2022, role_family: "operations", company: "Acme", country: "India", do_not_contact: false });
    expect(toQuery({ ...EMPTY_FILTERS, dnc: "only" }).do_not_contact).toBe(true);
  });
  it("drops a year the server would reject", () => {
    expect(toQuery({ ...EMPTY_FILTERS, gradYearMax: "20" }).grad_year_max).toBeUndefined();
  });
  it("maps the stored-classification filters and drops bad confidence", () => {
    expect(toQuery({ ...EMPTY_FILTERS, graduation: "borderline_2022", fn: "technology", contactability: "profile_only", minConfidence: "70" })).toEqual({
      limit: 200,
      graduation_eligibility: "borderline_2022",
      professional_function: "technology",
      contactability_status: "profile_only",
      min_classification_confidence: 70,
    });
    expect(toQuery({ ...EMPTY_FILTERS, minConfidence: "150" }).min_classification_confidence).toBeUndefined();
  });
  it("maps campaign filters correctly", () => {
    expect(toQuery({ campus: "vellore", acm: "confirmed", priority: "P1", search: "sde" })).toEqual({
      limit: 200,
      campus: "vellore",
      acm_vit_affiliation: "confirmed",
      priority_tier: "P1",
      search: "sde",
    });
  });
});

describe("badges", () => {
  it("returns correct priority tier badges", () => {
    expect(priorityTierBadge("P1").label).toContain("P1");
    expect(priorityTierBadge("P2").label).toContain("P2");
    expect(priorityTierBadge("P3").label).toContain("P3");
    expect(priorityTierBadge("P4").label).toContain("P4");
    expect(priorityTierBadge("REVIEW").label).toContain("REVIEW");
  });

  it("returns correct graduation eligibility badges", () => {
    expect(gradEligibilityBadge("eligible").label).toContain("Eligible");
    expect(gradEligibilityBadge("borderline_2022").label).toContain("Borderline");
    expect(gradEligibilityBadge("ineligible").label).toContain("Ineligible");
    expect(gradEligibilityBadge("unknown").label).toContain("Unknown");
  });
});

describe("messages", () => {
  it("summarises import counts", () => {
    expect(importSummary({ created: 4, updated: 2, skipped: 1 })).toBe("Imported: 4 new, 2 updated, 1 skipped.");
  });
  it("explains a 409", () => {
    expect(outreachErrorMessage({ status: 409, message: "x" })).toMatch(/do not contact/);
    expect(outreachErrorMessage(new Error("boom"))).toBe("boom");
  });
});

