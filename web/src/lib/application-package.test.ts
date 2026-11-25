import { describe, expect, it } from "vitest";
import type { PackageBrief } from "@/lib/api";
import {
  canDecide,
  deadlineLabel,
  packageAction,
  packageApprovalInfo,
  packageBadgeLabel,
  packageIdOfApproval,
  packageTone,
  resumeName,
} from "./application-package";

const brief = (status: PackageBrief["status"], missing = 0): PackageBrief => ({ id: "p1", status, approval_id: status === "ready_for_approval" ? "a1" : null, missing_count: missing });

describe("package badge", () => {
  it("says what the owner has to do", () => {
    expect(packageBadgeLabel(brief("ready_for_approval"))).toBe("Waiting for your approval");
    expect(packageBadgeLabel(brief("needs_owner", 3))).toBe("Needs your input: 3 things");
    expect(packageBadgeLabel(brief("needs_owner", 1))).toBe("Needs your input: 1 thing");
    expect(packageBadgeLabel(brief("approved"))).toBe("Package approved");
  });
  it("marks a waiting package as the one to act on and a rejected one as bad", () => {
    expect(packageTone("ready_for_approval")).toBe("default");
    expect(packageTone("rejected")).toBe("destructive");
    expect(packageTone("needs_owner")).toBe("outline");
  });
});

describe("the action on a listing card", () => {
  it("offers Prepare application only for an open listing without a package", () => {
    expect(packageAction({ stage: "verified", package: null })).toEqual({ kind: "prepare", label: "Prepare application" });
    expect(packageAction({ stage: "closed", package: null })).toBeNull();
    expect(packageAction({ stage: "approved", package: null })).toBeNull();
  });
  it("opens a package that waits for approval and re-checks one that needs the owner", () => {
    expect(packageAction({ stage: "drafted", package: brief("ready_for_approval") })).toEqual({ kind: "view", label: "Review package" });
    expect(packageAction({ stage: "drafted", package: brief("needs_owner", 2) })).toEqual({ kind: "retry", label: "Check again" });
    expect(packageAction({ stage: "verified", package: brief("rejected") })).toEqual({ kind: "retry", label: "Prepare again" });
    expect(packageAction({ stage: "approved", package: brief("approved") })).toEqual({ kind: "view", label: "View package" });
  });
  it("never offers to prepare a listing that is already being submitted or applied to", () => {
    expect(packageAction({ stage: "submitting", package: brief("approved") })?.kind).toBe("view");
    expect(packageAction({ stage: "confirmed", package: null })).toBeNull();
  });
});

describe("deciding", () => {
  it("is possible only for a complete package that has its approval", () => {
    expect(canDecide({ status: "ready_for_approval", approval_id: "a1" })).toBe(true);
    expect(canDecide({ status: "ready_for_approval", approval_id: null })).toBe(false);
    expect(canDecide({ status: "needs_owner", approval_id: null })).toBe(false);
    expect(canDecide({ status: "approved", approval_id: "a1" })).toBe(false);
  });
});

describe("deadline", () => {
  const now = new Date("2026-11-23T10:00:00Z");
  it("counts the days left or gone", () => {
    expect(deadlineLabel("2026-11-28T10:00:00Z", now)).toBe("28 Nov 2026, 5 days left");
    expect(deadlineLabel("2026-11-21T10:00:00Z", now)).toBe("21 Nov 2026, passed 2 days ago");
    expect(deadlineLabel("2026-11-23T12:00:00Z", now)).toBe("23 Nov 2026, today");
  });
  it("is absent without one", () => {
    expect(deadlineLabel(null)).toBeNull();
    expect(deadlineLabel("not a date")).toBeNull();
  });
});

describe("approval payload", () => {
  const payload = { package_id: "p1", listing_id: "l1", company: "Acme", role: "Backend Intern", portal_url: "https://acme.example/1", resume_path: "/r/Resume_Backend.pdf", projects: ["ComputeHive"] };
  it("reads the package an approval is about", () => {
    expect(packageIdOfApproval(payload)).toBe("p1");
    expect(packageIdOfApproval({ text: "hi" })).toBeNull();
    expect(packageIdOfApproval(null)).toBeNull();
    expect(packageApprovalInfo(payload)).toEqual({ company: "Acme", role: "Backend Intern", resume: "/r/Resume_Backend.pdf", projects: ["ComputeHive"], url: "https://acme.example/1" });
    expect(packageApprovalInfo({})).toBeNull();
  });
  it("names the resume by its file", () => {
    expect(resumeName("/home/x/Resume_Backend.pdf")).toBe("Resume_Backend.pdf");
  });
});
