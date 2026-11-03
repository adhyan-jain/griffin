import { describe, expect, it } from "vitest";
import type { SystemStatus } from "@/lib/api";
import { fixtures } from "@/test/fixtures";
import { overallState } from "@/lib/system-status";

const base = fixtures.systemStatus as SystemStatus;
const allOk = (): SystemStatus => ({
  ...base,
  components: Object.fromEntries(Object.keys(base.components).map((k) => [k, { state: "ok", evidence: "x", observed_at: null }])),
});

describe("overallState", () => {
  it("is unavailable when the endpoint gave nothing (never green)", () => {
    expect(overallState(null)).toBe("unavailable");
  });
  it("down beats degraded beats unknown", () => {
    expect(overallState(base)).toBe("down");
    const s = allOk();
    s.components.worker = { state: "degraded", evidence: "x", observed_at: null };
    s.components.provider = { state: "unknown", evidence: "x", observed_at: null };
    expect(overallState(s)).toBe("degraded");
  });
  it("is ok only when every component is ok", () => {
    expect(overallState(allOk())).toBe("ok");
    const s = allOk();
    delete s.components.runner;
    expect(overallState(s)).toBe("unknown");
  });
});
