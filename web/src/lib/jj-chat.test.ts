import { describe, expect, it } from "vitest";
import type { Task } from "@/lib/api";
import { fixtures } from "@/test/fixtures";
import {
  buildHistory,
  extractReply,
  parseBlocks,
  pollIntervalMs,
  reduceReply,
  stepResult,
  type Reply,
  type Turn,
} from "@/lib/jj-chat";

const task = (over: Partial<Task>): Task => ({ ...(fixtures.jjReplyTask as unknown as Task), ...over });

describe("extractReply", () => {
  it("returns the persisted response and provider for a COMPLETED task", () => {
    const r = extractReply(task({}));
    expect(r).toEqual({ kind: "done", text: "Hello! How can I help?\n\n- plan a report\n- review a repo", provider: { name: "bedrock", version: "1", model: "claude-sonnet" } });
  });

  it.each([["QUEUED"], ["RUNNING"], ["RETRYING"], ["AWAITING_APPROVAL"]] as const)("%s is pending", (status) => {
    expect(extractReply(task({ status })).kind).toBe("pending");
  });

  it.each([[undefined], [null], [{}], [{ response: "" }], [{ response: "   " }], [{ response: 42 }], ["str"], [[1]]])(
    "COMPLETED with unusable output_payload %j is a failure, never a silent success",
    (output_payload) => {
      const r = extractReply(task({ output_payload }));
      expect(r.kind).toBe("failed");
      if (r.kind === "failed") expect(r.reason).toMatch(/no answer/i);
    },
  );

  it("ignores a malformed provider", () => {
    const r = extractReply(task({ output_payload: { response: "ok", provider: "x" } }));
    expect(r).toEqual({ kind: "done", text: "ok", provider: undefined });
  });

  it("FAILED and CANCELLED surface failure_reason, else a generic message", () => {
    expect(extractReply(task({ status: "FAILED", failure_reason: "model timeout" }))).toEqual({ kind: "failed", reason: "model timeout" });
    const g = extractReply(task({ status: "CANCELLED", failure_reason: null }));
    expect(g).toEqual({ kind: "failed", reason: "JJ could not answer." });
  });
});

describe("stepResult", () => {
  it("shows response text for completed steps", () => {
    expect(stepResult(task({ output_payload: { response: "done it" } }))).toEqual({ kind: "text", text: "done it" });
  });
  it("says a COMPLETED step with empty output finished without a result", () => {
    expect(stepResult(task({ output_payload: {} }))).toEqual({ kind: "empty" });
    expect(stepResult(task({ output_payload: null }))).toEqual({ kind: "empty" });
  });
  it("shows failure_reason for failed or cancelled steps", () => {
    expect(stepResult(task({ status: "FAILED", failure_reason: "boom" }))).toEqual({ kind: "failed", reason: "boom" });
    expect(stepResult(task({ status: "CANCELLED", failure_reason: undefined }))).toEqual({ kind: "failed", reason: "This step did not finish." });
  });
  it("is null while unfinished", () => {
    expect(stepResult(task({ status: "RUNNING" }))).toBeNull();
  });
});

describe("reduceReply", () => {
  const sending: Reply = { phase: "sending" };
  const conv = fixtures.jjConversation as never;

  it("sending -> waiting on a conversation result, carrying the runner wake", () => {
    const r = reduceReply(sending, { type: "accepted", result: { ...(conv as object), runner: { wake: "failed", detail: "x" } } as never });
    expect(r).toMatchObject({ phase: "waiting", taskId: fixtures.jjConversation.task_id, wake: { wake: "failed", detail: "x" } });
  });

  it("sending -> plan on an objective result", () => {
    const r = reduceReply(sending, { type: "accepted", result: fixtures.jjObjective as never });
    expect(r.phase).toBe("plan");
    if (r.phase === "plan") expect(r.planOnly).toBe(false);
  });

  it("fallback result is labelled plan only", () => {
    const r = reduceReply(sending, { type: "fallback", result: fixtures.submitResult as never });
    expect(r).toMatchObject({ phase: "plan", planOnly: true });
  });

  it("waiting -> done once, and a second COMPLETED poll changes nothing (no duplicates)", () => {
    const waiting = reduceReply(sending, { type: "accepted", result: conv });
    const done = reduceReply(waiting, { type: "task", task: task({}) });
    expect(done.phase).toBe("done");
    expect(reduceReply(done, { type: "task", task: task({}) })).toBe(done);
  });

  it("stays waiting on non-terminal tasks (same object, no re-render churn)", () => {
    const waiting = reduceReply(sending, { type: "accepted", result: conv });
    expect(reduceReply(waiting, { type: "task", task: task({ status: "RUNNING" }) })).toBe(waiting);
  });

  it("waiting -> failed with the reason; failed is terminal", () => {
    const waiting = reduceReply(sending, { type: "accepted", result: conv });
    const failed = reduceReply(waiting, { type: "task", task: task({ status: "FAILED", failure_reason: "nope" }) });
    expect(failed).toEqual({ phase: "failed", reason: "nope" });
    expect(reduceReply(failed, { type: "task", task: task({}) })).toBe(failed);
  });

  it("send errors move sending -> failed but never overwrite a finished reply", () => {
    expect(reduceReply(sending, { type: "error", reason: "offline" })).toEqual({ phase: "failed", reason: "offline" });
    const done: Reply = { phase: "done", text: "hi" };
    expect(reduceReply(done, { type: "error", reason: "late" })).toBe(done);
  });

  it("retry resets to sending only from failed", () => {
    expect(reduceReply({ phase: "failed", reason: "x" }, { type: "retry" })).toEqual({ phase: "sending" });
    const done: Reply = { phase: "done", text: "hi" };
    expect(reduceReply(done, { type: "retry" })).toBe(done);
  });
});

describe("buildHistory", () => {
  const turn = (i: number, reply: Reply): Turn => ({ id: i, text: `q${i}`, reply });
  it("includes user text and finished assistant text, skips unfinished and failed replies", () => {
    const h = buildHistory([
      turn(1, { phase: "done", text: "a1" }),
      turn(2, { phase: "failed", reason: "x" }),
      turn(3, { phase: "waiting", taskId: "t" }),
    ]);
    expect(h).toEqual([
      { role: "user", text: "q1" },
      { role: "assistant", text: "a1" },
    ]);
  });
  it("keeps only the last 10 turns", () => {
    const turns = Array.from({ length: 12 }, (_, i) => turn(i + 1, { phase: "done", text: `a${i + 1}` }));
    const h = buildHistory(turns);
    expect(h).toHaveLength(10);
    expect(h[0]).toEqual({ role: "user", text: "q8" });
    expect(h.at(-1)).toEqual({ role: "assistant", text: "a12" });
  });
  it("a plan turn contributes the user text only", () => {
    const h = buildHistory([turn(1, { phase: "plan", result: fixtures.submitResult as never, planOnly: true })]);
    expect(h).toEqual([{ role: "user", text: "q1" }]);
  });
});

describe("pollIntervalMs", () => {
  it("is 2s for the first 30s and 5s afterwards", () => {
    expect(pollIntervalMs(0)).toBe(2000);
    expect(pollIntervalMs(29_999)).toBe(2000);
    expect(pollIntervalMs(30_000)).toBe(5000);
  });
});

describe("parseBlocks", () => {
  it("splits paragraphs, bullet lists, ordered lists and code fences without HTML", () => {
    const blocks = parseBlocks("Intro line\nsecond line\n\n- one\n* two\n\n1. first\n2. second\n\n```\n<b>x</b>\n```");
    expect(blocks).toEqual([
      { type: "p", text: "Intro line\nsecond line" },
      { type: "ul", items: ["one", "two"] },
      { type: "ol", items: ["first", "second"] },
      { type: "code", text: "<b>x</b>" },
    ]);
  });
  it("returns no blocks for blank input", () => {
    expect(parseBlocks("  \n ")).toEqual([]);
  });
});
