"use client";

import { createContext, useContext, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { TRACKER_TYPES, appliedKeys, applyTaskInput, classifyTask, groupTrackerTasks, internshipActionMessage, isApplyTask, trackerPeers, trackerTypeOf, type InternshipAction } from "@/lib/trackers";
import { mergeTables, splitRows } from "@/lib/tracker-rows";
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  Banknote,
  Calendar,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Clock,
  ClipboardList,
  Download,
  FileText,
  ExternalLink,
  FileSpreadsheet,
  Layers,
  LayoutGrid,
  MapPin,
  RefreshCw,
  Search,
  Sparkles,
  Table2,
} from "lucide-react";
import { toast } from "sonner";
import { api, type Task } from "@/lib/api";
import { usePolling } from "@/lib/use-polling";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, ErrorNote, PageContainer, PageHeader, timeAgo } from "@/components/page";
import { extractTablesFromMarkdown, exportToExcel, exportToCSV, type ParsedTable } from "@/lib/excel-export";
import { parseResult, slug } from "@/lib/result-text";
import { cn } from "@/lib/utils";
import { MarkdownText } from "@/components/result-view";

export type ViewMode = "auto" | "table" | "cards";

type TrackerCategory = "all" | "internships" | "hackathons" | "opensource" | "hygiene" | "dsa" | "reports";

const CATEGORIES: { id: TrackerCategory; label: string; pattern: RegExp }[] = [
  { id: "all", label: "All Trackers", pattern: /.*/ },
  ...TRACKER_TYPES,
];

function isTrackerTask(task: Task): boolean {
  if (isApplyTask(task)) return false;
  const t = task.title.toLowerCase();
  const type = classifyTask(task);
  return (
    type === "internships" ||
    type === "hackathons" ||
    type === "opensource" ||
    t.includes("tracker") ||
    t.includes("internship") ||
    t.includes("hackathon") ||
    t.includes("open-source") ||
    t.includes("hygiene") ||
    t.includes("dsa coach") ||
    t.includes("brief") ||
    t.includes("scorecard") ||
    t.includes("audit") ||
    t.includes("thread alerts") ||
    t.includes("cold email")
  );
}

/** Strictly strip any stray emoji symbols from raw data */
function cleanEmojis(str: string): string {
  if (!str) return "";
  return str.replace(/[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}\u{FE00}-\u{FE0F}]/gu, "").trim();
}

/** Extracts a clean label and URL from markdown or plaintext cell */
function extractLinkFromCell(cell: string): { label: string; url: string } | null {
  if (!cell) return null;
  const cleaned = cleanEmojis(cell);
  const urlMatch = cleaned.match(/(https?:\/\/[^\s)]+)/i);
  if (!urlMatch) {
    if (/^www\./i.test(cleaned.trim())) {
      const url = `https://${cleaned.trim()}`;
      return { label: cleaned.trim(), url };
    }
    return null;
  }
  const url = urlMatch[1];
  let label = cleaned.replace(url, "").trim();
  label = label.replace(/^[:\-\s]+|[:\-\s]+$/g, "");
  if (!label || label.toLowerCase() === url.toLowerCase()) {
    try {
      label = new URL(url).hostname.replace(/^www\./, "");
    } catch {
      label = "Link";
    }
  }
  return { label, url };
}

/** Parses arbitrary table rows into structured card entities */
function parseRowToEntity(headers: string[], row: string[]) {
  let title = "";
  let subtitle = "";
  let tier: { label: string; variant: "a" | "b" | "c" | "neutral" } | null = null;
  let stipend: string | null = null;
  let deadline: { text: string; urgent: boolean } | null = null;
  let location: string | null = null;
  let duration: string | null = null;
  let ppo: string | null = null;
  let eligible: string | null = null;
  let posted: string | null = null;
  let applyLink: { label: string; url: string } | null = null;
  const otherFields: { label: string; value: string; link: { label: string; url: string } | null }[] = [];

  for (let i = 0; i < headers.length; i++) {
    const h = headers[i].toLowerCase();
    const rawVal = row[i] || "";
    const val = cleanEmojis(rawVal);
    if (!val) continue;

    if (h.includes("tier")) {
      const vUpper = val.toUpperCase();
      const variant = vUpper.includes("TIER A")
        ? "a"
        : vUpper.includes("TIER B")
        ? "b"
        : vUpper.includes("TIER C")
        ? "c"
        : "neutral";
      tier = { label: val, variant };
    } else if (h.includes("company") || (h.includes("name") && !title)) {
      title = val;
    } else if (h.includes("role") || h.includes("title") || h.includes("theme")) {
      subtitle = val;
    } else if (h.includes("stipend") || h.includes("prize") || h.includes("compensation") || h.includes("bounty")) {
      stipend = val;
    } else if (h.includes("deadline") || h.includes("due")) {
      const urgent = rawVal.includes("\u26A0") || /under 72h|< 72h|urgent/i.test(rawVal);
      deadline = { text: val, urgent };
    } else if (h.includes("location") || h.includes("mode")) {
      location = val;
    } else if (h.includes("duration")) {
      duration = val;
    } else if (h === "ppo" || h.includes("ppo")) {
      ppo = val;
    } else if (h.includes("eligible") || h.includes("batch")) {
      eligible = val;
    } else if (h.includes("posted")) {
      posted = val;
    } else if (h.includes("link") || h.includes("apply") || h.includes("url")) {
      applyLink = extractLinkFromCell(rawVal);
    } else {
      otherFields.push({
        label: headers[i],
        value: val,
        link: extractLinkFromCell(rawVal),
      });
    }
  }

  if (!title && headers.length > 0) {
    title = cleanEmojis(row[0]) || "Untitled Listing";
  }

  return {
    title,
    subtitle,
    tier,
    stipend,
    deadline,
    location,
    duration,
    ppo,
    eligible,
    posted,
    applyLink,
    otherFields,
  };
}

/** Formatted table cell renderer for desktop table view */
function TableCellContent({ header, rawCell }: { header: string; rawCell: string }) {
  const h = header.toLowerCase();
  const cell = cleanEmojis(rawCell);
  if (!cell) return <span className="text-muted-foreground/30">-</span>;

  // Tier column
  if (h.includes("tier")) {
    const upper = cell.toUpperCase();
    if (upper.includes("TIER A")) {
      return (
        <span className="inline-flex items-center rounded-full border border-emerald-500/30 bg-emerald-500/15 px-2.5 py-0.5 font-semibold text-emerald-700 dark:text-emerald-400">
          Tier A
        </span>
      );
    }
    if (upper.includes("TIER B")) {
      return (
        <span className="inline-flex items-center rounded-full border border-blue-500/30 bg-blue-500/15 px-2.5 py-0.5 font-semibold text-blue-700 dark:text-blue-400">
          Tier B
        </span>
      );
    }
    if (upper.includes("TIER C")) {
      return (
        <span className="inline-flex items-center rounded-full border border-amber-500/30 bg-amber-500/15 px-2.5 py-0.5 font-semibold text-amber-700 dark:text-amber-400">
          Tier C
        </span>
      );
    }
    return <span className="font-medium text-foreground">{cell}</span>;
  }

  // Stipend / Prize column
  if (h.includes("stipend") || h.includes("prize") || h.includes("compensation")) {
    return <span className="font-mono font-semibold text-foreground tracking-tight">{cell}</span>;
  }

  // Deadline column
  if (h.includes("deadline") || h.includes("due")) {
    const isUrgent = rawCell.includes("\u26A0") || /under 72h|< 72h|urgent/i.test(rawCell);
    return (
      <span
        className={cn(
          "inline-flex items-center gap-1 font-mono text-xs font-medium",
          isUrgent && "rounded-md border border-destructive/30 bg-destructive/15 px-2 py-0.5 font-semibold text-destructive",
        )}
      >
        <Clock className="size-3 text-muted-foreground" />
        <span>{cell}</span>
        {isUrgent && <span className="ml-0.5 rounded bg-destructive/20 px-1 py-0.2 text-[9px] uppercase tracking-wider">Urgent</span>}
      </span>
    );
  }

  // PPO / Eligibility column
  if (h === "ppo" || h.includes("ppo") || h.includes("eligible")) {
    const isYes = /^yes/i.test(cell.trim());
    const isNo = /^no/i.test(cell.trim());
    if (isYes) {
      return (
        <span className="inline-flex items-center rounded-md bg-emerald-500/15 px-2 py-0.5 font-medium text-emerald-700 dark:text-emerald-400 text-[11px]">
          Yes
        </span>
      );
    }
    if (isNo) {
      return (
        <span className="inline-flex items-center rounded-md bg-muted px-2 py-0.5 font-medium text-muted-foreground text-[11px]">
          No
        </span>
      );
    }
    return <span className="text-foreground">{cell}</span>;
  }

  // Link / Application column
  const link = extractLinkFromCell(rawCell);
  if (link) {
    return (
      <a
        href={link.url}
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex items-center gap-1 rounded-lg border border-primary/20 bg-primary/5 px-2.5 py-1 text-xs font-medium text-primary hover:bg-primary/10 transition-colors"
      >
        <span>{link.label || "Open"}</span>
        <ExternalLink className="size-3" />
      </a>
    );
  }

  return <span className="text-foreground leading-relaxed">{cell}</span>;
}

/** Individual record card for mobile and card view modes */
/** Set by an Internships tracker card so its rows can act on themselves (draft, plan, exact URL). */
const RecordActionsContext = createContext<{ taskId: string } | null>(null);

function InternshipActions({ taskId, entity }: { taskId: string; entity: ReturnType<typeof parseRowToEntity> }) {
  const [busy, setBusy] = useState<InternshipAction | "apply" | null>(null);
  const apply = async () => {
    setBusy("apply");
    try {
      await api.createTask(applyTaskInput(entity.title, entity.subtitle, entity.applyLink?.url ?? null));
      toast.success("Application task created. It will ask for your approval before submitting");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not create the application task");
    } finally {
      setBusy(null);
    }
  };
  const run = async (action: InternshipAction) => {
    setBusy(action);
    try {
      await api.followUp(taskId, internshipActionMessage(action, entity.title, entity.subtitle));
      toast.success(action === "url" ? "Looking for the exact URL" : action === "draft" ? "Drafting. It will wait for your approval" : "Building the plan");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not start that");
    } finally {
      setBusy(null);
    }
  };
  return (
    <div className="grid grid-cols-3 gap-1.5 pt-2">
      <Button size="sm" variant="outline" className="h-8 gap-1 rounded-xl text-xs" disabled={busy !== null} onClick={() => run("draft")}>
        <FileText className="size-3" /> Draft
      </Button>
      <Button size="sm" variant="outline" className="h-8 gap-1 rounded-xl text-xs" disabled={busy !== null} onClick={() => run("plan")}>
        <ClipboardList className="size-3" /> Plan
      </Button>
      <Button size="sm" className="h-8 gap-1 rounded-xl text-xs" disabled={busy !== null} onClick={apply}>
        Apply
      </Button>
      {entity.applyLink ? (
        <a
          href={entity.applyLink.url}
          target="_blank"
          rel="noopener noreferrer"
          className="col-span-3 inline-flex items-center justify-center gap-1 text-[11px] text-muted-foreground hover:text-foreground"
        >
          Open listing <ExternalLink className="size-3" />
        </a>
      ) : null}
    </div>
  );
}

function RecordCard({ entity }: { entity: ReturnType<typeof parseRowToEntity> }) {
  const [detailsOpen, setDetailsOpen] = useState(false);
  const actions = useContext(RecordActionsContext);

  return (
    <div className="flex flex-col justify-between rounded-xl border bg-card p-4 shadow-xs transition-all hover:border-primary/40 hover:shadow-sm">
      <div className="space-y-3">
        {/* Top Header: Title & Badges */}
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0 flex-1">
            <h4 className="text-sm font-semibold text-foreground tracking-tight truncate">
              {entity.title}
            </h4>
            {entity.subtitle && (
              <p className="text-xs text-muted-foreground line-clamp-2 mt-0.5">
                {entity.subtitle}
              </p>
            )}
          </div>

          <div className="flex flex-wrap items-center gap-1.5 shrink-0">
            {entity.tier && (
              <span
                className={cn(
                  "rounded-full px-2 py-0.5 text-[11px] font-semibold border",
                  entity.tier.variant === "a" && "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400 border-emerald-500/30",
                  entity.tier.variant === "b" && "bg-blue-500/15 text-blue-700 dark:text-blue-400 border-blue-500/30",
                  entity.tier.variant === "c" && "bg-amber-500/15 text-amber-700 dark:text-amber-400 border-amber-500/30",
                  entity.tier.variant === "neutral" && "bg-muted text-muted-foreground border-border",
                )}
              >
                {entity.tier.label}
              </span>
            )}
            {entity.ppo && /^yes/i.test(entity.ppo) && (
              <span className="rounded-md bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:text-emerald-400">
                PPO
              </span>
            )}
          </div>
        </div>

        {/* Highlight Pills / Metrics */}
        <div className="flex flex-wrap gap-1.5 text-xs">
          {entity.stipend && (
            <div className="inline-flex items-center gap-1 rounded-lg bg-primary/10 px-2.5 py-1 font-mono font-semibold text-primary">
              <Banknote className="size-3.5" />
              <span>{entity.stipend}</span>
            </div>
          )}

          {entity.deadline && (
            <div
              className={cn(
                "inline-flex items-center gap-1 rounded-lg px-2.5 py-1 font-mono text-[11px]",
                entity.deadline.urgent
                  ? "border border-destructive/30 bg-destructive/15 font-semibold text-destructive"
                  : "bg-muted/50 text-muted-foreground",
              )}
            >
              <Clock className="size-3" />
              <span>{entity.deadline.text}</span>
              {entity.deadline.urgent && (
                <span className="font-sans font-semibold text-[9px] uppercase tracking-wider">Urgent</span>
              )}
            </div>
          )}

          {entity.location && (
            <div className="inline-flex items-center gap-1 rounded-lg bg-muted/50 px-2.5 py-1 text-muted-foreground">
              <MapPin className="size-3" />
              <span className="truncate max-w-[180px]">{entity.location}</span>
            </div>
          )}

          {entity.duration && (
            <div className="inline-flex items-center gap-1 rounded-lg bg-muted/50 px-2.5 py-1 text-muted-foreground">
              <Calendar className="size-3" />
              <span className="truncate max-w-[180px]">{entity.duration}</span>
            </div>
          )}
        </div>

        {/* Secondary Details Collapsible */}
        {entity.otherFields.length > 0 && (
          <div className="border-t pt-2 space-y-1.5 text-xs">
            <button
              type="button"
              onClick={() => setDetailsOpen((v) => !v)}
              className="flex items-center gap-1 font-medium text-muted-foreground hover:text-foreground text-[11px] transition-colors"
            >
              {detailsOpen ? <ChevronUp className="size-3" /> : <ChevronDown className="size-3" />}
              <span>{detailsOpen ? "Hide Details" : "More Details"}</span>
            </button>

            {detailsOpen && (
              <div className="space-y-1 pt-1 rounded-lg bg-muted/20 p-2 text-muted-foreground text-[11px] leading-relaxed">
                {entity.posted && (
                  <div>
                    <span className="font-semibold text-foreground">Posted: </span>
                    <span>{entity.posted}</span>
                  </div>
                )}
                {entity.eligible && (
                  <div>
                    <span className="font-semibold text-foreground">Eligibility: </span>
                    <span>{entity.eligible}</span>
                  </div>
                )}
                {entity.otherFields.map((f, idx) => (
                  <div key={idx} className="break-words">
                    <span className="font-semibold text-foreground">{f.label}: </span>
                    {f.link ? (
                      <a
                        href={f.link.url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="text-primary hover:underline inline-flex items-center gap-0.5 font-medium"
                      >
                        {f.link.label}
                        <ExternalLink className="size-2.5" />
                      </a>
                    ) : (
                      <span>{f.value}</span>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>

      {actions ? (
        <InternshipActions taskId={actions.taskId} entity={entity} />
      ) : null}

      {/* Action Button */}
      {!actions && entity.applyLink && (
        <div className="pt-3 mt-1">
          <Button
            asChild
            size="sm"
            className="w-full h-8.5 rounded-xl gap-1.5 text-xs font-medium shadow-xs"
          >
            <a href={entity.applyLink.url} target="_blank" rel="noopener noreferrer">
              <span>Open Listing</span>
              <ExternalLink className="size-3" />
            </a>
          </Button>
        </div>
      )}
    </div>
  );
}

/** Desktop Table Component with Interactive Column Sorting */
function TableView({
  headers,
  rows,
  sortCol,
  sortAsc,
  onSort,
  expanded,
  onToggleExpanded,
}: {
  headers: string[];
  rows: string[][];
  sortCol: number | null;
  sortAsc: boolean;
  onSort: (colIdx: number) => void;
  expanded: boolean;
  onToggleExpanded: () => void;
}) {
  const displayRows = expanded ? rows : rows.slice(0, 8);

  return (
    <div className="space-y-3">
      <div className="overflow-x-auto rounded-xl border bg-card shadow-xs">
        <table className="w-full text-left text-xs">
          <thead className="bg-muted/60 border-b font-medium text-muted-foreground select-none">
            <tr>
              {headers.map((h, i) => {
                const isSorted = sortCol === i;
                return (
                  <th
                    key={i}
                    onClick={() => onSort(i)}
                    className="px-3.5 py-2.5 font-semibold whitespace-nowrap cursor-pointer hover:bg-muted/80 hover:text-foreground transition-colors"
                  >
                    <div className="flex items-center gap-1.5">
                      <span>{h}</span>
                      {isSorted ? (
                        sortAsc ? (
                          <ArrowUp className="size-3 text-primary" />
                        ) : (
                          <ArrowDown className="size-3 text-primary" />
                        )
                      ) : (
                        <ArrowUpDown className="size-3 text-muted-foreground/40" />
                      )}
                    </div>
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody className="divide-y divide-border/60">
            {displayRows.length === 0 ? (
              <tr>
                <td colSpan={headers.length} className="px-4 py-8 text-center text-muted-foreground">
                  No records match your filter.
                </td>
              </tr>
            ) : (
              displayRows.map((row, rIdx) => (
                <tr key={rIdx} className="hover:bg-muted/30 transition-colors">
                  {row.map((cell, cIdx) => (
                    <td key={cIdx} className="px-3.5 py-2.5 align-top leading-relaxed max-w-xs break-words">
                      <TableCellContent header={headers[cIdx] || ""} rawCell={cell} />
                    </td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {rows.length > 8 && (
        <div className="flex justify-center pt-1">
          <Button
            variant="ghost"
            size="sm"
            onClick={onToggleExpanded}
            className="h-8 text-xs font-medium text-primary hover:bg-primary/10"
          >
            {expanded ? `Show first 8 rows (of ${rows.length})` : `Show all ${rows.length} rows`}
          </Button>
        </div>
      )}
    </div>
  );
}

/** Mobile Cards View Component */
function CardsView({
  headers,
  rows,
  expanded,
  onToggleExpanded,
}: {
  headers: string[];
  rows: string[][];
  expanded: boolean;
  onToggleExpanded: () => void;
}) {
  const displayRows = expanded ? rows : rows.slice(0, 6);

  if (rows.length === 0) {
    return (
      <div className="rounded-xl border border-dashed py-8 text-center text-xs text-muted-foreground">
        No records match your filter.
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-3.5 sm:grid-cols-2 lg:grid-cols-2">
        {displayRows.map((row, rIdx) => {
          const entity = parseRowToEntity(headers, row);
          return <RecordCard key={rIdx} entity={entity} />;
        })}
      </div>

      {rows.length > 6 && (
        <div className="flex justify-center pt-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={onToggleExpanded}
            className="h-8 text-xs font-medium text-primary hover:bg-primary/10"
          >
            {expanded ? `Show first 6 cards (of ${rows.length})` : `Show all ${rows.length} cards`}
          </Button>
        </div>
      )}
    </div>
  );
}

interface TrackerCardProps {
  task: Task;
  parsedTables: ParsedTable[];
  closed?: { headers: string[]; applied: string[][]; passed: number };
  defaultViewMode?: ViewMode;
}

function TrackerCard({ task, parsedTables, closed, defaultViewMode = "auto" }: TrackerCardProps) {
  const { text } = parseResult(task.output_payload);
  const [tableFilter, setTableFilter] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [viewMode, setViewMode] = useState<ViewMode>(defaultViewMode);
  const [sortCol, setSortCol] = useState<number | null>(null);
  const [sortAsc, setSortAsc] = useState<boolean>(true);

  // Sync with global view mode changes
  useEffect(() => {
    setViewMode(defaultViewMode);
  }, [defaultViewMode]);

  const formattedDate = task.completed_at
    ? new Date(task.completed_at).toLocaleString("en-IN", {
        dateStyle: "medium",
        timeStyle: "short",
      })
    : task.started_at
    ? new Date(task.started_at).toLocaleString("en-IN", {
        dateStyle: "medium",
        timeStyle: "short",
      })
    : new Date(task.created_at).toLocaleString("en-IN", {
        dateStyle: "medium",
        timeStyle: "short",
      });

  const exportSingleExcel = () => {
    try {
      if (parsedTables.length > 0) {
        exportToExcel(
          `${slug(task.title)}_${new Date().toISOString().slice(0, 10)}`,
          parsedTables.map((t, idx) => ({
            name: (t.title || `Table ${idx + 1}`).slice(0, 30),
            headers: t.headers,
            rows: t.rows,
          })),
        );
        toast.success(`Exported ${parsedTables.length} table${parsedTables.length === 1 ? "" : "s"} to Excel`);
      } else {
        const rows = (text || "")
          .split("\n")
          .filter((l) => l.trim())
          .map((l) => [l.trim()]);
        exportToExcel(`${slug(task.title)}_${new Date().toISOString().slice(0, 10)}`, [
          {
            name: "Result",
            headers: ["Content"],
            rows,
          },
        ]);
        toast.success("Exported result to Excel");
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to export Excel");
    }
  };

  const exportSingleCSV = () => {
    try {
      const primary = parsedTables[0];
      if (primary) {
        exportToCSV(`${slug(task.title)}_${new Date().toISOString().slice(0, 10)}`, primary.headers, primary.rows);
        toast.success("Downloaded CSV spreadsheet");
      } else {
        const rows = (text || "")
          .split("\n")
          .filter((l) => l.trim())
          .map((l) => [l.trim()]);
        exportToCSV(`${slug(task.title)}_${new Date().toISOString().slice(0, 10)}`, ["Content"], rows);
        toast.success("Downloaded CSV");
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to export CSV");
    }
  };

  const primaryTable = parsedTables[0];

  const handleSort = (colIdx: number) => {
    if (sortCol === colIdx) {
      setSortAsc((v) => !v);
    } else {
      setSortCol(colIdx);
      setSortAsc(true);
    }
  };

  const processedRows = useMemo(() => {
    if (!primaryTable) return [];
    const q = tableFilter.trim().toLowerCase();
    let rows = primaryTable.rows;
    if (q) {
      rows = rows.filter((row) => row.some((cell) => cell.toLowerCase().includes(q)));
    }
    if (sortCol === null || sortCol >= primaryTable.headers.length) {
      return rows;
    }
    return [...rows].sort((a, b) => {
      const valA = cleanEmojis(a[sortCol] || "");
      const valB = cleanEmojis(b[sortCol] || "");

      // Numeric / Currency
      const numA = parseFloat(valA.replace(/[^0-9.-]/g, ""));
      const numB = parseFloat(valB.replace(/[^0-9.-]/g, ""));
      if (!isNaN(numA) && !isNaN(numB) && /[₹$\d]/.test(valA) && /[₹$\d]/.test(valB)) {
        return sortAsc ? numA - numB : numB - numA;
      }

      // Date comparison
      const dateA = Date.parse(valA.slice(0, 10));
      const dateB = Date.parse(valB.slice(0, 10));
      if (!isNaN(dateA) && !isNaN(dateB) && valA.length >= 10 && valB.length >= 10) {
        return sortAsc ? dateA - dateB : dateB - dateA;
      }

      // String locale compare
      return sortAsc ? valA.localeCompare(valB) : valB.localeCompare(valA);
    });
  }, [primaryTable, tableFilter, sortCol, sortAsc]);

  return (
    <RecordActionsContext.Provider value={trackerTypeOf(task.title) === "internships" ? { taskId: task.id } : null}>
    <div className="overflow-hidden rounded-2xl border bg-card shadow-xs transition-colors hover:border-primary/40">
      {/* Header bar */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-b bg-muted/20 p-4 sm:px-6">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <span
              className={cn(
                "inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-medium",
                task.status === "COMPLETED"
                  ? "bg-success/15 text-success"
                  : task.status === "RUNNING"
                  ? "bg-primary/15 text-primary animate-pulse"
                  : "bg-muted text-muted-foreground",
              )}
            >
              <CheckCircle2 className="size-3.5" />
              {task.status}
            </span>
            <span className="rounded-md border bg-muted/40 px-2 py-0.5 font-mono text-[11px] text-muted-foreground uppercase">
              {task.manager_domain}
            </span>
          </div>
          <h3 className="text-base font-semibold text-foreground tracking-tight">{task.title}</h3>
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Clock className="size-3.5" />
            <span>Updated {formattedDate} ({timeAgo(task.completed_at || task.created_at)})</span>
          </div>
        </div>

        {/* Action Toolbar */}
        <div className="flex flex-wrap items-center gap-2">
          {/* View Mode Toggle */}
          <div className="flex items-center rounded-xl border bg-muted/30 p-0.5 text-xs">
            <button
              type="button"
              onClick={() => setViewMode("auto")}
              className={cn(
                "flex items-center gap-1 rounded-lg px-2.5 py-1 text-[11px] font-medium transition-colors",
                viewMode === "auto" ? "bg-background text-foreground shadow-xs" : "text-muted-foreground hover:text-foreground",
              )}
              title="Responsive: Table on desktop, Cards on phone"
            >
              <Layers className="size-3" />
              <span className="hidden sm:inline">Auto</span>
            </button>
            <button
              type="button"
              onClick={() => setViewMode("table")}
              className={cn(
                "flex items-center gap-1 rounded-lg px-2.5 py-1 text-[11px] font-medium transition-colors",
                viewMode === "table" ? "bg-background text-foreground shadow-xs" : "text-muted-foreground hover:text-foreground",
              )}
              title="Force Table view"
            >
              <Table2 className="size-3" />
              <span>Table</span>
            </button>
            <button
              type="button"
              onClick={() => setViewMode("cards")}
              className={cn(
                "flex items-center gap-1 rounded-lg px-2.5 py-1 text-[11px] font-medium transition-colors",
                viewMode === "cards" ? "bg-background text-foreground shadow-xs" : "text-muted-foreground hover:text-foreground",
              )}
              title="Force Cards view"
            >
              <LayoutGrid className="size-3" />
              <span>Cards</span>
            </button>
          </div>

          <Button
            size="sm"
            onClick={exportSingleExcel}
            className="h-8.5 gap-1.5 rounded-xl font-medium shadow-xs"
          >
            <FileSpreadsheet className="size-3.5" />
            <span>Excel</span>
          </Button>

          <Button
            variant="outline"
            size="sm"
            onClick={exportSingleCSV}
            className="h-8.5 gap-1.5 rounded-xl text-xs font-medium"
          >
            <Download className="size-3" />
            <span>CSV</span>
          </Button>

          <Button
            variant="ghost"
            size="sm"
            asChild
            className="h-8.5 gap-1.5 rounded-xl text-xs font-medium"
          >
            <Link href={`/tasks/${task.id}`}>
              <span>Task</span>
              <ExternalLink className="size-3" />
            </Link>
          </Button>
        </div>
      </div>

      {/* Content Section */}
      <div className="p-4 sm:p-6 space-y-4">
        {primaryTable ? (
          <div className="space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center gap-2 text-xs font-semibold text-foreground">
                <Table2 className="size-4 text-primary" />
                <span>{primaryTable.title || "Tracker Listings"}</span>
                <span className="rounded-full bg-primary/10 px-2 py-0.5 font-mono text-primary">
                  {primaryTable.rows.length} record{primaryTable.rows.length === 1 ? "" : "s"}
                </span>
              </div>

              {primaryTable.rows.length > 5 && (
                <div className="relative w-full sm:w-64">
                  <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground" />
                  <Input
                    type="search"
                    placeholder="Filter records..."
                    value={tableFilter}
                    onChange={(e) => setTableFilter(e.target.value)}
                    className="h-8 pl-8 text-xs rounded-xl"
                  />
                </div>
              )}
            </div>

            {/* Responsive Render: Table vs Cards */}
            {viewMode === "table" ? (
              <TableView
                headers={primaryTable.headers}
                rows={processedRows}
                sortCol={sortCol}
                sortAsc={sortAsc}
                onSort={handleSort}
                expanded={expanded}
                onToggleExpanded={() => setExpanded((v) => !v)}
              />
            ) : viewMode === "cards" ? (
              <CardsView
                headers={primaryTable.headers}
                rows={processedRows}
                expanded={expanded}
                onToggleExpanded={() => setExpanded((v) => !v)}
              />
            ) : (
              <>
                {/* Auto mode: Table on md+, Cards on mobile */}
                <div className="hidden md:block">
                  <TableView
                    headers={primaryTable.headers}
                    rows={processedRows}
                    sortCol={sortCol}
                    sortAsc={sortAsc}
                    onSort={handleSort}
                    expanded={expanded}
                    onToggleExpanded={() => setExpanded((v) => !v)}
                  />
                </div>
                <div className="block md:hidden">
                  <CardsView
                    headers={primaryTable.headers}
                    rows={processedRows}
                    expanded={expanded}
                    onToggleExpanded={() => setExpanded((v) => !v)}
                  />
                </div>
              </>
            )}
          </div>
        ) : text ? (
          <div className="space-y-2">
            <div className="rounded-xl border bg-card/60 p-5 text-sm leading-relaxed max-h-96 overflow-y-auto">
              <MarkdownText text={text} />
            </div>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground italic">No output data available yet for this task.</p>
        )}
      </div>
      {closed ? (
        <div className="space-y-2 border-t bg-muted/10 p-4 text-xs sm:px-6">
          {closed.passed > 0 ? (
            <p className="text-muted-foreground">{closed.passed} listing{closed.passed === 1 ? "" : "s"} past their deadline {closed.passed === 1 ? "was" : "were"} removed.</p>
          ) : null}
          {closed.applied.length > 0 ? (
            <div>
              <p className="mb-1 font-semibold text-foreground">Applied ({closed.applied.length})</p>
              <ul className="space-y-0.5 text-muted-foreground">
                {closed.applied.map((r, i) => {
                  const e = parseRowToEntity(closed.headers, r);
                  return (
                    <li key={i}>
                      {e.title}
                      {e.subtitle ? `, ${e.subtitle}` : ""}
                    </li>
                  );
                })}
              </ul>
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
    </RecordActionsContext.Provider>
  );
}

export default function TrackersPage() {
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [category, setCategory] = useState<TrackerCategory>("all");
  const [query, setQuery] = useState("");
  const [globalViewMode, setGlobalViewMode] = useState<ViewMode>("auto");
  const [refreshing, setRefreshing] = useState(false);

  const { refresh } = usePolling(
    async (signal) => {
      const all = await api.listTasks(undefined, { signal });
      setTasks(all);
      setError(null);
    },
    { intervalMs: 15000 },
  );

  // Filter tasks to only trackers, grouping recurring runs under a single card showing the latest run
  const trackerTasks = useMemo(() => {
    if (!tasks) return [];
    return groupTrackerTasks(tasks, isTrackerTask);
  }, [tasks]);

  // Group by category and search
  const filteredTasks = useMemo(() => {
    const q = query.trim().toLowerCase();
    const activeCat = CATEGORIES.find((c) => c.id === category);

    return trackerTasks.filter((t) => {
      const matchesCategory = category === "all" || (activeCat && activeCat.pattern.test(t.title));
      if (!matchesCategory) return false;
      if (!q) return true;
      const { text } = parseResult(t.output_payload);
      return (
        t.title.toLowerCase().includes(q) ||
        t.goal.toLowerCase().includes(q) ||
        (text && text.toLowerCase().includes(q))
      );
    });
  }, [trackerTasks, category, query]);

  // Parsed tables mapped by task id
  // Each card shows every task of its tracker appended into one list. Applied listings and passed
  // deadlines come out of the open list; applied ones are kept in their own list.
  const applied = useMemo(() => appliedKeys(tasks ?? []), [tasks]);
  const { taskTables, closedByTask } = useMemo(() => {
    const tables = new Map<string, ParsedTable[]>();
    const closed = new Map<string, { headers: string[]; applied: string[][]; passed: number }>();
    const firstTable = (t: Task) => {
      const { text } = parseResult(t.output_payload);
      return text ? extractTablesFromMarkdown(text)[0] : undefined;
    };
    const today = new Date();
    trackerTasks.forEach((t) => {
      const peers = trackerPeers(tasks ?? [], isTrackerTask, t);
      const merged = mergeTables(peers.map(firstTable).filter((x): x is ParsedTable => !!x));
      if (!merged) {
        const { text } = parseResult(t.output_payload);
        tables.set(t.id, text ? extractTablesFromMarkdown(text) : []);
        return;
      }
      const split = splitRows(merged, today, applied);
      tables.set(t.id, [{ ...merged, rows: split.open }]);
      if (split.applied.length || split.passed.length) {
        closed.set(t.id, { headers: merged.headers, applied: split.applied, passed: split.passed.length });
      }
    });
    return { taskTables: tables, closedByTask: closed };
  }, [trackerTasks, tasks, applied]);

  // Global Export: Combines latest runs across categories into one multi-sheet Excel
  const exportMasterExcel = () => {
    try {
      const sheets: { name: string; headers: string[]; rows: (string | number)[][] }[] = [];
      const seenTitles = new Set<string>();

      trackerTasks
        .filter((t) => t.status === "COMPLETED")
        .forEach((task) => {
          const baseName = task.title.split("(")[0].trim();
          if (seenTitles.has(baseName)) return;
          seenTitles.add(baseName);

          const tables = taskTables.get(task.id) || [];
          if (tables.length > 0) {
            tables.forEach((tbl, idx) => {
              const sheetName = tables.length === 1 ? baseName : `${baseName} ${idx + 1}`;
              sheets.push({
                name: sheetName.slice(0, 30),
                headers: tbl.headers,
                rows: tbl.rows,
              });
            });
          }
        });

      if (sheets.length === 0) {
        toast.error("No tracker tables found to export.");
        return;
      }

      const today = new Date().toISOString().slice(0, 10);
      exportToExcel(`Griffin_Trackers_Master_${today}`, sheets);
      toast.success(`Exported master Excel workbook with ${sheets.length} sheets!`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to generate master Excel");
    }
  };

  return (
    <PageContainer>
      <PageHeader
        title="Trackers & Spreadsheets"
        description="Unified hub for all autonomous trackers. View tables on desktop or responsive cards on mobile, filter records, and export results directly to Excel."
        actions={
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setRefreshing(true);
                refresh();
                setTimeout(() => setRefreshing(false), 600);
              }}
              disabled={refreshing}
              className="h-10 gap-1.5 rounded-xl text-xs font-medium"
            >
              <RefreshCw className={cn("size-3.5", refreshing && "animate-spin")} />
              <span>Refresh</span>
            </Button>

            <Button
              size="sm"
              onClick={exportMasterExcel}
              className="h-10 gap-2 rounded-xl font-medium shadow-sm"
            >
              <FileSpreadsheet className="size-4" />
              <span>Export Master Excel</span>
            </Button>
          </div>
        }
      />

      <div className="space-y-6">
        {/* Category Filter Pills & Global View Mode Selector */}
        <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-3">
          <div className="flex flex-wrap items-center gap-1.5">
            {CATEGORIES.map((cat) => {
              const count =
                cat.id === "all"
                  ? trackerTasks.length
                  : trackerTasks.filter((t) => cat.pattern.test(t.title)).length;
              const active = category === cat.id;
              return (
                <button
                  key={cat.id}
                  type="button"
                  onClick={() => setCategory(cat.id)}
                  className={cn(
                    "flex items-center gap-1.5 rounded-xl px-3.5 py-1.5 text-xs font-medium transition-colors",
                    active
                      ? "bg-primary text-primary-foreground shadow-sm"
                      : "bg-muted/50 text-muted-foreground hover:bg-muted hover:text-foreground",
                  )}
                >
                  <span>{cat.label}</span>
                  <span
                    className={cn(
                      "rounded-full px-1.5 py-0.2 font-mono text-[10px]",
                      active
                        ? "bg-primary-foreground/20 text-primary-foreground"
                        : "bg-muted-foreground/15 text-muted-foreground",
                    )}
                  >
                    {count}
                  </span>
                </button>
              );
            })}
          </div>

          {/* Global View Mode Switcher */}
          <div className="flex items-center rounded-xl border bg-muted/30 p-0.5 text-xs">
            <span className="px-2 text-muted-foreground font-medium text-[11px] hidden sm:inline">View:</span>
            <button
              type="button"
              onClick={() => setGlobalViewMode("auto")}
              className={cn(
                "flex items-center gap-1 rounded-lg px-2.5 py-1 text-xs font-medium transition-colors",
                globalViewMode === "auto" ? "bg-background text-foreground shadow-xs" : "text-muted-foreground hover:text-foreground",
              )}
              title="Responsive: Table on desktop, Cards on phone"
            >
              <Layers className="size-3.5" />
              <span>Responsive</span>
            </button>
            <button
              type="button"
              onClick={() => setGlobalViewMode("table")}
              className={cn(
                "flex items-center gap-1 rounded-lg px-2.5 py-1 text-xs font-medium transition-colors",
                globalViewMode === "table" ? "bg-background text-foreground shadow-xs" : "text-muted-foreground hover:text-foreground",
              )}
              title="Table view on all devices"
            >
              <Table2 className="size-3.5" />
              <span>Table</span>
            </button>
            <button
              type="button"
              onClick={() => setGlobalViewMode("cards")}
              className={cn(
                "flex items-center gap-1 rounded-lg px-2.5 py-1 text-xs font-medium transition-colors",
                globalViewMode === "cards" ? "bg-background text-foreground shadow-xs" : "text-muted-foreground hover:text-foreground",
              )}
              title="Cards view on all devices"
            >
              <LayoutGrid className="size-3.5" />
              <span>Cards</span>
            </button>
          </div>
        </div>

        {/* Search Bar */}
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
          <Input
            type="search"
            aria-label="Search tracker results"
            placeholder="Search across all tracker outputs, companies, roles, hackathons..."
            className="h-11 pl-9 rounded-xl"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>

        {error && <ErrorNote message={error} />}

        {/* Content */}
        {tasks === null ? (
          <div className="space-y-4">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-48 w-full rounded-2xl" />
            ))}
          </div>
        ) : filteredTasks.length === 0 ? (
          <EmptyState
            icon={<Table2 className="size-5" />}
            title="No tracker results found"
            description={
              query
                ? "No tracker outputs match your search query."
                : "No trackers have completed in this category yet. When scheduled trackers run, their results appear here."
            }
          />
        ) : (
          <div className="space-y-6">
            {filteredTasks.map((task) => (
              <TrackerCard
                key={task.id}
                task={task}
                parsedTables={taskTables.get(task.id) || []}
                closed={closedByTask.get(task.id)}
                defaultViewMode={globalViewMode}
              />
            ))}
          </div>
        )}
      </div>
    </PageContainer>
  );
}
