import { describe, expect, it } from "vitest";
import type { HostView, Task } from "@/lib/api";
import { fixtures } from "@/test/fixtures";
import { describeMode, displayWorker, effectiveSummary, hostStatus, lastSeen, laptopSetup, requiredCapabilities, waitingForDevice } from "@/lib/hosts";

const task = fixtures.task as Task;
const NOW = new Date("2026-11-07T12:00:00Z").getTime();

describe("lastSeen", () => {
  it("reads in plain units, never raw timestamps", () => {
    expect(lastSeen(null, NOW)).toBe("never");
    expect(lastSeen("2026-11-07T11:59:40Z", NOW)).toBe("just now");
    expect(lastSeen("2026-11-07T11:55:00Z", NOW)).toBe("5 min ago");
    expect(lastSeen("2026-11-07T09:00:00Z", NOW)).toBe("3 h ago");
    expect(lastSeen("2026-11-04T12:00:00Z", NOW)).toBe("3 d ago");
  });
  it("does not go negative when clocks disagree", () => {
    expect(lastSeen("2026-11-07T12:00:30Z", NOW)).toBe("just now");
  });
});

describe("describeMode", () => {
  it("says which device is primary and what that means", () => {
    expect(describeMode("laptop").title).toMatch(/laptop is primary/i);
    expect(describeMode("phone").title).toMatch(/phone is primary/i);
    expect(describeMode("none").detail).toMatch(/wait/i);
  });
});

describe("hostStatus", () => {
  it("shows a pause before anything else", () => {
    expect(hostStatus({ live: true, paused: true, last_seen_at: null }, NOW).label).toBe("Paused");
  });
  it("shows online, and offline with when it was last seen", () => {
    expect(hostStatus({ live: true, paused: false, last_seen_at: "2026-11-07T11:59:50Z" }, NOW).tone).toBe("ok");
    expect(hostStatus({ live: false, paused: false, last_seen_at: "2026-11-07T11:00:00Z" }, NOW).label).toBe("Offline, last seen 1 h ago");
  });
});

describe("effectiveSummary", () => {
  const h = (over: Partial<HostView>) => ({ capabilities: ["agent", "browser"], effective_capabilities: ["agent"], live: true, ...over }) as HostView;
  it("shows only what is both granted and reported while online", () => {
    expect(effectiveSummary(h({}))).toBe("Run the assistant");
  });
  it("shows what is granted while offline", () => {
    expect(effectiveSummary(h({ live: false }))).toBe("Run the assistant, Use a browser");
  });
  it("says so when nothing is granted or nothing matches", () => {
    expect(effectiveSummary(h({ capabilities: [] }))).toMatch(/no permissions/i);
    expect(effectiveSummary(h({ effective_capabilities: [] }))).toMatch(/nothing it reports matches/i);
  });
});

describe("waiting for a device", () => {
  const t = (over: Partial<Task>) => ({ ...task, ...over }) as Task;
  it("reads the declared requirements and ignores junk", () => {
    expect(requiredCapabilities(t({ input_payload: { requires: ["browser", "nope", 3] } }))).toEqual(["browser"]);
    expect(requiredCapabilities(t({ input_payload: null }))).toEqual([]);
    expect(requiredCapabilities(t({ input_payload: { requires: "browser" } }))).toEqual([]);
  });
  it("names the laptop for browser steps and the phone for phone-only steps", () => {
    expect(waitingForDevice(t({ status: "QUEUED", input_payload: { requires: ["browser"] } }))).toBe("your laptop");
    expect(waitingForDevice(t({ status: "QUEUED", input_payload: { requires: ["phone_apps"] } }))).toBe("your phone");
  });
  it("is silent for steps that are running, done, or need nothing", () => {
    expect(waitingForDevice(t({ status: "RUNNING", input_payload: { requires: ["browser"] } }))).toBeNull();
    expect(waitingForDevice(t({ status: "COMPLETED", input_payload: { requires: ["browser"] } }))).toBeNull();
    expect(waitingForDevice(t({ status: "QUEUED", input_payload: {} }))).toBeNull();
  });
});

describe("laptopSetup", () => {
  it("passes the token by file and never inline", () => {
    const cmd = laptopSetup("https://api.example.com");
    expect(cmd).toContain("GRIFFIN_HOST_TOKEN_FILE");
    expect(cmd).not.toMatch(/GRIFFIN_HOST_TOKEN=/);
    expect(cmd).toContain("https://api.example.com");
  });
});

describe("displayWorker", () => {
  it("hides raw device ids and keeps cloud workers as they are", () => {
    expect(displayWorker("host:3f2a1c9e-0000-4000-8000-000000000000")).toBe("Your device");
    expect(displayWorker("griffin-worker-1")).toBe("griffin-worker-1");
    expect(displayWorker(null)).toBeNull();
  });
});
