"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { usePolling } from "@/lib/use-polling";
import { Check, CheckCheck, ExternalLink, FileText, Loader2, Pencil, ShieldCheck, Sparkles, X } from "lucide-react";
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

interface TailoringInfo {
  company: string;
  role: string;
  tier: string;
  job_link: string;
  source_url: string;
  base_resume: string;
  bullet_diff: string;
  cover_note: string;
  screening_answers: Record<string, string>;
}

function tailoringOf(a: Approval): TailoringInfo | null {
  const p = a.action_payload;
  if (!p || typeof p !== "object") return null;
  const payload = (p as { payload?: unknown }).payload || p;
  if (!payload || typeof payload !== "object") return null;
  const t = payload as Record<string, unknown>;
  if (typeof t.company === "string" && (typeof t.bullet_diff === "string" || typeof t.cover_note === "string")) {
    return {
      company: String(t.company),
      role: typeof t.role === "string" ? t.role : "",
      tier: typeof t.tier === "string" ? t.tier : "",
      job_link: typeof t.job_link === "string" ? t.job_link : "",
      source_url: typeof t.source_url === "string" ? t.source_url : "",
      base_resume: typeof t.base_resume === "string" ? t.base_resume : "",
      bullet_diff: typeof t.bullet_diff === "string" ? t.bullet_diff : "",
      cover_note: typeof t.cover_note === "string" ? t.cover_note : "",
      screening_answers:
        t.screening_answers && typeof t.screening_answers === "object"
          ? (t.screening_answers as Record<string, string>)
          : {},
    };
  }
  return null;
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
                  const tail = tailoringOf(a);
                  if (!tail) return null;
                  return (
                    <div className="space-y-3 rounded-xl border border-primary/20 bg-muted/30 p-4">
                      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border/60 pb-2">
                        <div className="flex items-center gap-2">
                          <span className="flex size-6 items-center justify-center rounded-full bg-primary/20 text-primary">
                            <Sparkles className="size-3.5" />
                          </span>
                          <span className="text-xs font-semibold uppercase tracking-wider text-primary">
                            Tailored Application Card
                          </span>
                        </div>
                        {tail.tier && (
                          <span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-semibold text-primary">
                            {tail.tier}
                          </span>
                        )}
                      </div>

                      {/* Role & Company & Links */}
                      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
                        {tail.base_resume && (
                          <span className="inline-flex items-center gap-1 font-mono text-foreground">
                            <FileText className="size-3.5 text-muted-foreground" />
                            {tail.base_resume}
                          </span>
                        )}
                        {tail.source_url && (
                          <a
                            href={tail.source_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 text-primary hover:underline"
                          >
                            <ExternalLink className="size-3.5" />
                            Source Reference
                          </a>
                        )}
                        {tail.job_link && (
                          <a
                            href={tail.job_link}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 text-primary hover:underline"
                          >
                            <ExternalLink className="size-3.5" />
                            Job Listing
                          </a>
                        )}
                      </div>

                      {/* Bullet Diff */}
                      {tail.bullet_diff && (
                        <div className="space-y-1">
                          <p className="text-xs font-semibold text-muted-foreground">Resume Bullet Diff (Claude CLI)</p>
                          <div className="rounded-lg border bg-background/80 p-3 font-mono text-xs leading-relaxed text-foreground whitespace-pre-wrap">
                            {tail.bullet_diff}
                          </div>
                        </div>
                      )}

                      {/* Sourced Cover Note */}
                      {tail.cover_note && (
                        <div className="space-y-1">
                          <p className="text-xs font-semibold text-muted-foreground">
                            Cover Note (Max 90 words with grounded source citation)
                          </p>
                          <div className="rounded-lg border bg-background/80 p-3 text-xs leading-relaxed text-foreground whitespace-pre-wrap">
                            {tail.cover_note}
                          </div>
                        </div>
                      )}

                      {/* Screening Answers */}
                      {Object.keys(tail.screening_answers).length > 0 && (
                        <div className="space-y-2">
                          <p className="text-xs font-semibold text-muted-foreground">Screening Answers</p>
                          <div className="space-y-2 rounded-lg border bg-background/80 p-3 text-xs">
                            {Object.entries(tail.screening_answers).map(([q, ans], i) => (
                              <div key={i} className="border-b border-border/40 pb-2 last:border-b-0 last:pb-0">
                                <p className="font-medium text-foreground">Q: {q}</p>
                                <p className="mt-0.5 text-muted-foreground">A: {ans}</p>
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })()}
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
