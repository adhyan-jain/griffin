"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import {
  Calendar,
  CheckCircle2,
  Clock,
  Download,
  ExternalLink,
  FileSpreadsheet,
  Filter,
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

type TrackerCategory = "all" | "internships" | "hackathons" | "opensource" | "hygiene" | "dsa" | "reports";

const CATEGORIES: { id: TrackerCategory; label: string; pattern: RegExp }[] = [
  { id: "all", label: "All Trackers", pattern: /.*/ },
  { id: "internships", label: "Internships", pattern: /internship/i },
  { id: "hackathons", label: "Hackathons", pattern: /hackathon/i },
  { id: "opensource", label: "Open Source", pattern: /open-source|oss/i },
  { id: "hygiene", label: "Repo Hygiene", pattern: /hygiene|repo/i },
  { id: "dsa", label: "DSA Coach", pattern: /dsa/i },
  { id: "reports", label: "Reports & Briefs", pattern: /brief|report|scorecard|audit|thread/i },
];

function isTrackerTask(task: Task): boolean {
  const t = task.title.toLowerCase();
  return (
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

function getCategoryForTask(task: Task): TrackerCategory {
  const t = task.title.toLowerCase();
  if (t.includes("internship")) return "internships";
  if (t.includes("hackathon")) return "hackathons";
  if (t.includes("open-source") || t.includes("oss")) return "opensource";
  if (t.includes("hygiene")) return "hygiene";
  if (t.includes("dsa")) return "dsa";
  if (t.includes("brief") || t.includes("report") || t.includes("scorecard") || t.includes("audit") || t.includes("thread")) return "reports";
  return "all";
}

interface TrackerCardProps {
  task: Task;
  parsedTables: ParsedTable[];
}

function TrackerCard({ task, parsedTables }: TrackerCardProps) {
  const { text } = parseResult(task.output_payload);
  const [tableFilter, setTableFilter] = useState("");
  const [expanded, setExpanded] = useState(false);

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
  const filteredRows = useMemo(() => {
    if (!primaryTable) return [];
    const q = tableFilter.trim().toLowerCase();
    if (!q) return primaryTable.rows;
    return primaryTable.rows.filter((row) => row.some((cell) => cell.toLowerCase().includes(q)));
  }, [primaryTable, tableFilter]);

  const displayRows = expanded ? filteredRows : filteredRows.slice(0, 8);

  return (
    <div className="overflow-hidden rounded-2xl border bg-card shadow-sm transition-colors hover:border-primary/40">
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

        {/* Action Buttons */}
        <div className="flex flex-wrap items-center gap-2">
          <Button
            size="sm"
            onClick={exportSingleExcel}
            className="h-9 gap-1.5 rounded-xl font-medium shadow-sm"
          >
            <FileSpreadsheet className="size-4" />
            <span>Export Excel (.xlsx)</span>
          </Button>

          <Button
            variant="outline"
            size="sm"
            onClick={exportSingleCSV}
            className="h-9 gap-1.5 rounded-xl text-xs font-medium"
          >
            <Download className="size-3.5" />
            <span>CSV</span>
          </Button>

          <Button
            variant="ghost"
            size="sm"
            asChild
            className="h-9 gap-1.5 rounded-xl text-xs font-medium"
          >
            <Link href={`/tasks/${task.id}`}>
              <span>Task</span>
              <ExternalLink className="size-3.5" />
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

            {/* Structured Data Table */}
            <div className="overflow-x-auto rounded-xl border bg-card">
              <table className="w-full text-left text-xs">
                <thead className="bg-muted/60 border-b font-medium text-muted-foreground">
                  <tr>
                    {primaryTable.headers.map((h, i) => (
                      <th key={i} className="px-3.5 py-2.5 font-semibold whitespace-nowrap">
                        {h}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-border/60">
                  {displayRows.length === 0 ? (
                    <tr>
                      <td colSpan={primaryTable.headers.length} className="px-4 py-6 text-center text-muted-foreground">
                        No records match your filter.
                      </td>
                    </tr>
                  ) : (
                    displayRows.map((row, rIdx) => (
                      <tr key={rIdx} className="hover:bg-muted/30 transition-colors">
                        {row.map((cell, cIdx) => {
                          const isUrl = /^(https?:\/\/|www\.)/i.test(cell.trim());
                          const urlMatch = cell.match(/(https?:\/\/[^\s]+)/);
                          return (
                            <td key={cIdx} className="px-3.5 py-2.5 align-top leading-relaxed max-w-xs break-words">
                              {urlMatch ? (
                                <a
                                  href={urlMatch[1]}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="text-primary hover:underline font-medium inline-flex items-center gap-1"
                                >
                                  {cell.replace(urlMatch[1], "").trim() || "Link"}
                                  <ExternalLink className="size-3" />
                                </a>
                              ) : isUrl ? (
                                <a
                                  href={cell.startsWith("http") ? cell : `https://${cell}`}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="text-primary hover:underline font-medium inline-flex items-center gap-1"
                                >
                                  Open
                                  <ExternalLink className="size-3" />
                                </a>
                              ) : (
                                cell || <span className="text-muted-foreground/40">-</span>
                              )}
                            </td>
                          );
                        })}
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>

            {filteredRows.length > 8 && (
              <div className="flex justify-center pt-1">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setExpanded((v) => !v)}
                  className="h-8 text-xs font-medium text-primary hover:bg-primary/10"
                >
                  {expanded ? `Show first 8 rows (of ${filteredRows.length})` : `Show all ${filteredRows.length} rows`}
                </Button>
              </div>
            )}
          </div>
        ) : text ? (
          <div className="space-y-2">
            <div className="rounded-xl border bg-muted/20 p-4 font-mono text-xs whitespace-pre-wrap leading-relaxed max-h-60 overflow-y-auto">
              {text}
            </div>
          </div>
        ) : (
          <p className="text-xs text-muted-foreground italic">No output data available yet for this task.</p>
        )}
      </div>
    </div>
  );
}

export default function TrackersPage() {
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [category, setCategory] = useState<TrackerCategory>("all");
  const [query, setQuery] = useState("");
  const [refreshing, setRefreshing] = useState(false);

  const { refresh } = usePolling(
    async (signal) => {
      const all = await api.listTasks(undefined, { signal });
      setTasks(all);
      setError(null);
    },
    { intervalMs: 15000 },
  );

  // Filter tasks to only trackers
  const trackerTasks = useMemo(() => {
    if (!tasks) return [];
    return tasks.filter((t) => isTrackerTask(t));
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
  const taskTables = useMemo(() => {
    const map = new Map<string, ParsedTable[]>();
    trackerTasks.forEach((t) => {
      const { text } = parseResult(t.output_payload);
      if (text) {
        map.set(t.id, extractTablesFromMarkdown(text));
      } else {
        map.set(t.id, []);
      }
    });
    return map;
  }, [trackerTasks]);

  // Global Export: Combines latest runs across categories into one multi-sheet Excel
  const exportMasterExcel = () => {
    try {
      const sheets: { name: string; headers: string[]; rows: (string | number)[][] }[] = [];
      const seenTitles = new Set<string>();

      // Pick latest completed task from each tracker category
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
        description="Unified hub for all autonomous trackers. View live tables, filter records, and export results directly to Excel sheets."
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
        {/* Category Filter Pills */}
        <div className="flex flex-wrap items-center gap-1.5 border-b pb-3">
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
                    active ? "bg-primary-foreground/20 text-primary-foreground" : "bg-muted-foreground/15 text-muted-foreground",
                  )}
                >
                  {count}
                </span>
              </button>
            );
          })}
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
              />
            ))}
          </div>
        )}
      </div>
    </PageContainer>
  );
}
