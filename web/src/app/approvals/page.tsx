"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { usePolling } from "@/lib/use-polling";
import { Check, CheckCheck, Loader2, Pencil, ShieldCheck, X } from "lucide-react";
import { toast } from "sonner";
import { api, type Approval } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { RiskBadge, StatusBadge } from "@/components/StatusBadge";
import { EmptyState, ErrorNote, PageContainer, PageHeader, timeAgo } from "@/components/page";

/** The message a send approval is about, when it has one (other approvals carry no draft). */
function draftOf(a: Approval): { contact: string; text: string } | null {
  const p = a.action_payload;
  if (!p || typeof p !== "object") return null;
  const { text, contact } = p as { text?: unknown; contact?: unknown };
  return typeof text === "string" && text !== "" ? { text, contact: typeof contact === "string" ? contact : "" } : null;
}

export default function ApprovalsPage() {
  const [tab, setTab] = useState<"pending" | "all">("pending");
  const [items, setItems] = useState<Approval[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [notes, setNotes] = useState<Record<string, string>>({});
  const [busyId, setBusyId] = useState<string | null>(null);
  const [bulkBusy, setBulkBusy] = useState(false);
  const [editing, setEditing] = useState<Record<string, string>>({});

  const { refresh } = usePolling(
    async (signal) => {
      setItems(await api.listApprovals(tab === "pending" ? "PENDING" : undefined, { signal }));
      setError(null);
    },
    {
      intervalMs: 10000,
      onError: (e) => {
        setError(e instanceof Error ? e.message : "Could not load approvals.");
        setItems((prev) => prev ?? []);
      },
    },
  );

  // Switching the tab changes what to fetch: show the skeleton and fetch now. The loop itself
  // keeps running; it is not restarted.
  useEffect(() => {
    setItems(null);
    refresh();
  }, [tab, refresh]);

  const respond = async (a: Approval, approve: boolean) => {
    setBusyId(a.id);
    try {
      await api.respondApproval(a.id, approve, notes[a.id]);
      toast.success(approve ? "Approved" : "Rejected");
      refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not record your decision.");
    } finally {
      setBusyId(null);
    }
  };

  const saveEdit = async (a: Approval) => {
    setBusyId(a.id);
    try {
      await api.editApproval(a.id, editing[a.id] ?? "");
      setEditing(({ [a.id]: _gone, ...rest }) => rest);
      toast.success("Draft updated. Approve it when you are happy.");
      refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save your edit.");
    } finally {
      setBusyId(null);
    }
  };

  const respondAll = async (approve: boolean) => {
    if (!items || items.length === 0 || bulkBusy) return;
    setBulkBusy(true);
    try {
      const res = await api.batchRespondApprovals(approve);
      toast.success(approve ? `Approved all ${res.approved} actions` : `Rejected all ${res.rejected} actions`);
      refresh();
    } catch {
      try {
        const pending = items.filter((a) => a.status === "PENDING");
        await Promise.allSettled(pending.map((a) => api.respondApproval(a.id, approve, "Bulk " + (approve ? "approved" : "rejected"))));
        toast.success(approve ? `Approved ${pending.length} actions` : `Rejected ${pending.length} actions`);
        refresh();
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Could not complete bulk decision.");
      }
    } finally {
      setBulkBusy(false);
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
        <div className="space-y-4">
          {tab === "pending" && items && items.length > 0 && (
            <div className="rounded-2xl border-2 border-primary/30 bg-primary/5 p-4 sm:p-5 shadow-sm">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <span className="flex size-7 items-center justify-center rounded-full bg-primary/20 text-primary">
                      <CheckCheck className="size-4" />
                    </span>
                    <h3 className="text-base font-semibold text-foreground">
                      Allow All Actions
                    </h3>
                    <span className="rounded-full bg-primary/20 px-2 py-0.5 text-xs font-semibold text-primary">
                      {items.length} pending
                    </span>
                  </div>
                  <p className="text-xs sm:text-sm text-muted-foreground">
                    Approve and execute all queued decisions at once, or review and decide individually below.
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <Button
                    variant="outline"
                    className="h-10 text-muted-foreground hover:text-destructive"
                    disabled={bulkBusy || busyId !== null}
                    onClick={() => respondAll(false)}
                  >
                    <X className="size-4" /> Reject All
                  </Button>
                  <Button
                    className="h-10 bg-primary font-medium text-primary-foreground shadow-sm hover:bg-primary/90"
                    disabled={bulkBusy || busyId !== null}
                    onClick={() => respondAll(true)}
                  >
                    {bulkBusy ? <Loader2 className="size-4 animate-spin" /> : <CheckCheck className="size-4" />}
                    Allow All ({items.length})
                  </Button>
                </div>
              </div>
            </div>
          )}

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
                {(() => {
                  const draft = draftOf(a);
                  if (!draft) return null;
                  const isEditing = a.id in editing;
                  return (
                    <div className="space-y-2 rounded-lg border bg-muted/40 p-3">
                      <p className="text-xs font-medium text-muted-foreground">
                        {draft.contact ? `Message to ${draft.contact}` : "Message"}
                      </p>
                      {isEditing ? (
                        <>
                          <Textarea
                            aria-label="Edit the message"
                            rows={4}
                            value={editing[a.id]}
                            onChange={(e) => setEditing((m) => ({ ...m, [a.id]: e.target.value }))}
                          />
                          <div className="flex gap-2">
                            <Button size="sm" disabled={busyId === a.id || !editing[a.id]?.trim()} onClick={() => saveEdit(a)}>
                              Save draft
                            </Button>
                            <Button
                              size="sm"
                              variant="ghost"
                              onClick={() => setEditing(({ [a.id]: _gone, ...rest }) => rest)}
                            >
                              Cancel
                            </Button>
                          </div>
                        </>
                      ) : (
                        <>
                          <p className="whitespace-pre-wrap text-sm">{draft.text}</p>
                          {pending && (
                            <Button size="sm" variant="outline" onClick={() => setEditing((m) => ({ ...m, [a.id]: draft.text }))}>
                              <Pencil /> Edit
                            </Button>
                          )}
                        </>
                      )}
                    </div>
                  );
                })()}
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
      </div>
    )}
  </PageContainer>
);
}
