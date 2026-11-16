export interface ParsedTable {
  title: string;
  headers: string[];
  rows: string[][];
}

/**
 * Cleans markdown formatting from text:
 * - Converts [Text](url) to "Text (url)" or "url"
 * - Removes bold (**text**), italics (*text*), code (`code`)
 * - Strips emojis and decorative markers
 */
export function cleanMarkdownCell(raw: string): string {
  if (!raw) return "";
  let s = raw.trim();
  // Strip Markdown links: [label](url) -> "label (url)" if different, else "url"
  s = s.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, (_, label, url) => {
    const l = label.trim();
    const u = url.trim();
    if (l.toLowerCase() === u.toLowerCase() || !l) return u;
    return `${l}: ${u}`;
  });
  // Strip formatting
  s = s.replace(/\*\*([^*]+)\*\*/g, "$1");
  s = s.replace(/\*([^*]+)\*/g, "$1");
  s = s.replace(/__([^_]+)__/g, "$1");
  s = s.replace(/_([^_]+)_/g, "$1");
  s = s.replace(/`([^`]+)`/g, "$1");
  s = s.replace(/~~([^~]+)~~/g, "$1");
  // Remove emoji characters if any
  s = s.replace(/[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/gu, "");
  return s.trim();
}

/**
 * Parses all markdown tables from text into structured ParsedTable objects.
 */
export function extractTablesFromMarkdown(markdown: string): ParsedTable[] {
  if (!markdown) return [];
  const lines = markdown.split(/\r?\n/);
  const tables: ParsedTable[] = [];

  let currentTitle = "Tracker Data";
  let inTable = false;
  let headers: string[] = [];
  let rows: string[][] = [];

  const isTableRow = (line: string) => {
    const trimmed = line.trim();
    return trimmed.startsWith("|") && trimmed.endsWith("|") && trimmed.includes("|");
  };

  const isSeparatorRow = (line: string) => {
    const trimmed = line.trim();
    if (!trimmed.startsWith("|") || !trimmed.endsWith("|")) return false;
    const inner = trimmed.slice(1, -1);
    const cells = inner.split("|").map((c) => c.trim());
    return cells.every((c) => /^:?-+:?$/.test(c));
  };

  const parseCells = (line: string): string[] => {
    const trimmed = line.trim();
    const inner = trimmed.startsWith("|") && trimmed.endsWith("|") ? trimmed.slice(1, -1) : trimmed;
    return inner.split("|").map((c) => cleanMarkdownCell(c));
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trim();

    // Check for headings preceding a table
    if (trimmed.startsWith("#")) {
      const h = trimmed.replace(/^#+\s*/, "").replace(/[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/gu, "").trim();
      if (h) currentTitle = h;
    }

    if (isTableRow(line)) {
      if (!inTable) {
        // Potential table start (headers)
        if (i + 1 < lines.length && isSeparatorRow(lines[i + 1])) {
          inTable = true;
          headers = parseCells(line);
          rows = [];
          i++; // Skip the separator row
          continue;
        }
      } else {
        // Table data row
        const cells = parseCells(line);
        // Normalize cell length to match headers
        while (cells.length < headers.length) cells.push("");
        rows.push(cells.slice(0, headers.length));
      }
    } else {
      if (inTable) {
        if (headers.length > 0 && rows.length > 0) {
          tables.push({
            title: currentTitle || `Table ${tables.length + 1}`,
            headers,
            rows,
          });
        }
        inTable = false;
        headers = [];
        rows = [];
      }
    }
  }

  // End of text check
  if (inTable && headers.length > 0 && rows.length > 0) {
    tables.push({
      title: currentTitle || `Table ${tables.length + 1}`,
      headers,
      rows,
    });
  }

  // If no pipe table was found, try extracting structured key-value bullet lists
  if (tables.length === 0) {
    const listTable = extractStructuredListAsTable(markdown);
    if (listTable) {
      tables.push(listTable);
    }
  }

  return tables;
}

/**
 * Extracts structured bullet records (e.g. ### 1. Title \n * **Field:** Value) into a ParsedTable.
 */
function extractStructuredListAsTable(markdown: string): ParsedTable | null {
  const lines = markdown.split(/\r?\n/);
  const items: { title: string; fields: Record<string, string> }[] = [];
  const fieldOrder: string[] = [];

  let currentTitle = "";
  let currentFields: Record<string, string> = {};

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    // Check for item heading like "### 1. Title" or "### Title" or "**1. Title**"
    const headingMatch = line.match(/^#{2,4}\s+(?:\d+[\.\)]\s*)?([^\n]+)/);
    if (headingMatch) {
      if (currentTitle && Object.keys(currentFields).length > 0) {
        items.push({ title: currentTitle, fields: currentFields });
        currentFields = {};
      }
      currentTitle = cleanMarkdownCell(headingMatch[1]);
      continue;
    }

    // Check for bullet key-value: "* **Key:** Value" or "- **Key:** Value"
    const kvMatch = line.match(/^[\*\-]\s+\*\*([^*:]+):\*\*\s*(.*)/);
    if (kvMatch) {
      const key = kvMatch[1].trim();
      const val = cleanMarkdownCell(kvMatch[2].trim());
      currentFields[key] = val;
      if (!fieldOrder.includes(key)) fieldOrder.push(key);
    }
  }

  if (currentTitle && Object.keys(currentFields).length > 0) {
    items.push({ title: currentTitle, fields: currentFields });
  }

  if (items.length >= 1 && fieldOrder.length >= 2) {
    const headers = ["Title", ...fieldOrder];
    const rows = items.map((item) => [
      item.title,
      ...fieldOrder.map((f) => item.fields[f] || ""),
    ]);
    return {
      title: "Tracker Records",
      headers,
      rows,
    };
  }

  return null;
}

/**
 * Downloads a multi-sheet Excel (.xlsx) file.
 */
export async function exportToExcel(
  filename: string,
  sheets: { name: string; headers: string[]; rows: (string | number)[][] }[],
) {
  if (!sheets || sheets.length === 0) return;
  const XLSX = await import("xlsx");
  const wb = XLSX.utils.book_new();

  sheets.forEach((sheet, idx) => {
    const data = [sheet.headers, ...sheet.rows];
    const ws = XLSX.utils.aoa_to_sheet(data);

    // Auto-fit column widths
    const colWidths = sheet.headers.map((h, colIdx) => {
      let maxLen = h.length;
      sheet.rows.forEach((row) => {
        const val = row[colIdx] != null ? String(row[colIdx]) : "";
        if (val.length > maxLen) maxLen = Math.min(val.length, 50);
      });
      return { wch: Math.max(maxLen + 3, 12) };
    });
    ws["!cols"] = colWidths;

    // Excel limits sheet names to 31 characters and forbids : \ / ? * [ ]
    const safeName = (sheet.name || `Sheet${idx + 1}`)
      .replace(/[:\\/?*\[\]]/g, "_")
      .trim()
      .slice(0, 31) || `Sheet${idx + 1}`;

    XLSX.utils.book_append_sheet(wb, ws, safeName);
  });

  const safeFilename = filename.replace(/[^a-zA-Z0-9_\-.]/g, "_");
  XLSX.writeFile(wb, safeFilename.endsWith(".xlsx") ? safeFilename : `${safeFilename}.xlsx`);
}

/**
 * Downloads a CSV file with UTF-8 BOM so Excel opens it with proper encoding.
 */
export function exportToCSV(filename: string, headers: string[], rows: (string | number)[][]) {
  const allRows = [headers, ...rows];
  const csvContent =
    "\uFEFF" +
    allRows
      .map((row) =>
        row
          .map((cell) => {
            const val = cell != null ? String(cell) : "";
            if (val.includes(",") || val.includes('"') || val.includes("\n") || val.includes("\r")) {
              return `"${val.replace(/"/g, '""')}"`;
            }
            return val;
          })
          .join(","),
      )
      .join("\r\n");

  const blob = new Blob([csvContent], { type: "text/csv;charset=utf-8;" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  const safeFilename = filename.replace(/[^a-zA-Z0-9_\-.]/g, "_");
  a.download = safeFilename.endsWith(".csv") ? safeFilename : `${safeFilename}.csv`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}
