import type { ParsedTable } from "@/lib/excel-export";

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
