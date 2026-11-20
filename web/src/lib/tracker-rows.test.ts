import { describe, expect, it } from "vitest";
import { listingKey, mergeTables, removeListing, splitRows } from "@/lib/tracker-rows";

const H = ["Company", "Role", "Deadline", "Application Link"];
const t = (rows: string[][], headers = H) => ({ title: "t", headers, rows });

describe("mergeTables", () => {
  it("appends rows from a second task instead of replacing the first", () => {
    const m = mergeTables([t([["Acme", "SDE Intern", "30 Dec 2026", "https://a"]]), t([["Beta", "ML Intern", "", ""]])])!;
    expect(m.rows.map((r) => r[0])).toEqual(["Acme", "Beta"]);
  });
  it("the same listing found twice is one row, and a newer run fills the blanks", () => {
    const m = mergeTables([t([["Acme", "SDE Intern", "", ""]]), t([["ACME", "sde intern", "30 Dec 2026", "https://a"]])])!;
    expect(m.rows).toHaveLength(1);
    expect(m.rows[0][3]).toBe("https://a");
  });
  it("lines columns up by header when a run orders them differently", () => {
    const m = mergeTables([t([["Acme", "SDE Intern", "", ""]]), t([["https://a", "Acme", "SDE Intern"]], ["Application Link", "Company", "Role"])])!;
    expect(m.rows).toEqual([["Acme", "SDE Intern", "", "https://a"]]);
  });
});

describe("splitRows", () => {
  const today = new Date(2026, 10, 20);
  const table = t([
    ["Old", "Intern", "10 Nov 2026", ""],
    ["Live", "Intern", "30 Nov 2026", ""],
    ["Rolling", "Intern", "Rolling", ""],
    ["Done", "Intern", "30 Nov 2026", ""],
  ]);
  it("drops passed deadlines, keeps undated ones, and files applied rows separately", () => {
    const s = splitRows(table, today, new Set([listingKey("Done", "Intern")]));
    expect(s.passed.map((r) => r[0])).toEqual(["Old"]);
    expect(s.open.map((r) => r[0])).toEqual(["Live", "Rolling"]);
    expect(s.applied.map((r) => r[0])).toEqual(["Done"]);
  });
  it("an applied row with a passed deadline is still applied, not passed", () => {
    expect(splitRows(table, today, new Set([listingKey("Old", "Intern")])).applied.map((r) => r[0])).toEqual(["Old"]);
  });
});

describe("removeListing", () => {
  const md = "# Internships tracker\n\n| Company | Role | Tier |\n|---|---|---|\n| Acme | SDE Intern | A |\n| Beta | ML Intern | C |";
  it("takes out only the matching row and keeps the rest of the file", () => {
    const r = removeListing(md, "acme", "sde  intern");
    expect(r.removed).toBe(1);
    expect(r.text).toContain("# Internships tracker");
    expect(r.text).toContain("Beta");
    expect(r.text).not.toContain("Acme");
  });
  it("reports a removal that matched nothing and changes nothing", () => {
    const r = removeListing(md, "Nobody", "Intern");
    expect(r.removed).toBe(0);
    expect(r.text).toBe(md);
  });
});
