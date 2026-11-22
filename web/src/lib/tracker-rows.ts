import type { ParsedTable } from "@/lib/excel-export";
import type { Listing } from "@/lib/api";

/** One listing is identified by its company and role, so the same posting found twice is one row. */
export const listingKey = (company: string, role: string) =>
  `${company}|${role}`.toLowerCase().replace(/[^a-z0-9|]+/g, " ").replace(/\s+/g, " ").trim();

const col = (headers: string[], ...needles: string[]) =>
  headers.findIndex((h) => needles.some((n) => h.toLowerCase().includes(n)));

export function rowKey(headers: string[], row: string[]): string {
  const c = col(headers, "company", "name");
  const r = col(headers, "role", "title", "position");
  return listingKey(c >= 0 ? row[c] ?? "" : row[0] ?? "", r >= 0 ? row[r] ?? "" : "");
}

/**
 * Appends the tables of several runs into one. Oldest first: a newer run fills in or updates a row the
 * older runs already had, and adds the rows they did not. Nothing is dropped, so tasks that each
 * gather part of the list build one list.
 */
export function mergeTables(tables: ParsedTable[]): ParsedTable | null {
  const usable = tables.filter((t) => t.headers.length > 0);
  if (usable.length === 0) return null;
  const headers = usable[0].headers;
  const rows = new Map<string, string[]>();
  for (const t of usable) {
    const idx = headers.map((h) => t.headers.findIndex((x) => x.trim().toLowerCase() === h.trim().toLowerCase()));
    for (const raw of t.rows) {
      const aligned = idx.map((i) => (i >= 0 ? raw[i] ?? "" : ""));
      const key = rowKey(headers, aligned);
      const old = rows.get(key);
      rows.set(key, old ? aligned.map((v, i) => (v.trim() ? v : old[i])) : aligned);
    }
  }
  return { title: usable[0].title, headers, rows: Array.from(rows.values()) };
}

/** The deadline in a row, or null when it has none or is not a date ("Rolling", "ASAP"). */
export function deadlineOf(headers: string[], row: string[]): Date | null {
  const i = col(headers, "deadline", "due");
  if (i < 0) return null;
  const text = (row[i] ?? "").replace(/[^\w\s,:/.-]/g, " ").trim();
  const ms = Date.parse(text);
  return Number.isNaN(ms) ? null : new Date(ms);
}

export interface SplitRows {
  open: string[][];
  passed: string[][];
  applied: string[][];
}

/** Sorts rows into still open, deadline passed, and applied. Applied wins, so an applied row never shows as open. */
export function splitRows(table: ParsedTable, today: Date, applied: Set<string>): SplitRows {
  const out: SplitRows = { open: [], passed: [], applied: [] };
  const startOfToday = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  for (const row of table.rows) {
    const d = deadlineOf(table.headers, row);
    if (applied.has(rowKey(table.headers, row))) out.applied.push(row);
    else if (d && d < startOfToday) out.passed.push(row);
    else out.open.push(row);
  }
  return out;
}

const cellsOf = (line: string) => line.trim().replace(/^\||\|$/g, "").split("|").map((c) => c.trim());

/**
 * Takes one listing out of a list's markdown, leaving every other line as it was. Returns the new text and
 * how many rows went, so the caller can tell a removal that matched nothing.
 */
export function removeListing(markdown: string, company: string, role: string): { text: string; removed: number } {
  const want = listingKey(company, role);
  let headers: string[] | null = null;
  let removed = 0;
  const kept: string[] = [];
  for (const line of markdown.split("\n")) {
    if (!line.trim().startsWith("|")) {
      kept.push(line);
      continue;
    }
    const cells = cellsOf(line);
    if (headers === null) headers = cells;
    else if (!cells.every((c) => /^:?-{3,}:?$/.test(c)) && rowKey(headers, cells) === want) {
      removed++;
      continue;
    }
    kept.push(line);
  }
  return { text: kept.join("\n"), removed };
}

/** The id the Internships card uses for its list: the verified listings, not a stored text list. */
export const LISTINGS_LIST_ID = "listings";

const OPEN_STAGES = new Set(["verified", "drafted", "approved", "submitting", "needs_you"]);

/**
 * The Internships card's table, built from the verified listings (read from the internship sites by code),
 * best tier first. Closed and confirmed listings are left out.
 */
export function listingsTable(listings: Listing[]): string {
  const cell = (s: string) => (s || "").replace(/\|/g, "/").replace(/\s+/g, " ").trim();
  const rows = listings
    .filter((l) => OPEN_STAGES.has(l.stage))
    .sort((a, b) => (a.tier || "Z").localeCompare(b.tier || "Z"))
    .map((l) => `| Tier ${cell(l.tier)} | ${cell(l.company)} | ${cell(l.role)} | ${cell(l.location)} | ${cell(l.pay)} | ${cell(l.source)} | ${cell(l.url)} |`);
  return ["| Tier | Company | Role | Mode & Location | Stipend | Source | Link |", "| --- | --- | --- | --- | --- | --- | --- |", ...rows].join("\n");
}

/** The open listing with this company and role, if any. */
export const findListing = (listings: Listing[], company: string, role: string) =>
  listings.find((l) => OPEN_STAGES.has(l.stage) && listingKey(l.company, l.role) === listingKey(company, role));
