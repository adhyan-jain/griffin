import { describe, expect, it } from "vitest";
import { taskKind } from "./task-kinds";

const t = (title: string, goal = "", input_payload: unknown = {}) => ({ title, goal, input_payload });

describe("taskKind", () => {
  it("finds reminders", () => {
    expect(taskKind(t("REMINDER - call mom"))).toBe("reminder");
    expect(taskKind(t("call mom", "", { kind: "reminder" }))).toBe("reminder");
  });
  it("finds routines", () => {
    expect(taskKind(t("INTERNSHIP TRACKER - LinkedIn"))).toBe("routine");
    expect(taskKind(t("ALUMNI DM BATCH 3"))).toBe("routine");
    expect(taskKind(t("DSA COACH (daily 21:00)"))).toBe("routine");
    expect(taskKind(t("anything", "", { cadence: "daily" }))).toBe("routine");
  });
  it("finds system rows", () => {
    expect(taskKind(t("ALUMNI DM - Asha"))).toBe("system");
    expect(taskKind(t("APPLY - Acme"))).toBe("system");
    expect(taskKind(t("x", "Merged tracker snapshot of 3"))).toBe("system");
    expect(taskKind(t("x", "", { kind: "tracker_snapshot" }))).toBe("system");
  });
  it("defaults to request and tolerates null payload", () => {
    expect(taskKind(t("Write my resume"))).toBe("request");
    expect(taskKind(t("Write my resume", "", null))).toBe("request");
  });
});

describe("taskKind owner wording", () => {
  it("keeps an owner's request that names a tracker as a request", () => {
    expect(taskKind({ title: "ok, so none of the 19 still shows on internship tracker", goal: "", input_payload: null })).toBe("request");
    expect(taskKind({ title: "Chat: update the HACKATHON TRACKER", goal: "", input_payload: { kind: "chat" } })).toBe("request");
  });
});
