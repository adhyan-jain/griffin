"use client";

import { useEffect, useState } from "react";
import {
  Check,
  Download,
  ExternalLink,
  Sparkles,
  MessageSquareReply,
  Pause,
  Pencil,
  Play,
  Plus,
  Send,
  UserPlus,
  X,
  Search,
  Copy,
  FileText,
  Award,
  ShieldCheck,
  RefreshCw,
  Loader2,
  Users,
  Briefcase,
  Layers,
} from "lucide-react";
import { toast } from "sonner";
import {
  api,
  type OutreachCampaign,
  type OutreachStatus,
  type OutreachTarget,
  type VITCampaignStats,
  type VITCampaignTarget,
  type LinkedInSearchQuery,
} from "@/lib/api";
import { usePolling } from "@/lib/use-polling";
import { priorityTierBadge, gradEligibilityBadge } from "@/lib/alumni";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState, ErrorNote, PageContainer, PageHeader, timeAgo } from "@/components/page";
import { parsePeople } from "@/lib/outreach-parse";
import { ResponsiveDialog } from "@/components/responsive-dialog";

type Mode = "vit_alumni" | "general";

type Tab = "all" | "approve" | "sent" | "replied" | "followups";
const TABS: [Tab, string][] = [
  ["all", "All"],
  ["approve", "Needs approval"],
  ["sent", "Sent"],
  ["replied", "Replied"],
  ["followups", "Follow-ups"],
];
const inTab = (t: OutreachTarget, tab: Tab) =>
  tab === "all" ||
  (tab === "approve" && t.status === "drafted") ||
  (tab === "sent" && t.status === "sent") ||
  (tab === "replied" && t.status === "replied") ||
  (tab === "followups" && t.status === "followup_due");

const LABEL: Record<OutreachStatus, string> = {
  found: "Found",
  drafted: "Needs approval",
  approved: "Approved, queued",
  sent: "Sent",
  replied: "Replied",
  followup_due: "Follow up",
  closed: "Closed",
};

function csvDownload(name: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/csv" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = `${name.replace(/[^\w-]+/g, "_") || "outreach"}.csv`;
  a.click();
  URL.revokeObjectURL(url);
}

export default function OutreachPage() {
  const [mode, setMode] = useState<Mode>("vit_alumni");

  // VIT Alumni Campaign State
  const [vitStats, setVitStats] = useState<VITCampaignStats | null>(null);
  const [vitQueue, setVitQueue] = useState<VITCampaignTarget[]>([]);
  const [vitLoading, setVitLoading] = useState(false);
  const [building, setBuilding] = useState(false);
  const [queryModalOpen, setQueryModalOpen] = useState(false);
  const [queries, setQueries] = useState<LinkedInSearchQuery[]>([]);
  const [queueFilter, setQueueFilter] = useState<string>("all");
  const [copiedDraftId, setCopiedDraftId] = useState<string | null>(null);

  // General Campaign State
  const [campaigns, setCampaigns] = useState<OutreachCampaign[] | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [targets, setTargets] = useState<OutreachTarget[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<Tab>("all");
  const [editing, setEditing] = useState<OutreachTarget | null>(null);
  const [draft, setDraft] = useState("");
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState("");
  const [newGoal, setNewGoal] = useState("");
  const [newChannel, setNewChannel] = useState<OutreachCampaign["channel"]>("email");
  const [busy, setBusy] = useState(false);
  const [adding, setAdding] = useState(false);
  const [pasted, setPasted] = useState("");

  // Polling for general campaigns
  const { refresh: refreshGeneral } = usePolling(
    async (signal) => {
      if (mode !== "general") return;
      const cs = await api.listCampaigns({ signal });
      setCampaigns(cs);
      const id = selected && cs.some((c) => c.id === selected) ? selected : (cs[0]?.id ?? null);
      if (id !== selected) setSelected(id);
      setTargets(id ? await api.listTargets(id, { signal }) : []);
      setError(null);
    },
    {
      intervalMs: 8000,
      onError: (e) => {
        setError(e instanceof Error ? e.message : "Could not load outreach.");
        setCampaigns((p) => p ?? []);
        setTargets((p) => p ?? []);
      },
    },
  );

  // Load VIT Campaign data
  const loadVITData = async () => {
    setVitLoading(true);
    try {
      const [stats, queue] = await Promise.all([
        api.getVITCampaignStats(),
        api.getVITCampaignQueue(50),
      ]);
      setVitStats(stats);
      setVitQueue(queue);
    } catch (e) {
      // Fallback silently if the queue has not been built yet
    } finally {
      setVitLoading(false);
    }
  };

  useEffect(() => {
    if (mode === "vit_alumni") {
      loadVITData();
    }
  }, [mode]);

  const handleBuildQueue = async () => {
    setBuilding(true);
    try {
      const res = await api.buildVITQueue();
      setVitStats(res.stats);
      const queue = await api.getVITCampaignQueue(50);
      setVitQueue(queue);
      toast.success(
        res.queued > 0
          ? `Queue built from your records: ${res.queued} queued, ${res.needs_user_review} need your review.`
          : "No one in your records qualifies for the queue yet.",
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not build the queue");
    } finally {
      setBuilding(false);
    }
  };

  const handleOpenQueries = async () => {
    setQueryModalOpen(true);
    try {
      const res = await api.getLinkedInQueries();
      setQueries(res);
    } catch (e) {
      toast.error("Could not load search queries");
    }
  };

  const handleTargetAction = async (targetId: string, action: "approve" | "sent_manually" | "reply" | "dnc") => {
    try {
      await api.updateVITTargetAction(targetId, action);
      toast.success(`Target updated: ${action}`);
      loadVITData();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Action failed");
    }
  };

  const copyToClipboard = (text: string, id: string) => {
    navigator.clipboard.writeText(text);
    setCopiedDraftId(id);
    toast.success("Copied to clipboard");
    setTimeout(() => setCopiedDraftId(null), 2000);
  };

  const campaign = campaigns?.find((c) => c.id === selected) ?? null;
  const rows = (targets ?? []).filter((t) => inTab(t, tab));

  const act = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true);
    try {
      await fn();
      toast.success(ok);
      refreshGeneral();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "That did not work.");
    } finally {
      setBusy(false);
    }
  };

  const create = () =>
    act(async () => {
      const c = await api.createCampaign({ name: newName.trim(), goal: newGoal.trim(), channel: newChannel });
      setSelected(c.id);
      setCreating(false);
      setNewName("");
      setNewGoal("");
    }, "Campaign created");

  const filteredQueue = vitQueue.filter((t) => {
    if (queueFilter === "all") return true;
    if (queueFilter === "p1") return t.priority_tier === "P1";
    if (queueFilter === "p2") return t.priority_tier === "P2";
    if (queueFilter === "p3") return t.priority_tier === "P3";
    if (queueFilter === "p4") return t.priority_tier === "P4";
    if (queueFilter === "review") return t.priority_tier === "REVIEW";
    return true;
  });

  return (
    <PageContainer className="max-w-7xl">
      <PageHeader
        title="Outreach Campaigns & Intelligence"
        description="Priority outreach queues, draft proposals built from your own records, and LinkedIn search assistance. Griffin never automates sending or scraping: all messages are copy-ready for human review."
      />

      {error && <ErrorNote message={error} />}

      {/* Mode Switcher */}
      <div className="mb-6 flex items-center justify-between border-b pb-4">
        <div className="flex gap-2">
          <Button
            variant={mode === "vit_alumni" ? "default" : "outline"}
            onClick={() => setMode("vit_alumni")}
            className="gap-2 text-xs h-9 rounded-full"
          >
            <Award className="size-4 text-sky-400" /> VIT Alumni Campaign
          </Button>
          <Button
            variant={mode === "general" ? "default" : "outline"}
            onClick={() => setMode("general")}
            className="gap-2 text-xs h-9 rounded-full"
          >
            <Send className="size-4" /> General Campaigns
          </Button>
        </div>

        {mode === "vit_alumni" && (
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={handleOpenQueries}
              className="gap-1.5 text-xs h-9 rounded-full"
            >
              <Search className="size-3.5 text-primary" /> LinkedIn Search Helper
            </Button>
            <Button
              size="sm"
              onClick={handleBuildQueue}
              disabled={building}
              className="gap-1.5 text-xs h-9 rounded-full"
            >
              {building ? <Loader2 className="size-3.5 animate-spin" /> : <RefreshCw className="size-3.5" />}
              Build queue from my records
            </Button>
          </div>
        )}
      </div>

      {mode === "vit_alumni" ? (
        <div className="space-y-6">
          {/* Cohort Metrics Bar */}
          {vitStats && (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
              <div className="rounded-xl border bg-card p-3 shadow-sm">
                <span className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">People</span>
                <p className="mt-1 text-2xl font-bold text-foreground">{vitStats.total_people}</p>
                <span className="text-[10px] text-muted-foreground">{vitStats.confirmed_vellore} confirmed Vellore</span>
              </div>
              <div className="rounded-xl border bg-card p-3 shadow-sm">
                <span className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">ACM-VIT</span>
                <p className="mt-1 text-2xl font-bold text-sky-400">{vitStats.confirmed_acm}</p>
                <span className="text-[10px] text-muted-foreground">confirmed · {vitStats.likely_acm} likely</span>
              </div>
              <div className="rounded-xl border bg-card p-3 shadow-sm">
                <span className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">Eligible</span>
                <p className="mt-1 text-2xl font-bold text-emerald-400">{vitStats.eligible_cohort}</p>
                <span className="text-[10px] text-muted-foreground">{vitStats.borderline_2022} borderline 2022 · {vitStats.unverified} unverified</span>
              </div>
              <div className="rounded-xl border bg-card p-3 shadow-sm">
                <span className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">Functions</span>
                <p className="mt-1 text-lg font-bold text-foreground">{vitStats.tech_roles} Tech · {vitStats.ops_roles} Ops</p>
                <span className="text-[10px] text-muted-foreground">{vitStats.publicly_contactable} with a public contact route</span>
              </div>
              <div className="rounded-xl border bg-card p-3 shadow-sm">
                <span className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">Pay Evidence</span>
                <p className="mt-1 text-2xl font-bold text-amber-400">{vitStats.with_pay_evidence}</p>
                <span className="text-[10px] text-muted-foreground">company has a stored pay observation</span>
              </div>
              <div className="rounded-xl border bg-card p-3 shadow-sm">
                <span className="text-[11px] font-medium text-muted-foreground uppercase tracking-wider">Drafts</span>
                <p className="mt-1 text-2xl font-bold text-purple-400">{vitStats.drafts_ready}</p>
                <span className="text-[10px] text-muted-foreground">
                  {vitStats.needs_user_review} need review · {vitStats.approved} approved · {vitStats.messages_sent_manually} sent · {vitStats.replies_received} replied
                </span>
              </div>
            </div>
          )}

          {/* Queue Filter Controls */}
          <div className="flex flex-wrap items-center justify-between gap-3 bg-muted/20 p-2.5 rounded-xl border border-border">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-xs font-semibold text-muted-foreground mr-1">Filter Queue:</span>
              {[
                { id: "all", label: "All Candidates" },
                { id: "p1", label: "P1 (ACM + High Pot)" },
                { id: "p2", label: "P2 (ACM)" },
                { id: "p3", label: "P3 (Vellore High Pot)" },
                { id: "p4", label: "P4 (Vellore)" },
                { id: "review", label: "Review / Borderline" },
              ].map((f) => (
                <button
                  key={f.id}
                  onClick={() => setQueueFilter(f.id)}
                  className={cn(
                    "px-2.5 py-1 rounded-lg text-xs font-medium transition-colors",
                    queueFilter === f.id
                      ? "bg-primary text-primary-foreground font-semibold"
                      : "text-muted-foreground hover:text-foreground hover:bg-muted"
                  )}
                >
                  {f.label}
                </button>
              ))}
            </div>
            <span className="text-xs text-muted-foreground font-medium">
              Showing {filteredQueue.length} targets in staged pilot batch
            </span>
          </div>

          {/* Pilot Review Queue */}
          {vitLoading && vitQueue.length === 0 ? (
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              {[0, 1, 2, 3].map((i) => (
                <Skeleton key={i} className="h-64 rounded-2xl" />
              ))}
            </div>
          ) : filteredQueue.length === 0 ? (
            <EmptyState
              icon={<Users className="size-5" />}
              title="Queue is empty"
              description="Import alumni on the Alumni page or add them, then click 'Build queue from my records'. Only confirmed VIT Vellore, eligible, non-DNC people with no prior contact are queued; everyone else stays on the Alumni page for review."
            />
          ) : (
            <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
              {filteredQueue.map((target) => {
                const tier = priorityTierBadge(target.priority_tier);
                const elig = gradEligibilityBadge(target.graduation_eligibility);
                const isACM = target.acm_vit_affiliation === "confirmed";

                return (
                  <div
                    key={target.id}
                    className="flex flex-col justify-between rounded-2xl border bg-card p-5 shadow-sm space-y-4 hover:border-primary/40 transition-colors"
                  >
                    <div>
                      {/* Top Header */}
                      <div className="flex items-start justify-between gap-2">
                        <div>
                          <div className="flex items-center gap-1.5 flex-wrap">
                            <h3 className="font-semibold text-base text-foreground">{target.name}</h3>
                            {isACM && (
                              <Badge variant="outline" className="border-sky-500/30 bg-sky-500/10 text-sky-400 text-[10px] gap-1 py-0">
                                <Award className="size-3" /> ACM-VIT
                              </Badge>
                            )}
                          </div>
                          <p className="text-xs text-muted-foreground font-medium truncate mt-0.5">
                            {target.title} at <span className="text-foreground font-semibold">{target.company}</span>
                          </p>
                        </div>
                        <div className="flex flex-col items-end gap-1 shrink-0">
                          <Badge variant="outline" className={cn("text-[10px] font-semibold border", tier.color)}>
                            {tier.label}
                          </Badge>
                          {target.extended_status === "NEEDS_USER_REVIEW" && (
                            <Badge variant="outline" className="border-amber-500/40 bg-amber-500/10 text-amber-500 text-[10px]">
                              Needs your review
                            </Badge>
                          )}
                        </div>
                      </div>
                      {target.extended_status === "NEEDS_USER_REVIEW" && (
                        <p className="mt-2 text-[11px] text-amber-600 dark:text-amber-400">
                          This draft has a [placeholder]. Fill it in (or complete your owner profile) before sending.
                        </p>
                      )}

                      {/* Attribute Pills */}
                      <div className="mt-3 flex flex-wrap items-center gap-2 text-[11px]">
                        <span className="text-muted-foreground">Campus: <strong className="text-foreground capitalize">{target.campus}</strong></span>
                        <span className="text-muted-foreground">·</span>
                        <span className="text-muted-foreground">Batch: <strong className="text-foreground">{target.graduation_year ?? "Unknown"}</strong></span>
                        <Badge variant="outline" className={cn("text-[9px] py-0 border", elig.color)}>
                          {elig.label}
                        </Badge>
                        {target.pay_potential === "HIGH" && (
                          <Badge variant="outline" className="border-emerald-500/30 bg-emerald-500/10 text-emerald-400 text-[9px] py-0">
                            High pay (stored evidence)
                          </Badge>
                        )}
                      </div>

                      {target.acm_role && (
                        <div className="mt-2.5 text-[11px] text-sky-600 dark:text-sky-400 bg-sky-500/5 p-2 rounded-lg border border-sky-500/10 flex items-center gap-1.5">
                          <ShieldCheck className="size-3.5 shrink-0" />
                          <span>ACM-VIT role (as recorded): {target.acm_role}</span>
                        </div>
                      )}

                      {/* Selection Reason */}
                      {target.selection_reason && (
                        <p className="mt-2 text-xs text-muted-foreground bg-muted/30 p-2.5 rounded-lg border border-border italic">
                          &ldquo;{target.selection_reason}&rdquo;
                        </p>
                      )}

                      {/* Dual-Variant Draft Cards */}
                      <div className="mt-3.5 space-y-2.5">
                        {/* Variant A: Short Note */}
                        {target.short_note && (
                          <div className="rounded-xl border border-border p-3 bg-muted/20 space-y-1.5">
                            <div className="flex items-center justify-between">
                              <span className="text-[11px] font-semibold text-foreground flex items-center gap-1">
                                <FileText className="size-3 text-sky-400" /> Variant A: LinkedIn Connection Note
                              </span>
                              <Badge variant="outline" className="text-[9px]">
                                {target.short_note.length} / 300 chars
                              </Badge>
                            </div>
                            <p className="font-mono text-[11px] text-foreground/90 whitespace-pre-wrap leading-relaxed">
                              {target.short_note}
                            </p>
                            <div className="flex justify-end pt-1">
                              <Button
                                size="sm"
                                variant="outline"
                                onClick={() => copyToClipboard(target.short_note!, `${target.id}-short`)}
                                className="h-6 text-[10px] gap-1 px-2"
                              >
                                {copiedDraftId === `${target.id}-short` ? <Check className="size-2.5 text-emerald-400" /> : <Copy className="size-2.5" />}
                                {copiedDraftId === `${target.id}-short` ? "Copied" : "Copy Note"}
                              </Button>
                            </div>
                          </div>
                        )}

                        {/* Variant B: Warmer InMail / Email */}
                        {target.alternative_draft && (
                          <div className="rounded-xl border border-border p-3 bg-muted/20 space-y-1.5">
                            <div className="flex items-center justify-between">
                              <span className="text-[11px] font-semibold text-foreground flex items-center gap-1">
                                <FileText className="size-3 text-emerald-400" /> Variant B: Warmer InMail / Email
                              </span>
                            </div>
                            <p className="font-mono text-[11px] text-foreground/90 whitespace-pre-wrap leading-relaxed line-clamp-3 hover:line-clamp-none transition-all">
                              {target.alternative_draft}
                            </p>
                            <div className="flex justify-end pt-1">
                              <Button
                                size="sm"
                                variant="outline"
                                onClick={() => copyToClipboard(target.alternative_draft!, `${target.id}-warm`)}
                                className="h-6 text-[10px] gap-1 px-2"
                              >
                                {copiedDraftId === `${target.id}-warm` ? <Check className="size-2.5 text-emerald-400" /> : <Copy className="size-2.5" />}
                                {copiedDraftId === `${target.id}-warm` ? "Copied" : "Copy InMail"}
                              </Button>
                            </div>
                          </div>
                        )}
                      </div>
                    </div>

                    {/* Bottom Action Bar */}
                    <div className="border-t pt-3 flex flex-wrap items-center justify-between gap-2">
                      <a
                        href={target.profile_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1 text-xs text-primary font-medium hover:underline"
                      >
                        Open LinkedIn Profile <ExternalLink className="size-3" />
                      </a>

                      <div className="flex items-center gap-1.5">
                        {(target.extended_status === "DRAFTED" || target.extended_status === "NEEDS_USER_REVIEW") && (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => handleTargetAction(target.id, "approve")}
                            className="h-7 text-[11px] gap-1"
                          >
                            <Check className="size-3 text-emerald-400" /> Approve
                          </Button>
                        )}
                        {target.extended_status === "APPROVED" && (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => handleTargetAction(target.id, "sent_manually")}
                            className="h-7 text-[11px] gap-1"
                          >
                            <Send className="size-3 text-sky-400" /> Sent by Me
                          </Button>
                        )}
                        {(target.extended_status === "SENT_BY_USER" || target.extended_status === "FOLLOW_UP") && (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => handleTargetAction(target.id, "reply")}
                            className="h-7 text-[11px] gap-1"
                          >
                            <MessageSquareReply className="size-3 text-emerald-400" /> Replied
                          </Button>
                        )}
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => handleTargetAction(target.id, "dnc")}
                          className="h-7 text-[11px] text-destructive hover:bg-destructive/10"
                        >
                          DNC
                        </Button>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      ) : (
        /* General Campaigns Existing View */
        <>
          <div className="mb-4 flex flex-wrap items-center gap-2">
            {(campaigns ?? []).map((c) => (
              <Button
                key={c.id}
                variant={c.id === selected ? "default" : "outline"}
                className="h-10 rounded-full"
                onClick={() => { setSelected(c.id); setTargets(null); refreshGeneral(); }}
              >
                {c.name}
                {c.status !== "active" && <Badge variant="secondary" className="ml-2">{c.status}</Badge>}
              </Button>
            ))}
            <Button variant="ghost" className="h-10 rounded-full" onClick={() => setCreating(true)}>
              <Plus /> New campaign
            </Button>
          </div>

          {campaigns === null || targets === null ? (
            <Skeleton className="h-64 w-full rounded-xl" />
          ) : !campaign ? (
            <EmptyState
              icon={<Send />}
              title="No campaigns yet"
              description="Start one here, or switch to the VIT Alumni Campaign above."
            />
          ) : (
            <>
              <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                <p className="text-sm text-muted-foreground">
                  {campaign.goal || "No goal written."} Up to {campaign.limits.daily_cap} a day, at least{" "}
                  {Math.round(campaign.limits.min_gap_seconds / 60)} min apart, quiet {campaign.limits.quiet_start_hour}:00 to{" "}
                  {campaign.limits.quiet_end_hour}:00.
                </p>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    disabled={busy}
                    onClick={() => act(() => api.updateCampaign(campaign.id, { status: campaign.status === "active" ? "paused" : "active" }), campaign.status === "active" ? "Paused" : "Resumed")}
                  >
                    {campaign.status === "active" ? <Pause /> : <Play />} {campaign.status === "active" ? "Pause" : "Resume"}
                  </Button>
                  {!!campaign.counts.found && (
                    <Button disabled={busy} onClick={() => act(() => api.draftCampaign(campaign.id), "Drafts written. Review them under Needs approval.")}>
                      <Sparkles /> Draft {Math.min(campaign.counts.found, 10)} message{campaign.counts.found === 1 ? "" : "s"}
                    </Button>
                  )}
                  <Button variant="outline" onClick={() => setAdding(true)}>
                    <UserPlus /> Add people
                  </Button>
                  <Button variant="outline" onClick={() => api.exportCampaign(campaign.id).then((t) => csvDownload(campaign.name, t), (e) => toast.error(String(e.message ?? e)))}>
                    <Download /> CSV
                  </Button>
                </div>
              </div>

              <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)} className="mb-3">
                <TabsList className="h-auto flex-wrap gap-1 bg-transparent p-0">
                  {TABS.map(([v, label]) => (
                    <TabsTrigger key={v} value={v} className="h-9 rounded-full border px-3 data-[state=active]:border-primary data-[state=active]:bg-accent">
                      {label}
                      {v === "approve" && !!campaign.counts.drafted && <Badge className="ml-2">{campaign.counts.drafted}</Badge>}
                      {v === "replied" && !!campaign.counts.replied && <Badge className="ml-2">{campaign.counts.replied}</Badge>}
                    </TabsTrigger>
                  ))}
                </TabsList>
              </Tabs>

              {rows.length === 0 ? (
                <EmptyState icon={<Send />} title="Nobody here yet" description="People appear as Griffin finds them and as their status changes." />
              ) : (
                <div className="overflow-x-auto rounded-xl border">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Person</TableHead>
                        <TableHead>Status</TableHead>
                        <TableHead className="hidden md:table-cell">Message / reply</TableHead>
                        <TableHead className="text-right">Actions</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {rows.map((t) => (
                        <TableRow key={t.id}>
                          <TableCell className="max-w-xs">
                            <div className="flex items-center gap-1.5 font-medium">
                              <span>{t.name || t.handle}</span>
                              {t.handle && t.handle.startsWith("http") && (
                                <a
                                  href={t.handle}
                                  target="_blank"
                                  rel="noopener noreferrer"
                                  className="inline-flex items-center text-muted-foreground hover:text-primary"
                                  title="Open profile"
                                >
                                  <ExternalLink className="size-3" />
                                </a>
                              )}
                            </div>
                            {t.name && !t.handle.startsWith("http") && (
                              <div className="text-xs text-muted-foreground">{t.handle}</div>
                            )}
                            {t.info && (
                              <div className="mt-1 text-xs text-muted-foreground leading-snug line-clamp-2">
                                {t.info}
                              </div>
                            )}
                          </TableCell>
                          <TableCell>
                            <Badge variant={t.status === "replied" ? "default" : "secondary"}>{LABEL[t.status]}</Badge>
                            <div className="mt-1 text-xs text-muted-foreground">{timeAgo(t.replied_at ?? t.sent_at ?? t.updated_at)}</div>
                          </TableCell>
                          <TableCell className="hidden max-w-sm md:table-cell">
                            <p className="line-clamp-2 whitespace-pre-wrap text-sm">{t.status === "replied" ? t.reply_text : t.draft}</p>
                          </TableCell>
                          <TableCell className="text-right">
                            <div className="flex justify-end gap-1">
                              {(t.status === "drafted" || t.status === "approved") && (
                                <Button size="sm" variant="outline" onClick={() => { setEditing(t); setDraft(t.draft ?? ""); }}>
                                  <Pencil /> Edit
                                </Button>
                              )}
                              {t.status === "drafted" && (
                                <Button size="sm" disabled={busy} onClick={() => act(() => api.updateTarget(t.id, { status: "approved" }), "Approved. It will go out within the limits.")}>
                                  <Check /> Approve
                                </Button>
                              )}
                              {(t.status === "sent" || t.status === "followup_due") && (
                                <Button size="sm" variant="outline" disabled={busy} onClick={() => act(() => api.updateTarget(t.id, { status: "replied" }), "Marked as replied")}>
                                  <MessageSquareReply /> Replied
                                </Button>
                              )}
                              {t.status !== "closed" && (
                                <Button size="sm" variant="ghost" aria-label="Close this person" disabled={busy} onClick={() => act(() => api.updateTarget(t.id, { status: "closed" }), "Closed")}>
                                  <X />
                                </Button>
                              )}
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </>
          )}

          <ResponsiveDialog
            open={!!editing}
            onOpenChange={(o) => !o && setEditing(null)}
            title={`Message to ${editing?.name || editing?.handle || ""}`}
            description={editing?.info ? `${editing.info} · Changing an approved message sends it back for your approval.` : "Changing an approved message sends it back for your approval."}
          >
            <Textarea aria-label="Message" rows={8} value={draft} onChange={(e) => setDraft(e.target.value)} />
            <div className="mt-3 flex justify-end gap-2">
              <Button variant="ghost" onClick={() => setEditing(null)}>Cancel</Button>
              <Button
                disabled={busy || !draft.trim() || !editing}
                onClick={() =>
                  editing &&
                  act(async () => {
                    await api.updateTarget(editing.id, { status: "drafted", draft });
                    setEditing(null);
                  }, "Draft saved")
                }
              >
                Save draft
              </Button>
            </div>
          </ResponsiveDialog>

          <ResponsiveDialog open={adding} onOpenChange={setAdding} title="Add people" description="One per line: Name <email>, Name, email, or just an address or profile link. Griffin drafts a message for each, and you approve before anything is sent.">
            <Textarea aria-label="People" rows={8} placeholder={"Ada Lovelace <ada@example.com>\nhttps://linkedin.com/in/someone"} value={pasted} onChange={(e) => setPasted(e.target.value)} />
            <Button
              className="mt-3 w-full"
              disabled={busy || !campaign || parsePeople(pasted).length === 0}
              onClick={() =>
                campaign &&
                act(async () => {
                  await api.addTargets(campaign.id, parsePeople(pasted));
                  setPasted("");
                  setAdding(false);
                }, "People added")
              }
            >
              Add {parsePeople(pasted).length || ""} {parsePeople(pasted).length === 1 ? "person" : "people"}
            </Button>
          </ResponsiveDialog>

          <ResponsiveDialog open={creating} onOpenChange={setCreating} title="New campaign" description="You can also just ask Griffin in chat.">
            <div className="space-y-3">
              <Input aria-label="Name" placeholder="Name, e.g. Fintech founders" value={newName} onChange={(e) => setNewName(e.target.value)} />
              <Textarea aria-label="Goal" placeholder="What is this for? Who should be contacted?" rows={3} value={newGoal} onChange={(e) => setNewGoal(e.target.value)} />
              <div className="flex flex-wrap gap-2">
                {(["email", "linkedin", "whatsapp", "instagram"] as const).map((c) => (
                  <Button key={c} type="button" size="sm" variant={newChannel === c ? "default" : "outline"} onClick={() => setNewChannel(c)}>
                    {c}
                  </Button>
                ))}
              </div>
              <Button className="w-full" disabled={busy || !newName.trim()} onClick={create}>
                Create campaign
              </Button>
            </div>
          </ResponsiveDialog>
        </>
      )}

      {/* LinkedIn Search Queries Dialog */}
      <Dialog open={queryModalOpen} onOpenChange={setQueryModalOpen}>
        <DialogContent className="sm:max-w-2xl max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Search className="size-4 text-primary" /> LinkedIn Boolean Search Query Helper
            </DialogTitle>
            <DialogDescription>
              Copy-ready Boolean operator search queries and direct links for manual LinkedIn exploration.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 text-xs">
            {queries.map((q, idx) => (
              <div key={idx} className="rounded-xl border border-border p-3.5 space-y-2 bg-card">
                <div className="flex items-center justify-between">
                  <span className="font-semibold text-foreground">{q.category}</span>
                  <a
                    href={q.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-flex items-center gap-1 text-[11px] text-primary hover:underline font-medium"
                  >
                    Open in LinkedIn <ExternalLink className="size-3" />
                  </a>
                </div>
                <p className="text-muted-foreground text-[11px]">{q.target_audience}</p>
                <div className="rounded bg-muted/40 p-2.5 font-mono text-[11px] text-foreground/90 break-all border border-border">
                  {q.query}
                </div>
                <div className="flex items-center justify-between pt-1">
                  <span className="text-[10px] text-muted-foreground italic">{q.instructions}</span>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      navigator.clipboard.writeText(q.query);
                      toast.success("Query copied to clipboard");
                    }}
                    className="h-7 text-xs gap-1.5"
                  >
                    <Copy className="size-3" /> Copy Query
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </DialogContent>
      </Dialog>
    </PageContainer>
  );
}
