import { act, createElement } from "react";
import { createRoot } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { ApplicationPackage } from "@/lib/api";
import { PackageBadge, PackageView } from "./application-package";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const pkg = (over: Partial<ApplicationPackage> = {}): ApplicationPackage => ({
  id: "p1",
  listing_id: "l1",
  resume_file: "/home/x/Resumes/Resume_Backend.pdf",
  role_class: "backend",
  selected_projects: [{ project: "ComputeHive", facts: [{ id: "f1", text: "built a distributed task runner in Go with Postgres" }], skills: ["Go", "PostgreSQL"], quote: "build Go services backed by Postgres" }],
  cover_letter: "Dear Hiring Team,\n\nI am applying for the Backend Intern role at Acme.\n\nSincerely,\nAsha Verma",
  answers: [{ question: "What is your graduation year?", answer: "2028", source: "your profile: expected graduation", quote: "your graduation year" }],
  required_documents: [
    { name: "Resume", status: "ready", note: "Resume_Backend.pdf" },
    { name: "Transcript", status: "missing", note: "Griffin does not hold your transcript" },
  ],
  deadline: "2026-12-03T00:00:00Z",
  missing_information: [],
  evidence: [{ reason: "ComputeHive matches the posting on Go", quote: "build Go services backed by Postgres" }],
  status: "ready_for_approval",
  approval_id: "a1",
  model_used: "gemini-flash-latest",
  model_calls: 1,
  created_at: "2026-11-23T04:30:00Z",
  updated_at: "2026-11-23T04:30:00Z",
  ...over,
});

const html = (p: ApplicationPackage, onDecide?: (a: boolean) => void) =>
  renderToStaticMarkup(createElement(PackageView, { pkg: p, onDecide, now: new Date("2026-11-23T10:00:00Z") }));

describe("PackageView", () => {
  it("shows every part of the package with its evidence", () => {
    const out = html(pkg(), () => {});
    expect(out).toContain("Resume_Backend.pdf");
    expect(out).toContain("ComputeHive");
    expect(out).toContain("build Go services backed by Postgres"); // the posting's words that matched
    expect(out).toContain("Dear Hiring Team");
    expect(out).toContain("What is your graduation year?");
    expect(out).toContain("From your profile: expected graduation");
    expect(out).toContain("Transcript");
    expect(out).toContain("Griffin does not hold your transcript");
    expect(out).toContain("3 Dec 2026, 10 days left");
    expect(out).toContain("Evidence from the posting (1)");
  });

  it("offers Approve and Reject, and says approving sends nothing", () => {
    const out = html(pkg(), () => {});
    expect(out).toContain("Approve package");
    expect(out).toContain("Reject");
    expect(out).toContain("Approving does not send anything");
  });

  it("lists what is missing and offers no decision while the owner has things to supply", () => {
    const out = html(pkg({ status: "needs_owner", approval_id: null, missing_information: ["Notice period: your profile does not say", "Transcript: Griffin does not hold your transcript"] }), () => {});
    expect(out).toContain("Only you can supply this");
    expect(out).toContain("Notice period: your profile does not say");
    expect(out).toContain("Nothing was invented to fill the gap");
    expect(out).not.toContain("Approve package");
    expect(out).toContain("cannot be approved until the missing items");
  });

  it("says plainly when no letter was stored and when no project matches", () => {
    const out = html(pkg({ cover_letter: "", selected_projects: [], status: "needs_owner", approval_id: null, missing_information: ["The cover letter could not be written"] }));
    expect(out).toContain("No letter was stored");
    expect(out).toContain("none is featured");
  });

  it("shows a decided package without buttons", () => {
    expect(html(pkg({ status: "approved" }), () => {})).toContain("You approved this package. Nothing has been sent.");
    expect(html(pkg({ status: "approved" }), () => {})).not.toContain("Approve package");
  });

  it("calls onDecide with the owner's choice", () => {
    const onDecide = vi.fn();
    const host = document.createElement("div");
    document.body.appendChild(host);
    const root = createRoot(host);
    act(() => root.render(createElement(PackageView, { pkg: pkg(), onDecide })));
    const buttons = [...host.querySelectorAll("button")];
    const approve = buttons.find((b) => b.textContent?.includes("Approve package"));
    const reject = buttons.find((b) => b.textContent?.includes("Reject"));
    expect(approve && reject).toBeTruthy();
    act(() => approve!.click());
    act(() => reject!.click());
    expect(onDecide.mock.calls).toEqual([[true], [false]]);
    act(() => root.unmount());
    host.remove();
  });

  it("disables both buttons while a decision is being recorded", () => {
    const host = document.createElement("div");
    document.body.appendChild(host);
    const root = createRoot(host);
    act(() => root.render(createElement(PackageView, { pkg: pkg(), onDecide: () => {}, busy: true })));
    expect([...host.querySelectorAll("button")].every((b) => b.disabled)).toBe(true);
    act(() => root.unmount());
    host.remove();
  });
});

describe("PackageBadge", () => {
  it("shows the package status on a listing card", () => {
    const out = renderToStaticMarkup(createElement(PackageBadge, { brief: { id: "p1", status: "needs_owner", approval_id: null, missing_count: 2 } }));
    expect(out).toContain("Needs your input: 2 things");
    const ready = renderToStaticMarkup(createElement(PackageBadge, { brief: { id: "p1", status: "ready_for_approval", approval_id: "a1", missing_count: 0 } }));
    expect(ready).toContain("Waiting for your approval");
  });
});
