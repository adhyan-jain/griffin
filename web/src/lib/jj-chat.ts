/**
 * Pure state and helpers for the JJ chat: a per-message state machine, reply extraction from a task's
 * output_payload, history truncation, the poll cadence and a tiny safe markdown parser.
 * No React and no network here, so every rule is unit-tested.
 */
import type { DeviceNote, JJHistoryTurn, JJMessageResult, RunnerWake, SubmitResult, Task } from "@/lib/api";

export interface Provider {
  name?: string;
  version?: string;
  model?: string;
}

/** Lifecycle of one message: sending -> waiting -> done | failed (or plan for objectives). */
export type Reply =
  | { phase: "sending" }
  | { phase: "waiting"; taskId: string; wake?: RunnerWake }
  | { phase: "done"; text: string; provider?: Provider }
  | { phase: "failed"; reason: string }
  | { phase: "plan"; result: SubmitResult; planOnly: boolean; needsDevice?: DeviceNote };

export interface Turn {
  id: number;
  text: string;
  reply: Reply;
}

export type ReplyEvent =
  | { type: "accepted"; result: JJMessageResult }
  | { type: "fallback"; result: SubmitResult }
  | { type: "task"; task: Task }
  | { type: "error"; reason: string }
  | { type: "retry" };

export type Extracted = { kind: "pending" } | { kind: "done"; text: string; provider?: Provider } | { kind: "failed"; reason: string };

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);
const nonEmptyString = (v: unknown): v is string => typeof v === "string" && v.trim() !== "";
const responseOf = (payload: unknown): string | null => (isRecord(payload) && nonEmptyString(payload.response) ? payload.response : null);

function providerOf(payload: unknown): Provider | undefined {
  const p = isRecord(payload) ? payload.provider : undefined;
  if (!isRecord(p)) return undefined;
  const str = (v: unknown) => (typeof v === "string" && v ? v : undefined);
  const out = { name: str(p.name), version: str(p.version), model: str(p.model) };
  return out.name || out.version || out.model ? out : undefined;
}

/** What a conversation reply task means for the chat. A COMPLETED task with no text is a failure. */
export function extractReply(task: Task): Extracted {
  switch (task.status) {
    case "COMPLETED": {
      const text = responseOf(task.output_payload);
      if (text === null) return { kind: "failed", reason: "JJ finished but returned no answer." };
      return { kind: "done", text, provider: providerOf(task.output_payload) };
    }
    case "FAILED":
    case "CANCELLED":
      return { kind: "failed", reason: task.failure_reason || "JJ could not answer." };
    default:
      return { kind: "pending" };
  }
}

export type StepResult = { kind: "text"; text: string } | { kind: "empty" } | { kind: "failed"; reason: string };

/** Result of one plan step; null while unfinished. Empty output is never presented as success. */
export function stepResult(task: Task): StepResult | null {
  if (task.status === "COMPLETED") {
    const text = responseOf(task.output_payload);
    return text === null ? { kind: "empty" } : { kind: "text", text };
  }
  if (task.status === "FAILED" || task.status === "CANCELLED") {
    return { kind: "failed", reason: task.failure_reason || "This step did not finish." };
  }
  return null;
}

export function reduceReply(state: Reply, ev: ReplyEvent): Reply {
  switch (ev.type) {
    case "accepted": {
      if (state.phase !== "sending") return state;
      const r = ev.result;
      if (r.kind === "conversation") return { phase: "waiting", taskId: r.task_id, wake: r.runner };
      return { phase: "plan", result: { orchestration: r.orchestration, plan: r.plan }, planOnly: false, needsDevice: r.needs_device };
    }
    case "fallback":
      return state.phase === "sending" ? { phase: "plan", result: ev.result, planOnly: true } : state;
    case "task": {
      if (state.phase !== "waiting") return state; // terminal states never change: no duplicate replies
      const x = extractReply(ev.task);
      if (x.kind === "pending") return state;
      return x.kind === "done" ? { phase: "done", text: x.text, provider: x.provider } : { phase: "failed", reason: x.reason };
    }
    case "error":
      return state.phase === "sending" || state.phase === "waiting" ? { phase: "failed", reason: ev.reason } : state;
    case "retry":
      return state.phase === "failed" ? { phase: "sending" } : state;
  }
}

export const HISTORY_TURNS = 10;

/** The last <=10 chat turns (user text plus finished assistant text) to send as context. */
export function buildHistory(turns: Turn[]): JJHistoryTurn[] {
  const h: JJHistoryTurn[] = [];
  for (const t of turns) {
    if (t.reply.phase !== "done" && t.reply.phase !== "plan") continue; // unanswered turns are not context
    h.push({ role: "user", text: t.text });
    if (t.reply.phase === "done") h.push({ role: "assistant", text: t.reply.text });
  }
  return h.slice(-HISTORY_TURNS);
}

/** 2s for the first 30s of waiting, then 5s. */
export const pollIntervalMs = (elapsedMs: number) => (elapsedMs < 30_000 ? 2000 : 5000);

export type Block = { type: "p"; text: string } | { type: "ul" | "ol"; items: string[] } | { type: "code"; text: string };

/** Paragraphs, bullet/numbered lists and fenced code only. Output is plain text for React to escape. */
export function parseBlocks(src: string): Block[] {
  const blocks: Block[] = [];
  const lines = src.replaceAll("\r\n", "\n").split("\n");
  let para: string[] = [];
  const flush = () => {
    if (para.length) blocks.push({ type: "p", text: para.join("\n") });
    para = [];
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (line.trim().startsWith("```")) {
      flush();
      const code: string[] = [];
      for (i++; i < lines.length && !lines[i].trim().startsWith("```"); i++) code.push(lines[i]);
      blocks.push({ type: "code", text: code.join("\n") });
      continue;
    }
    const bullet = /^\s*[-*]\s+(.*)$/.exec(line);
    const numbered = /^\s*\d+[.)]\s+(.*)$/.exec(line);
    const m = bullet ?? numbered;
    if (m) {
      flush();
      const type = bullet ? "ul" : "ol";
      const last = blocks.at(-1);
      if (last && last.type === type) last.items.push(m[1]);
      else blocks.push({ type, items: [m[1]] });
      continue;
    }
    if (line.trim() === "") flush();
    else para.push(line);
  }
  flush();
  return blocks;
}
