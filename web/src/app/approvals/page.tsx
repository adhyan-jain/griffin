"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { usePolling } from "@/lib/use-polling";
import { Check, CheckCheck, Copy, ExternalLink, FileText, Loader2, MessageSquareDiff, Pencil, ShieldCheck, Sparkles, X } from "lucide-react";
import { toast } from "sonner";
import { api, type Approval } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { RiskBadge, StatusBadge } from "@/components/StatusBadge";
import { EmptyState, ErrorNote, PageContainer, PageHeader, timeAgo } from "@/components/page";

interface ReferralDraftInfo {
  contact: string;
  text: string;
  handle?: string;
  company?: string;
  role?: string;
  priority?: string;
  affiliation?: string;
  description?: string;
  resume_path?: string;
  portal_url?: string;
}

/** The message a send approval is about, when it has one (other approvals carry no draft). */
function draftOf(a: Approval): ReferralDraftInfo | null {
  const p = a.action_payload;
  if (!p || typeof p !== "object") return null;
  const raw = p as Record<string, unknown>;
  const text = typeof raw.text === "string" ? raw.text : "";
  if (!text) return null;
  return {
    text,
    contact: typeof raw.contact === "string" ? raw.contact : "",
    handle: typeof raw.handle === "string" ? raw.handle : undefined,
    company: typeof raw.company === "string" ? raw.company : undefined,
    role: typeof raw.role === "string" ? raw.role : undefined,
    priority: typeof raw.priority === "string" ? raw.priority : undefined,
    affiliation: typeof raw.affiliation === "string" ? raw.affiliation : undefined,
    description: typeof raw.description === "string" ? raw.description : undefined,
    resume_path: typeof raw.resume_path === "string" ? raw.resume_path : undefined,
    portal_url: typeof raw.portal_url === "string" ? raw.portal_url : undefined,
  };
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
  location?: string;
  stipend?: string;
}

function tailoringOf(a: Approval): TailoringInfo | null {
  const p = a.action_payload;
  if (!p || typeof p !== "object") return null;
  const payload = (p as { payload?: unknown }).payload || p;
  if (!payload || typeof payload !== "object") return null;
  const t = payload as Record<string, unknown>;
  const company = typeof t.company === "string" ? t.company : "";
  const portalUrl = typeof t.portal_url === "string" ? t.portal_url : (typeof t.job_link === "string" ? t.job_link : (typeof t.source_url === "string" ? t.source_url : ""));
  const resume = typeof t.resume_sent === "string" ? t.resume_sent : (typeof t.base_resume === "string" ? t.base_resume : (typeof t.resume_path === "string" ? t.resume_path : ""));
  if (company && (portalUrl || resume || typeof t.bullet_diff === "string" || typeof t.cover_note === "string" || typeof t.text === "string")) {
    return {
      company,
      role: typeof t.role === "string" ? t.role : "",
      tier: typeof t.tier === "string" ? t.tier : "",
      job_link: portalUrl,
      source_url: typeof t.source_url === "string" ? t.source_url : portalUrl,
      base_resume: resume,
      bullet_diff: typeof t.bullet_diff === "string" ? t.bullet_diff : "",
      cover_note: typeof t.cover_note === "string" ? t.cover_note : (typeof t.text === "string" ? t.text : ""),
      screening_answers:
        t.screening_answers && typeof t.screening_answers === "object"
          ? (t.screening_answers as Record<string, string>)
          : {},
      location: typeof t.location === "string" ? t.location : undefined,
      stipend: typeof t.stipend === "string" ? t.stipend : undefined,
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
  const [revising, setRevising] = useState<Record<string, string>>({});
  const [reviseOpen, setReviseOpen] = useState<Record<string, boolean>>({});
  const [revisingBusyId, setRevisingBusyId] = useState<string | null>(null);

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

  const askJJRevise = async (a: Approval) => {
    const instruction = revising[a.id]?.trim();
    if (!instruction) return;
    setRevisingBusyId(a.id);
    try {
      await api.chatReviseApproval(a.id, instruction);
      setRevising((m) => ({ ...m, [a.id]: "" }));
      setReviseOpen((m) => ({ ...m, [a.id]: false }));
      toast.success("JJ revised the content! Review the updated pitch below.");
      refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not revise with JJ.");
    } finally {
      setRevisingBusyId(null);
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

          <ul className="space-y-4">
          {[...items].sort((a, b) => {
            // Show Phase 1 (JOB_APPLY) first so user can apply to the portal before messaging
            const aIsJob = a.action_type === "JOB_APPLY" ? 0 : 1;
            const bIsJob = b.action_type === "JOB_APPLY" ? 0 : 1;
            if (aIsJob !== bIsJob) return aIsJob - bIsJob;
            return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
          }).map((a) => {
            const pending = a.status === "PENDING";
            const isJobApply = a.action_type === "JOB_APPLY";
            return (
              <li key={a.id} className="space-y-3 rounded-xl border bg-card p-4 sm:p-5 shadow-xs">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <RiskBadge risk={a.risk_level} />
                    <span className={`rounded-full px-2.5 py-0.5 text-xs font-semibold ${isJobApply ? "bg-primary/20 text-primary border border-primary/30" : "bg-muted text-muted-foreground"}`}>
                      {isJobApply ? "Phase 1: Apply to Portal First" : "Phase 2: Referral Follow-Up"}
                    </span>
                    {!pending && <StatusBadge status={a.status} />}
                  </div>
                  <span className="text-xs text-muted-foreground">{timeAgo(a.created_at)}</span>
                </div>
                <div>
                  <p className="text-base font-semibold leading-snug text-foreground">{a.summary}</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {a.action_type.replaceAll("_", " ").toLowerCase()} ·{" "}
                    <Link href={`/tasks/${a.task_id}`} className="text-primary underline-offset-2 hover:underline">
                      View task details
                    </Link>
                  </p>
                  {a.decision_reason && <p className="mt-1 text-xs text-muted-foreground">Note: {a.decision_reason}</p>}
                </div>
                {(() => {
                  const tail = tailoringOf(a);
                  if (!tail) return null;
                  return (
                    <div className="space-y-3 rounded-xl border border-primary/30 bg-primary/5 p-4">
                      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border/60 pb-2">
                        <div className="flex items-center gap-2">
                          <span className="flex size-6 items-center justify-center rounded-full bg-primary/20 text-primary">
                            <Sparkles className="size-3.5" />
                          </span>
                          <span className="text-xs font-semibold uppercase tracking-wider text-primary">
                            Phase 1 Job Application Package
                          </span>
                        </div>
                        <div className="flex items-center gap-2">
                          {tail.tier && (
                            <span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-semibold text-primary">
                              {tail.tier}
                            </span>
                          )}
                          {tail.stipend && (
                            <span className="rounded-full bg-muted px-2 py-0.5 text-xs font-mono font-medium text-foreground">
                              {tail.stipend}
                            </span>
                          )}
                        </div>
                      </div>

                      {/* Direct Apply Button & Resume Link */}
                      <div className="flex flex-wrap items-center gap-2 pt-1">
                        {(tail.job_link || tail.source_url) && (
                          <a
                            href={tail.job_link || tail.source_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1.5 rounded-lg bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground shadow-sm hover:bg-primary/90"
                          >
                            <ExternalLink className="size-3.5" />
                            Open Application Portal (Apply Here First)
                          </a>
                        )}
                        {tail.base_resume && (
                          <div className="flex items-center gap-1.5 rounded-lg border bg-background px-2.5 py-1 text-xs font-mono text-foreground">
                            <FileText className="size-3.5 text-muted-foreground" />
                            <span className="font-semibold">{tail.base_resume.split("/").pop()}</span>
                            <button
                              type="button"
                              onClick={() => {
                                navigator.clipboard.writeText(tail.base_resume);
                                toast.success("Copied resume path to clipboard");
                              }}
                              className="ml-1 inline-flex items-center gap-1 rounded bg-muted/60 px-1.5 py-0.5 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground"
                              title={tail.base_resume}
                            >
                              <Copy className="size-3" />
                              Copy Path
                            </button>
                          </div>
                        )}
                        {tail.location && (
                          <span className="text-xs text-muted-foreground">
                            Location: {tail.location}
                          </span>
                        )}
                      </div>
                      {tail.base_resume && (
                        <p className="text-[11px] text-muted-foreground font-mono truncate" title={tail.base_resume}>
                          Resume location: {tail.base_resume}
                        </p>
                      )}

                      {/* Bullet Diff */}
                      {tail.bullet_diff && (
                        <div className="space-y-1">
                          <p className="text-xs font-semibold text-muted-foreground">Resume Bullet Highlights</p>
                          <div className="rounded-lg border bg-background/80 p-3 font-mono text-xs leading-relaxed text-foreground whitespace-pre-wrap">
                            {tail.bullet_diff}
                          </div>
                        </div>
                      )}

                      {/* Sourced Cover Note */}
                      {tail.cover_note && (
                        <div className="space-y-1">
                          <p className="text-xs font-semibold text-muted-foreground">
                            Cover Pitch
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
                    <div className="space-y-3 rounded-lg border bg-muted/40 p-3.5">
                      {/* Recipient Profile & Description Header */}
                      <div className="space-y-1.5 border-b border-border/50 pb-2.5">
                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <div className="flex items-center gap-2">
                            <span className="font-semibold text-sm text-foreground">
                              {draft.contact || "Outreach Recipient"}
                            </span>
                            {draft.priority && (
                              <span className="rounded-full bg-accent px-2 py-0.5 text-[11px] font-medium text-accent-foreground">
                                {draft.priority}
                              </span>
                            )}
                          </div>
                          {draft.handle && (
                            <a
                              href={draft.handle}
                              target="_blank"
                              rel="noopener noreferrer"
                              className="inline-flex items-center gap-1 text-xs font-medium text-primary hover:underline"
                            >
                              <ExternalLink className="size-3" />
                              View LinkedIn Profile
                            </a>
                          )}
                        </div>

                        {/* Recipient Background & Why Chosen */}
                        {(draft.description || draft.affiliation) && (
                          <p className="text-xs text-muted-foreground leading-relaxed">
                            <span className="font-medium text-foreground/80">Background: </span>
                            {draft.description || draft.affiliation}
                          </p>
                        )}

                        {/* Company, Target Role, and Workflow Guidance */}
                        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
                          {draft.company && draft.role && (
                            <span>Target: <strong className="text-foreground">{draft.company}</strong> ({draft.role})</span>
                          )}
                          {draft.resume_path && (
                            <span className="inline-flex items-center gap-1 font-mono">
                              <FileText className="size-3" />
                              Attached: {draft.resume_path.split("/").pop()}
                            </span>
                          )}
                        </div>

                        <div className="rounded-md bg-amber-500/10 px-2.5 py-1 text-[11px] text-amber-700 dark:text-amber-300">
                          <strong>Step 2 Notice:</strong> Apply to the job on the company portal first. Send this referral message after applying.
                        </div>
                      </div>

                      {/* Draft Body and Editor */}
                      {isEditing ? (
                        <>
                          <Textarea
                            aria-label="Edit the message"
                            rows={5}
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
                          <p className="whitespace-pre-wrap text-sm leading-relaxed text-foreground">{draft.text}</p>
                          {pending && (
                            <Button size="sm" variant="outline" className="h-8" onClick={() => setEditing((m) => ({ ...m, [a.id]: draft.text }))}>
                              <Pencil className="size-3.5" /> Edit Message
                            </Button>
                          )}
                        </>
                      )}
                    </div>
                  );
                })()}
                {pending && (
                  <>
                    <div className="flex flex-wrap items-center gap-2 pt-1">
                      <Button
                        size="sm"
                        variant="outline"
                        className="h-8 gap-1.5 border-primary/40 bg-primary/5 text-primary hover:bg-primary/10"
                        onClick={() => setReviseOpen((m) => ({ ...m, [a.id]: !m[a.id] }))}
                      >
                        <Sparkles className="size-3.5 text-primary" />
                        {reviseOpen[a.id] ? "Close Revision Chat" : "Revise with JJ"}
                      </Button>
                    </div>

                    {reviseOpen[a.id] && (
                      <div className="space-y-2 rounded-xl border border-primary/30 bg-primary/5 p-3.5 shadow-xs">
                        <div className="flex items-center gap-2">
                          <Sparkles className="size-4 text-primary" />
                          <span className="text-xs font-semibold uppercase tracking-wider text-primary">
                            Chat with JJ to Improve Pitch & Content
                          </span>
                        </div>
                        <p className="text-xs text-muted-foreground">
                          Tell JJ what to change (e.g., &quot;make it more concise&quot;, &quot;emphasize ComputeHive over LabTutor&quot;, &quot;highlight SGLang documentation work&quot;).
                        </p>
                        <Textarea
                          placeholder="Ask JJ to revise or tweak this pitch..."
                          rows={2}
                          value={revising[a.id] ?? ""}
                          onChange={(e) => setRevising((m) => ({ ...m, [a.id]: e.target.value }))}
                          className="text-xs bg-background/90"
                        />
                        <div className="flex justify-end gap-2">
                          <Button
                            size="sm"
                            variant="ghost"
                            className="h-8 text-xs"
                            onClick={() => setReviseOpen((m) => ({ ...m, [a.id]: false }))}
                          >
                            Cancel
                          </Button>
                          <Button
                            size="sm"
                            className="h-8 gap-1.5 text-xs"
                            disabled={revisingBusyId === a.id || !revising[a.id]?.trim()}
                            onClick={() => askJJRevise(a)}
                          >
                            {revisingBusyId === a.id ? <Loader2 className="size-3.5 animate-spin" /> : <Sparkles className="size-3.5" />}
                            Revise Draft
                          </Button>
                        </div>
                      </div>
                    )}

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
                        {busyId === a.id ? <Loader2 className="animate-spin" /> : <Check />} {isJobApply ? "Approve & Auto-Apply" : "Approve & Send"}
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
