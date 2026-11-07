import { describe, expect, it } from "vitest";
import type { Policy } from "@/lib/api";
import {
  appRules, classMode, contactRules, folderError, folderKind, folderRules, fullLabel, fullRemainingMs,
  removeRule, setAppMode, setClassMode, setContactMode, setFolder, setFull,
} from "@/lib/permissions";

const base: Policy = { default: "ask", rules: [{ class: "read", mode: "auto" }, { class: "draft", mode: "auto" }] };
const NOW = new Date("2026-11-07T12:00:00Z").getTime();

describe("class modes", () => {
  it("reads the class rule, else the default", () => {
    expect(classMode(base, "read")).toBe("auto");
    expect(classMode(base, "send")).toBe("ask");
  });
  it("replaces rather than duplicates", () => {
    const next = setClassMode(setClassMode(base, "send", "off"), "send", "ask");
    expect(next.rules.filter((r) => r.class === "send")).toHaveLength(1);
    expect(classMode(next, "send")).toBe("ask");
  });
  it("never offers automatic buying or calling, matching the server's floor", () => {
    expect(classMode(setClassMode(base, "buy", "auto"), "buy")).toBe("ask");
    expect(classMode(setClassMode(base, "call", "auto"), "call")).toBe("ask");
    expect(classMode(setClassMode(base, "buy", "off"), "buy")).toBe("off");
  });
  it("does not mutate its input", () => {
    const before = JSON.stringify(base);
    setClassMode(base, "send", "auto");
    expect(JSON.stringify(base)).toBe(before);
  });
});

describe("app and contact rules narrow sending", () => {
  it("an app rule is app + send, so it outranks the everywhere-rule for sending", () => {
    const p = setAppMode(setClassMode(base, "send", "ask"), "  WhatsApp ", "auto");
    expect(appRules(p)).toEqual([{ app: "whatsapp", class: "send", mode: "auto" }]);
  });
  it("a contact rule is contact + send", () => {
    const p = setContactMode(base, " Mom", "auto");
    expect(contactRules(p)).toEqual([{ contact: "mom", class: "send", mode: "auto" }]);
  });
  it("ignores blank names", () => {
    expect(setAppMode(base, "  ", "auto")).toBe(base);
    expect(setContactMode(base, "", "auto")).toBe(base);
  });
  it("updating an app replaces its rule", () => {
    const p = setAppMode(setAppMode(base, "slack", "auto"), "slack", "off");
    expect(appRules(p)).toHaveLength(1);
    expect(appRules(p)[0].mode).toBe("off");
  });
});

describe("folders", () => {
  it("validates like the server", () => {
    expect(folderError("docs")).toMatch(/full path/i);
    expect(folderError("/home/me/../root")).toMatch(/\.\./);
    expect(folderError("/")).toMatch(/specific/i);
    expect(folderError("//")).toMatch(/specific/i);
    expect(folderError("/home/me/docs")).toBeNull();
  });
  it("read-without-asking and never are distinct rules, cleaned of stray slashes", () => {
    let p = setFolder(base, "/home/me//docs/", "read");
    p = setFolder(p, "/home/me/private", "never");
    const rules = folderRules(p);
    expect(rules).toHaveLength(2);
    expect(rules.find((r) => r.folder === "/home/me/docs")).toMatchObject({ class: "read", mode: "auto" });
    expect(folderKind(rules.find((r) => r.folder === "/home/me/private")!)).toBe("never");
  });
  it("switching a folder replaces its rule", () => {
    const p = setFolder(setFolder(base, "/home/me/x", "read"), "/home/me/x", "never");
    expect(folderRules(p)).toHaveLength(1);
    expect(folderKind(folderRules(p)[0])).toBe("never");
  });
  it("removing a rule removes only that rule", () => {
    const p = setFolder(setFolder(base, "/a", "read"), "/b", "never");
    const gone = removeRule(p, folderRules(p).find((r) => r.folder === "/a")!);
    expect(folderRules(gone).map((r) => r.folder)).toEqual(["/b"]);
    expect(classMode(gone, "read")).toBe("auto");
  });
});

describe("full mode", () => {
  it("sets a time box and clears it", () => {
    const on = setFull(base, 2, NOW);
    expect(fullRemainingMs(on, NOW)).toBe(2 * 3600_000);
    expect(fullRemainingMs(setFull(on, null, NOW), NOW)).toBe(0);
  });
  it("expires by itself", () => {
    expect(fullRemainingMs(setFull(base, 1, NOW), NOW + 2 * 3600_000)).toBe(0);
  });
  it("says what is left in plain words", () => {
    expect(fullLabel(0)).toBe("Off");
    expect(fullLabel(90 * 60_000)).toBe("On, 1 h 30 min left");
    expect(fullLabel(20 * 60_000)).toBe("On, 20 min left");
  });
});
