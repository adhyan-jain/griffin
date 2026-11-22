"use client";

import { useMemo, useState } from "react";
import { Briefcase, ExternalLink, Loader2, X } from "lucide-react";
import { toast } from "sonner";
import { api, type Listing } from "@/lib/api";
import { usePolling } from "@/lib/use-polling";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, ErrorNote, PageContainer, PageHeader, timeAgo } from "@/components/page";

const TIERS = [
  { id: "all", label: "All" },
  { id: "A", label: "Tier A · ₹40k+" },
  { id: "B", label: "Tier B · ₹15k+" },
  { id: "C", label: "Tier C" },
] as const;
type TierFilter = (typeof TIERS)[number]["id"];

const STAGE_LABEL: Record<Listing["stage"], string> = {
  verified: "Open",
  drafted: "Draft ready",
  approved: "Approved",
  submitting: "Submitting",
  confirmed: "Applied",
  needs_you: "Needs you",
  closed: "Closed",
};

const SOURCE_LABEL: Record<string, string> = { internshala: "Internshala", unstop: "Unstop", interndock: "InternDock" };

export default function InternshipsPage() {
  const [items, setItems] = useState<Listing[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tier, setTier] = useState<TierFilter>("all");
  const [query, setQuery] = useState("");
  const [showClosed, setShowClosed] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);

  const { refresh } = usePolling(
    async (signal) => {
      setItems(await api.listListings({ signal }));
      setError(null);
    },
    {
      intervalMs: 30000,
      onError: (e) => {
        setError(e instanceof Error ? e.message : "Could not load internships.");
        setItems((prev) => prev ?? []);
      },
    },
  );

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (items ?? [])
      .filter((l) => showClosed || l.stage !== "closed")
      .filter((l) => tier === "all" || l.tier === tier)
      .filter((l) => !q || `${l.company} ${l.role} ${l.location}`.toLowerCase().includes(q))
      .sort((a, b) => (a.tier || "Z").localeCompare(b.tier || "Z") || b.created_at.localeCompare(a.created_at));
  }, [items, tier, query, showClosed]);

  const counts = useMemo(() => {
    const open = (items ?? []).filter((l) => l.stage !== "closed");
    return { all: open.length, A: open.filter((l) => l.tier === "A").length, B: open.filter((l) => l.tier === "B").length, C: open.filter((l) => l.tier === "C").length };
  }, [items]);

  const close = async (l: Listing) => {
    setBusyId(l.id);
    try {
      await api.closeListing(l.id);
      toast.success(`Closed ${l.role} at ${l.company}`);
      refresh();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not close it.");
    } finally {
      setBusyId(null);
    }
  };

  return (
    <PageContainer>
      <PageHeader
        title="Internships"
        description="Read every day from Internshala, Unstop and InternDock by code, never guessed. Only backend, AI and SDE roles in India or worldwide-remote, paying ₹10k+ a month."
      />
      {error && <ErrorNote message={error} />}

      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-wrap gap-2" role="tablist" aria-label="Stipend tier">
          {TIERS.map((t) => (
            <Button
              key={t.id}
              role="tab"
              aria-selected={tier === t.id}
              size="sm"
              variant={tier === t.id ? "default" : "outline"}
              onClick={() => setTier(t.id)}
            >
              {t.label} <span className="ml-1 tabular-nums opacity-70">{counts[t.id]}</span>
            </Button>
          ))}
        </div>
        <div className="flex items-center gap-2">
          <Input placeholder="Search company, role, city" value={query} onChange={(e) => setQuery(e.target.value)} className="sm:w-64" aria-label="Search internships" />
          <Button size="sm" variant="ghost" onClick={() => setShowClosed((v) => !v)}>
            {showClosed ? "Hide closed" : "Show closed"}
          </Button>
        </div>
      </div>

      {items === null ? (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-20 w-full rounded-xl" />
          ))}
        </div>
      ) : shown.length === 0 ? (
        <EmptyState
          icon={<Briefcase className="size-5" />}
          title="No internships here yet"
          description="The daily sync adds real postings that fit you. An empty list means the sites have none that fit today."
        />
      ) : (
        <ul className="space-y-3">
          {shown.map((l) => (
            <li key={l.id} className={cn("rounded-xl border bg-card p-4 shadow-xs", l.stage === "closed" && "opacity-60")}>
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge variant={l.tier === "A" ? "default" : "secondary"}>Tier {l.tier || "?"}</Badge>
                    <Badge variant="outline">{STAGE_LABEL[l.stage] ?? l.stage}</Badge>
                    <span className="text-xs text-muted-foreground">{SOURCE_LABEL[l.source] ?? l.source}</span>
                  </div>
                  <h2 className="mt-1.5 font-medium leading-snug">{l.role}</h2>
                  <p className="text-sm text-muted-foreground">
                    {l.company} · {l.location || "Location not shown"}
                  </p>
                  <p className="mt-1 text-sm tabular-nums">{l.pay || "Stipend not shown"}</p>
                  {l.stage_note && <p className="mt-1 text-xs text-muted-foreground">{l.stage_note}</p>}
                  <p className="mt-1 text-xs text-muted-foreground">Found {timeAgo(l.created_at)}{l.last_checked_at ? ` · checked ${timeAgo(l.last_checked_at)}` : ""}</p>
                </div>
                <div className="flex shrink-0 gap-2">
                  <Button asChild size="sm" variant="outline">
                    <a href={l.url} target="_blank" rel="noopener noreferrer">
                      <ExternalLink className="size-4" /> Open posting
                    </a>
                  </Button>
                  {l.stage !== "closed" && l.stage !== "confirmed" && (
                    <Button size="sm" variant="ghost" onClick={() => close(l)} disabled={busyId === l.id} aria-label={`Close ${l.role} at ${l.company}`}>
                      {busyId === l.id ? <Loader2 className="size-4 animate-spin" /> : <X className="size-4" />}
                    </Button>
                  )}
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </PageContainer>
  );
}
