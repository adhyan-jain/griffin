"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { Check, Loader2, ShieldCheck, X } from "lucide-react";
import { toast } from "sonner";
import { api, type Approval } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { RiskBadge, StatusBadge } from "@/components/StatusBadge";
import { EmptyState, ErrorNote, PageContainer, PageHeader, timeAgo } from "@/components/page";

export default function ApprovalsPage() {
  const [tab, setTab] = useState<"pending" | "all">("pending");
  const [items, setItems] = useState<Approval[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notes, setNotes] = useState<Record<string, string>>({});
  const [busyId, setBusyId] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setItems(await api.listApprovals(tab === "pending" ? "PENDING" : undefined));
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not load approvals.");
      setItems((prev) => prev ?? []);
    }
  }, [tab]);

  useEffect(() => {
    setItems(null);
    void load();
    const id = setInterval(load, 10000);
    return () => clearInterval(id);
  }, [load]);

  const respond = async (a: Approval, approve: boolean) => {
    setBusyId(a.id);
    try {
      await api.respondApproval(a.id, approve, notes[a.id]);
      toast.success(approve ? "Approved" : "Rejected");
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not record your decision.");
    } finally {
      setBusyId(null);
    }
  };

  return (
    <PageContainer className="max-w-3xl">
      <PageHeader title="Approvals" description="Actions your team wants a yes or no on before going ahead." />

      <Tabs value={tab} onValueChange={(v) => setTab(v as "pending" | "all")} className="mb-4">
        <TabsList className="h-auto gap-1 bg-transparent p-0">
          {[
            ["pending", "Waiting"],
            ["all", "History"],
          ].map(([v, label]) => (
            <TabsTrigger
              key={v}
              value={v}
              className="h-10 rounded-full border px-4 data-[state=active]:border-primary data-[state=active]:bg-accent"
            >
              {label}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>

      {error && <ErrorNote message={error} />}

      {items === null ? (
        <div className="space-y-3">
          <Skeleton className="h-36 w-full rounded-xl" />
          <Skeleton className="h-36 w-full rounded-xl" />
        </div>
      ) : items.length === 0 ? (
        <EmptyState
          icon={<ShieldCheck />}
          title={tab === "pending" ? "Nothing needs your approval" : "No approvals yet"}
          description="When a task wants to do something risky, it will ask here first."
        />
      ) : (
        <ul className="space-y-3">
          {items.map((a) => {
            const pending = a.status === "PENDING";
            return (
              <li key={a.id} className="space-y-3 rounded-xl border bg-card p-4">
                <div className="flex flex-wrap items-center gap-2">
                  <RiskBadge risk={a.risk_level} />
                  {!pending && <StatusBadge status={a.status} />}
                </div>
                <div>
                  <p className="text-sm font-medium leading-snug">{a.summary}</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {a.action_type.replaceAll("_", " ").toLowerCase()} · {timeAgo(a.created_at)} ·{" "}
                    <Link href={`/tasks/${a.task_id}`} className="text-primary underline-offset-2 hover:underline">
                      View task
                    </Link>
                  </p>
                  {a.decision_reason && <p className="mt-1 text-xs text-muted-foreground">Note: {a.decision_reason}</p>}
                </div>
                {pending && (
                  <>
                    <Input
                      aria-label="Note for this decision"
                      placeholder="Add a note (optional)"
                      className="h-11"
                      value={notes[a.id] ?? ""}
                      onChange={(e) => setNotes((n) => ({ ...n, [a.id]: e.target.value }))}
                    />
                    <div className="grid grid-cols-2 gap-2">
                      <Button variant="outline" className="h-11" disabled={busyId === a.id} onClick={() => respond(a, false)}>
                        <X /> Reject
                      </Button>
                      <Button className="h-11" disabled={busyId === a.id} onClick={() => respond(a, true)}>
                        {busyId === a.id ? <Loader2 className="animate-spin" /> : <Check />} Approve
                      </Button>
                    </div>
                  </>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </PageContainer>
  );
}
