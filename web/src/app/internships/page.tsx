"use client";

import { useMemo, useState } from "react";
import { BadgeCheck, Briefcase, ExternalLink, FilePenLine, Link2, Loader2, X } from "lucide-react";
import { toast } from "sonner";
import { api, type Listing } from "@/lib/api";
import { usePolling } from "@/lib/use-polling";
import { packageAction, type PackageAction } from "@/lib/application-package";
import { cn } from "@/lib/utils";
import {
  BUCKETS,
  ELIGIBILITY_LABEL,
  ROLE_LABEL,
  bucketOf,
  byValue,
  eligibilityTone,
  isEmployerVerified,
  statedPay,
  unknownWeight,
  whyLines,
  type BucketId,
} from "@/lib/listing-value";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { EmptyState, ErrorNote, PageContainer, PageHeader, timeAgo } from "@/components/page";
import { PackageBadge, PackageDialog } from "@/components/application-package";

const STAGE_LABEL: Record<Listing["stage"], string> = {
  verified: "Open",
  drafted: "Draft ready",
  approved: "Approved",
  submitting: "Submitting",
  confirmed: "Applied",
  needs_you: "Needs you",
  closed: "Closed",
};

const SOURCE_LABEL: Record<string, string> = {
  internshala: "Internshala",
  unstop: "Unstop",
  interndock: "InternDock",
  greenhouse: "Greenhouse",
  lever: "Lever",
  lever_eu: "Lever",
  ashby: "Ashby",
  smartrecruiters: "SmartRecruiters",
  workday: "Workday",
  recruitee: "Recruitee",
  workable: "Workable",
  careers_jsonld: "Careers page",
};

const EMPTY: Record<BucketId, string> = {
  exceptional: "Nothing here yet. Exceptional is for deep engineering work at a strong employer that fits you.",
  strong: "Nothing strong yet.",
  good: "Nothing good yet.",
  exploratory: "Nothing here. Exploratory listings are real but thin on detail.",
  low: "Nothing here.",
  review: "Nothing waits for review. Listings whose eligibility the posting does not state land here instead of being dropped.",
  ineligible: "Nothing here. Listings closed to you (a graduation year, a country, a degree) are kept here with the words that rule them out.",
};

function ScoreBadge({ score, tone }: { score: number; tone: BucketId }) {
  return (
    <div
      className={cn(
        "flex size-12 shrink-0 flex-col items-center justify-center rounded-lg border text-center tabular-nums",
        tone === "exceptional" && "border-primary bg-primary text-primary-foreground",
        tone === "strong" && "border-primary/60 bg-primary/15",
        tone === "good" && "bg-accent",
        (tone === "exploratory" || tone === "low" || tone === "review" || tone === "ineligible") && "bg-muted text-muted-foreground",
      )}
      aria-label={`Value score ${score} out of 100`}
    >
      <span className="text-lg font-semibold leading-none">{score}</span>
      <span className="text-[10px] leading-tight opacity-70">/100</span>
    </div>
  );
}

function ListingCard({
  l,
  bucket,
  busy,
  preparing,
  onClose,
  onPackage,
}: {
  l: Listing;
  bucket: BucketId;
  busy: boolean;
  preparing: boolean;
  onClose: (l: Listing) => void;
  onPackage: (l: Listing, action: PackageAction) => void;
}) {
  const action = packageAction(l);
  const a = l.assessment;
  const verified = isEmployerVerified(l);
  const why = a ? whyLines(a, 3) : [];
  const unknownPts = a ? unknownWeight(a) : 0;
  const reason = a?.eligibility_reasons?.[0];
  const pay = statedPay(a) ?? (l.pay || null);
  return (
    <li className={cn("rounded-xl border bg-card p-4 shadow-xs", l.stage === "closed" && "opacity-60")}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 gap-3">
          {a ? <ScoreBadge score={a.value_score} tone={bucket} /> : <div className="flex size-12 shrink-0 items-center justify-center rounded-lg border bg-muted text-xs text-muted-foreground">New</div>}
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              {a ? <Badge variant={eligibilityTone(a.eligibility)}>{ELIGIBILITY_LABEL[a.eligibility]}</Badge> : <Badge variant="outline">Not assessed yet</Badge>}
              <Badge variant={verified ? "secondary" : "outline"} title={verified ? "Read from the employer's own posting" : "Found on a third-party site; the employer's own posting has not been read"}>
                {verified ? <BadgeCheck /> : <Link2 />}
                {verified ? "Employer-verified" : "Lead"}
              </Badge>
              <span className="text-xs text-muted-foreground">{SOURCE_LABEL[l.source] ?? l.source}</span>
              {a && a.role_class !== "unknown" && <span className="text-xs text-muted-foreground">· {ROLE_LABEL[a.role_class] ?? a.role_class}</span>}
              {l.stage !== "verified" && <Badge variant="outline">{STAGE_LABEL[l.stage] ?? l.stage}</Badge>}
              {l.package && <PackageBadge brief={l.package} />}
            </div>
            <h2 className="mt-1.5 font-medium leading-snug">{l.role}</h2>
            <p className="text-sm text-muted-foreground">
              {l.company} · {l.location || "Location not shown"}
              {a?.duration ? ` · ${a.duration}` : ""}
            </p>
            <p className="mt-1 text-sm tabular-nums">{pay ?? "Stipend not shown"}</p>
          </div>
        </div>
        <div className="flex shrink-0 flex-wrap gap-2">
          {action && (
            <Button size="sm" variant={action.kind === "prepare" ? "default" : "secondary"} onClick={() => onPackage(l, action)} disabled={preparing}>
              {preparing ? <Loader2 className="size-4 animate-spin" /> : <FilePenLine className="size-4" />} {preparing ? "Preparing" : action.label}
            </Button>
          )}
          <Button asChild size="sm" variant="outline">
            <a href={l.url} target="_blank" rel="noopener noreferrer">
              <ExternalLink className="size-4" /> Open posting
            </a>
          </Button>
          {l.stage !== "closed" && l.stage !== "confirmed" && (
            <Button size="sm" variant="ghost" onClick={() => onClose(l)} disabled={busy} aria-label={`Close ${l.role} at ${l.company}`}>
              {busy ? <Loader2 className="size-4 animate-spin" /> : <X className="size-4" />}
            </Button>
          )}
        </div>
      </div>

      {a && why.length > 0 && (
        <ul className="mt-3 space-y-1 border-t pt-3 text-sm" aria-label="Why this score">
          {why.map((w) => (
            <li key={w.label} className="flex gap-2">
              <span className="w-32 shrink-0 text-muted-foreground">{w.label}</span>
              <span className="min-w-0">{w.reason}</span>
            </li>
          ))}
          {unknownPts > 0 && <li className="text-xs text-muted-foreground">{unknownPts} of 100 points are not stated in the listing and are scored neutral.</li>}
        </ul>
      )}

      {a && (bucket === "review" || bucket === "ineligible") && reason && (
        <p className="mt-3 border-t pt-3 text-sm">
          <span className="text-muted-foreground">{bucket === "ineligible" ? "Why not eligible: " : "Why review: "}</span>
          {reason.reason}
          {reason.quote && <q className="ml-1 text-muted-foreground">{reason.quote}</q>}
        </p>
      )}
      {!a && <p className="mt-3 border-t pt-3 text-sm text-muted-foreground">Waiting for the next assessment run. Nothing is concluded about it yet.</p>}

      <p className="mt-2 text-xs text-muted-foreground">
        Found {timeAgo(l.created_at)}
        {l.last_checked_at ? ` · checked ${timeAgo(l.last_checked_at)}` : ""}
        {a?.model_used ? " · read with a model, every claim quoted from the posting" : ""}
      </p>
    </li>
  );
}

export default function InternshipsPage() {
  const [items, setItems] = useState<Listing[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<BucketId>("exceptional");
  const [query, setQuery] = useState("");
  const [showClosed, setShowClosed] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [preparingId, setPreparingId] = useState<string | null>(null);
  const [viewing, setViewing] = useState<Listing | null>(null);

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

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    return (items ?? [])
      .filter((l) => showClosed || l.stage !== "closed")
      .filter((l) => !q || `${l.company} ${l.role} ${l.location}`.toLowerCase().includes(q));
  }, [items, query, showClosed]);

  const counts = useMemo(() => {
    const c = Object.fromEntries(BUCKETS.map((b) => [b.id, 0])) as Record<BucketId, number>;
    for (const l of visible) c[bucketOf(l)]++;
    return c;
  }, [visible]);

  const shown = useMemo(() => visible.filter((l) => bucketOf(l) === tab).sort(byValue), [visible, tab]);

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

  const onPackage = async (l: Listing, action: PackageAction) => {
    if (action.kind === "view") {
      setViewing(l);
      return;
    }
    setPreparingId(l.id);
    try {
      const pkg = await api.prepareListing(l.id);
      toast.success(
        pkg.status === "needs_owner"
          ? `Package for ${l.company} is ready, but ${pkg.missing_information.length} things need you`
          : `Package for ${l.company} is waiting for your approval`,
      );
      refresh();
      setViewing(l);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not prepare the application.");
    } finally {
      setPreparingId(null);
    }
  };

  return (
    <PageContainer>
      <PageHeader
        title="Internships"
        description="Read every day by code and scored for your career, not for pay alone. Each score shows its reasons; what a listing does not say stays unknown and is never guessed."
      />
      {error && <ErrorNote message={error} />}

      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <Tabs value={tab} onValueChange={(v) => setTab(v as BucketId)}>
          <TabsList className="h-auto flex-wrap gap-1 bg-transparent p-0" aria-label="Value class">
            {BUCKETS.map((b) => (
              <TabsTrigger key={b.id} value={b.id} className="h-9 rounded-full border px-3 text-xs data-[state=active]:border-primary data-[state=active]:bg-accent">
                {b.label} <span className="ml-1 tabular-nums opacity-70">{counts[b.id]}</span>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
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
            <Skeleton key={i} className="h-24 w-full rounded-xl" />
          ))}
        </div>
      ) : shown.length === 0 ? (
        <EmptyState icon={<Briefcase className="size-5" />} title="No internships here" description={EMPTY[tab]} />
      ) : (
        <ul className="space-y-3">
          {shown.map((l) => (
            <ListingCard key={l.id} l={l} bucket={tab} busy={busyId === l.id} preparing={preparingId === l.id} onClose={close} onPackage={onPackage} />
          ))}
        </ul>
      )}
      <PackageDialog
        open={viewing !== null}
        onOpenChange={(o) => !o && setViewing(null)}
        title={viewing ? `${viewing.role} at ${viewing.company}` : "Application package"}
        listingId={viewing?.id}
        onChanged={refresh}
      />
    </PageContainer>
  );
}
