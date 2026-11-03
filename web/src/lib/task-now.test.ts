import { describe, expect, it } from "vitest";
import type { Task, TaskAttempt } from "@/lib/api";
import { fixtures } from "@/test/fixtures";
import { deriveNow } from "@/lib/task-now";

const task = fixtures.task as Task;
const attempt = (n: number, extra: Partial<TaskAttempt> = {}): TaskAttempt => ({
  id: `a${n}`, task_id: task.id, worker_id: `worker-${n}`, attempt_number: n, status: "FAILED",
  prompt_tokens: 0, completion_tokens: 0, duration_ms: 1, started_at: "2026-11-01T10:10:00Z", ...extra,
});

describe("deriveNow", () => {
  it("maps status to a plain-language stage", () => {
    expect(deriveNow({ ...task, status: "AWAITING_APPROVAL" }, [], []).stage).toBe("Waiting for your approval");
  });
  it("uses the newest attempt's worker and the latest timestamp", () => {
    const n = deriveNow(task, [], [attempt(1), attempt(2, { started_at: "2026-11-02T00:00:00Z" })]);
    expect(n.worker).toBe("worker-2");
    expect(n.lastActivityAt).toBe("2026-11-02T00:00:00Z");
  });
  it("reports retries only when there were some", () => {
    expect(deriveNow(task, [], []).retry).toBeNull();
    expect(deriveNow({ ...task, retry_count: 2, max_retries: 3 }, [], []).retry).toEqual({ count: 2, max: 3 });
  });
  it("falls back to the attempt error for failed tasks and never exposes event messages", () => {
    const n = deriveNow({ ...task, status: "FAILED" }, [{ id: 1, task_id: task.id, event_type: "LOG", message: "secret reasoning", created_at: task.created_at }], [attempt(1, { error_message: "worker crashed" })]);
    expect(n.failureReason).toBe("worker crashed");
    expect(JSON.stringify(n)).not.toContain("secret reasoning");
  });
});
