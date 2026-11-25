"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Building2, Search, ExternalLink, Activity, ShieldCheck, AlertTriangle, Loader2, Layers } from "lucide-react";
import { api, type CompanyCard, type CompanyDetail } from "@/lib/api";
import { claimStatusView, type ClaimTone } from "@/lib/research";
import {
  evidenceList,
  keyFields,
  listingCountText,
  observationHistory,
  safeHref,
  type ObservationView,
} from "@/lib/companies";
import { usePolling } from "@/lib/use-polling";
import { cn } from "@/lib/utils";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState, ErrorNote, PageContainer, PageHeader } from "@/components/page";

const CLAIM_TONE: Record<ClaimTone, string> = {
  supported: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  unverified: "bg-amber-500/15 text-amber-700 dark:text-amber-400",
  conflicting: "bg-destructive/15 text-destructive",
  other: "bg-muted text-muted-foreground",
};

function ClaimBadge({ status }: { status: string }) {
  const v = claimStatusView(status);
  return (
    <Badge className={cn("text-[10px]", CLAIM_TONE[v.tone])}>
      {v.verified ? <ShieldCheck className="mr-1 size-3" /> : <AlertTriangle className="mr-1 size-3" />}
      {v.label}
    </Badge>
  );
}

/** Origin, source link, age and stale badge for one observation. */
function ObservationMeta({ o }: { o: ObservationView }) {
  return (
    <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[10px] text-muted-foreground">
      <span className="rounded border px-1.5 py-0.5">{o.origin}</span>
      {o.sourceUrl ? (
        <a
          href={o.sourceUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-0.5 text-primary hover:underline"
        >
          source <ExternalLink className="size-2.5" />
        </a>
      ) : (
        <span>no source link</span>
      )}
      <span>{o.age}</span>
      {o.stale && <Badge className="bg-amber-500/15 text-[10px] text-amber-700 dark:text-amber-400">stale</Badge>}
    </div>
  );
}

function KeyFields({ latest }: { latest: CompanyCard["latest_observations"] }) {
  return (
    <dl className="mt-4 space-y-2.5">
      {keyFields(latest).map((f) => (
        <div key={f.property} className="rounded-lg border bg-muted/20 p-2.5 text-xs">
          <dt className="font-mono text-[10px] uppercase tracking-wide text-muted-foreground">{f.label}</dt>
          {f.observation ? (
            <dd>
              <span className="font-medium text-foreground">{f.observation.value}</span>
              <ObservationMeta o={f.observation} />
            </dd>
          ) : (
            <dd className="text-muted-foreground">unknown</dd>
          )}
        </div>
      ))}
    </dl>
  );
}

export default function CompanyExplorerPage() {
  const [companies, setCompanies] = useState<CompanyCard[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [search, setSearch] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [detail, setDetail] = useState<CompanyDetail | null>(null);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  // The company whose detail is on screen; a response for any other id is stale and dropped.
  const activeIdRef = useRef<string | null>(null);

  usePolling(
    async (signal) => {
      try {
        const list = await api.listCompanies({ limit: 200 }, { signal });
        setCompanies(list);
        setLoaded(true);
        setError(null);
      } catch (e) {
        if (!signal.aborted) setError(e instanceof Error ? e.message : "Failed to load companies");
      }
    },
    { intervalMs: 15000 },
  );

  useEffect(() => {
    activeIdRef.current = selectedId;
    setDetail(null);
    setDetailError(null);
    if (!selectedId) {
      setLoadingDetail(false);
      return;
    }
    const ctrl = new AbortController();
    setLoadingDetail(true);
    api
      .getCompany(selectedId, { signal: ctrl.signal })
      .then((d) => {
        if (activeIdRef.current === selectedId) setDetail(d);
      })
      .catch((e) => {
        if (!ctrl.signal.aborted && activeIdRef.current === selectedId) {
          setDetailError(e instanceof Error ? e.message : "Could not load company details");
        }
      })
      .finally(() => {
        if (activeIdRef.current === selectedId) setLoadingDetail(false);
      });
    return () => ctrl.abort();
  }, [selectedId]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return companies;
    return companies.filter((c) => c.name.toLowerCase().includes(q) || (c.domain ?? "").toLowerCase().includes(q));
  }, [companies, search]);

  const history = useMemo(() => observationHistory(detail?.observations), [detail]);

  return (
    <PageContainer className="max-w-7xl">
      <PageHeader
        title="Company Explorer"
        description="Companies found by the opportunity scan. Funding, revenue, profitability, pay and hiring signals are separate facts, each with its source and age."
      />

      {error && (
        <div className="mb-6">
          <ErrorNote message={error} />
        </div>
      )}

      <div className="mb-8 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="relative flex-1 sm:max-w-md">
          <Search className="absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="Search by company name or domain..."
            aria-label="Search companies"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="h-11 pl-10 text-base"
          />
        </div>
        <span className="text-xs text-muted-foreground">{filtered.length} companies</span>
      </div>

      {!loaded && !error ? (
        <div className="grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-3" aria-busy="true">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-72 rounded-2xl" />
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={<Building2 className="size-5" />}
          title="No companies found"
          description={
            search
              ? "No companies match your search."
              : "No companies yet. They appear when the opportunity scan or a research run finds them."
          }
        />
      ) : (
        <div className="grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-3">
          {filtered.map((c) => (
            <button
              key={c.id}
              type="button"
              onClick={() => setSelectedId(c.id)}
              className="group flex flex-col rounded-2xl border bg-card p-5 text-left shadow-sm transition-all hover:border-primary/50 hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
            >
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <h3 className="truncate font-semibold text-foreground transition-colors group-hover:text-primary">
                    {c.name}
                  </h3>
                  <p className="truncate font-mono text-xs text-muted-foreground">{c.domain || (c as any).industry || "domain unknown"}</p>
                </div>
                <div className="flex flex-col items-end gap-1 shrink-0">
                  <Badge variant="outline" className="text-[11px]">
                    <Layers className="size-3" /> {listingCountText(c.open_listings)}
                  </Badge>
                  {((c as any).pay_potential === "HIGH" || c.latest_observations?.some((o) => o.property === "advertised_pay" || o.property === "employee_reported_pay")) ? (
                    <Badge variant="outline" className="border-emerald-500/30 bg-emerald-500/10 text-emerald-400 text-[10px]">
                      High Pay
                    </Badge>
                  ) : ((c as any).pay_potential === "MEDIUM") ? (
                    <Badge variant="outline" className="border-amber-500/30 bg-amber-500/10 text-amber-400 text-[10px]">
                      Med Pay
                    </Badge>
                  ) : null}
                </div>
              </div>
              <KeyFields latest={c.latest_observations} />
            </button>
          ))}
        </div>
      )}

      <Dialog open={selectedId !== null} onOpenChange={(open) => !open && setSelectedId(null)}>
        <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{detail?.company.name ?? "Company"}</DialogTitle>
            <DialogDescription className="font-mono text-xs">
              {detail ? detail.company.domain || "domain unknown" : "Loading company details"}
            </DialogDescription>
          </DialogHeader>

          {loadingDetail ? (
            <div className="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground" role="status">
              <Loader2 className="size-4 animate-spin" /> Loading company details...
            </div>
          ) : detailError ? (
            <ErrorNote message={detailError} />
          ) : detail ? (
            <div className="space-y-6">
              <section>
                <h4 className="mb-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  Latest, by property
                </h4>
                <KeyFields latest={detail.observations} />
              </section>

              <section>
                <h4 className="mb-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  Listings ({detail.open_listings} open)
                </h4>
                {detail.listings.length === 0 ? (
                  <p className="text-xs text-muted-foreground">unknown: no listings recorded.</p>
                ) : (
                  <ul className="space-y-2">
                    {detail.listings.map((l) => {
                      const official = safeHref(l.official_url);
                      const lead = safeHref(l.url);
                      return (
                        <li key={l.id} className="rounded-lg border p-2.5 text-xs">
                          <div className="flex items-start justify-between gap-2">
                            <span className="font-medium text-foreground">{l.role}</span>
                            <Badge variant="outline" className="text-[10px]">
                              {l.open ? "open" : "closed"}
                            </Badge>
                          </div>
                          <p className="text-muted-foreground">
                            {[l.location, l.pay, l.stage].filter(Boolean).join(" - ") || "unknown"}
                          </p>
                          <div className="mt-1 flex flex-wrap gap-3">
                            {official ? (
                              <a
                                href={official}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="inline-flex items-center gap-1 text-primary hover:underline"
                              >
                                Official posting <ExternalLink className="size-3" />
                              </a>
                            ) : (
                              <span className="text-muted-foreground">no official source (lead only)</span>
                            )}
                            {lead && lead !== official && (
                              <a
                                href={lead}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="inline-flex items-center gap-1 text-muted-foreground hover:underline"
                              >
                                Found via {l.source_kind.replace(/_/g, " ")} <ExternalLink className="size-3" />
                              </a>
                            )}
                          </div>
                        </li>
                      );
                    })}
                  </ul>
                )}
              </section>

              <section>
                <h4 className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  <Activity className="size-3.5 text-emerald-500" /> Observation history
                </h4>
                {history.length === 0 ? (
                  <p className="text-xs text-muted-foreground">unknown: nothing observed yet.</p>
                ) : (
                  <ul className="space-y-2">
                    {history.map((o, i) => (
                      <li key={i} className="rounded-lg border bg-muted/20 p-2.5 text-xs">
                        <span className="font-semibold text-foreground">{o.label}</span>
                        <span className="ml-2 text-foreground/80">{o.value}</span>
                        <ObservationMeta o={o} />
                      </li>
                    ))}
                  </ul>
                )}
              </section>

              <section>
                <h4 className="mb-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">Evidence</h4>
                {evidenceList(detail.evidence).length === 0 ? (
                  <p className="text-xs text-muted-foreground">unknown: no research evidence yet.</p>
                ) : (
                  <ul className="space-y-2">
                    {evidenceList(detail.evidence).map((ev) => {
                      const href = safeHref(ev.source_url);
                      return (
                        <li key={ev.id} className="rounded-lg border p-2.5 text-xs">
                          <div className="flex items-start justify-between gap-2">
                            <span className="font-medium text-foreground">{ev.claim}</span>
                            <ClaimBadge status={ev.status} />
                          </div>
                          {ev.value && <p className="text-foreground/80">{ev.value}</p>}
                          {ev.quote && (
                            <blockquote className="mt-1 border-l-2 pl-2 text-muted-foreground">{ev.quote}</blockquote>
                          )}
                          <div className="mt-1 flex items-center gap-3 text-[10px] text-muted-foreground">
                            <span>{ev.confidence}% confidence</span>
                            {href && (
                              <a
                                href={href}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="inline-flex items-center gap-0.5 text-primary hover:underline"
                              >
                                source <ExternalLink className="size-2.5" />
                              </a>
                            )}
                          </div>
                        </li>
                      );
                    })}
                  </ul>
                )}
              </section>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </PageContainer>
  );
}
