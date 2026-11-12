"use client";

import { useState } from "react";
import { Check, Download, Pause, Pencil, Play, Plus, Send, X } from "lucide-react";
import { toast } from "sonner";
import { api, type OutreachCampaign, type OutreachStatus, type OutreachTarget } from "@/lib/api";
import { usePolling } from "@/lib/use-polling";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { EmptyState, ErrorNote, PageContainer, PageHeader, timeAgo } from "@/components/page";
import { ResponsiveDialog } from "@/components/responsive-dialog";

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

/** Outreach: who Griffin found, what it drafted, what you approved, what was sent and who answered. */
export default function OutreachPage() {
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

  const { refresh } = usePolling(
    async (signal) => {
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

  const campaign = campaigns?.find((c) => c.id === selected) ?? null;
  const rows = (targets ?? []).filter((t) => inTab(t, tab));

  const act = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true);
    try {
      await fn();
      toast.success(ok);
      refresh();
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

  return (
    <PageContainer>
      <PageHeader
        title="Outreach"
        description="Griffin finds people and drafts the messages. Nothing is sent until you approve it, and sending stays inside the daily limits."
      />
      {error && <ErrorNote message={error} />}

      <div className="mb-4 flex flex-wrap items-center gap-2">
        {(campaigns ?? []).map((c) => (
          <Button key={c.id} variant={c.id === selected ? "default" : "outline"} className="h-10 rounded-full" onClick={() => { setSelected(c.id); setTargets(null); refresh(); }}>
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
          description="Start one here, or ask in chat: “find 30 founders in fintech and cold-mail them”."
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
                      <TableCell>
                        <div className="font-medium">{t.name || t.handle}</div>
                        <div className="text-xs text-muted-foreground">{t.name ? t.handle : ""}</div>
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

      <ResponsiveDialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)} title={`Message to ${editing?.name || editing?.handle || ""}`} description="Changing an approved message sends it back for your approval.">
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
    </PageContainer>
  );
}
