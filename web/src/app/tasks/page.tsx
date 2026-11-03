"use client";

import { useMemo, useState } from "react";
import { usePolling } from "@/lib/use-polling";
import { useRouter } from "next/navigation";
import { ListChecks, Plus, Search } from "lucide-react";
import { api, type Task } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ACTIVE_STATUSES, WAITING_STATUSES, isDone } from "@/components/StatusBadge";
import { EmptyState, ErrorNote, PageContainer, PageHeader } from "@/components/page";
import { NewTaskDialog } from "@/components/new-task-dialog";
import { TaskRow } from "@/components/task-row";

const FILTERS = [
  { value: "all", label: "All" },
  { value: "active", label: "Active" },
  { value: "approval", label: "Needs approval" },
  { value: "done", label: "Done" },
] as const;
type Filter = (typeof FILTERS)[number]["value"];

const matches = (t: Task, f: Filter) =>
  f === "all" ||
  (f === "active" && (ACTIVE_STATUSES.includes(t.status) || WAITING_STATUSES.includes(t.status))) ||
  (f === "approval" && t.status === "AWAITING_APPROVAL") ||
  (f === "done" && isDone(t.status));

export default function TasksPage() {
  const router = useRouter();
  const [tasks, setTasks] = useState<Task[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<Filter>("all");
  const [query, setQuery] = useState("");
  const [newOpen, setNewOpen] = useState(false);

  usePolling(
    async (signal) => {
      setTasks(await api.listTasks({ limit: 200 }, { signal }));
      setError(null);
    },
    {
      intervalMs: 10000,
      onError: (e) => {
        setError(e instanceof Error ? e.message : "Could not load tasks.");
        setTasks((prev) => prev ?? []);
      },
    },
  );

  const { rows, subtaskCount } = useMemo(() => {
    const all = tasks ?? [];
    const counts = new Map<string, number>();
    all.forEach((t) => t.parent_id && counts.set(t.parent_id, (counts.get(t.parent_id) ?? 0) + 1));
    const q = query.trim().toLowerCase();
    const rows = all
      .filter((t) => !t.parent_id)
      .filter((t) => matches(t, filter))
      .filter((t) => !q || `${t.title} ${t.goal} ${t.manager_domain} ${t.id}`.toLowerCase().includes(q));
    return { rows, subtaskCount: counts };
  }, [tasks, filter, query]);

  return (
    <PageContainer>
      <PageHeader
        title="Tasks"
        description="Everything JJ and your team are working on."
        actions={
          <Button className="h-10" onClick={() => setNewOpen(true)}>
            <Plus /> New
          </Button>
        }
      />

      <div className="space-y-3">
        <Tabs value={filter} onValueChange={(v) => setFilter(v as Filter)}>
          <TabsList className="h-auto w-full justify-start gap-1 overflow-x-auto bg-transparent px-0 py-1">
            {FILTERS.map((f) => (
              <TabsTrigger
                key={f.value}
                value={f.value}
                className="h-10 flex-none rounded-full border px-4 data-[state=active]:border-primary data-[state=active]:bg-accent"
              >
                {f.label}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            aria-label="Search tasks"
            placeholder="Search tasks"
            className="h-11 pl-9"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>

        {error && <ErrorNote message={error} />}

        {tasks === null ? (
          <div className="space-y-2">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-16 w-full rounded-xl" />
            ))}
          </div>
        ) : rows.length === 0 ? (
          <EmptyState
            icon={<ListChecks />}
            title={tasks.length ? "Nothing matches" : "No tasks yet"}
            description={tasks.length ? "Try another filter or search." : "Create a task and it will show up here."}
            action={
              !tasks.length && (
                <Button onClick={() => setNewOpen(true)}>
                  <Plus /> New task
                </Button>
              )
            }
          />
        ) : (
          <div className="divide-y overflow-hidden rounded-xl border bg-card">
            {rows.map((t) => (
              <TaskRow key={t.id} task={t} subtasks={subtaskCount.get(t.id) ?? 0} />
            ))}
          </div>
        )}
      </div>

      <NewTaskDialog open={newOpen} onOpenChange={setNewOpen} onCreated={(t) => router.push(`/tasks/${t.id}`)} />
    </PageContainer>
  );
}
