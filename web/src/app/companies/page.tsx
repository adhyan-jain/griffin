"use client";

import { useMemo, useState } from "react";
import {
  Building2,
  Search,
  ExternalLink,
  Layers,
  Calendar,
  Users,
  Activity,
  ChevronRight,
  TrendingUp,
  X,
} from "lucide-react";
import { api, type CompanyCard, type CompanyDetail } from "@/lib/api";
import { usePolling } from "@/lib/use-polling";
import { cn } from "@/lib/utils";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, ErrorNote, PageContainer, PageHeader, timeAgo } from "@/components/page";

export default function CompanyExplorerPage() {
  const [companies, setCompanies] = useState<CompanyCard[]>([]);
  const [search, setSearch] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [detail, setDetail] = useState<CompanyDetail | null>(null);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Poll company explorer list
  usePolling(
    async (signal) => {
      try {
        const list = await api.listCompanies({ signal });
        setCompanies(list);
        setError(null);
      } catch (e) {
        if (!signal.aborted) {
          setError(e instanceof Error ? e.message : "Failed to load companies");
        }
      }
    },
    { intervalMs: 15000 },
  );

  async function openCompany(id: string) {
    setSelectedId(id);
    setLoadingDetail(true);
    try {
      const data = await api.getCompany(id);
      setDetail(data);
    } catch {
      // Fallback
      setDetail(null);
    } finally {
      setLoadingDetail(false);
    }
  }

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return companies;
    return companies.filter((c) => {
      const name = c.name.toLowerCase();
      const domain = c.domain.toLowerCase();
      const summary = c.summary.toLowerCase();
      const meta = JSON.stringify(c.metadata ?? {}).toLowerCase();
      return name.includes(q) || domain.includes(q) || summary.includes(q) || meta.includes(q);
    });
  }, [companies, search]);

  return (
    <PageContainer className="max-w-7xl">
      <PageHeader
        title="Company Explorer"
        description="Search discovered organizations, canonical entities, verified tech stacks, funding milestones, and team relationships."
      />

      {error && <div className="mb-6"><ErrorNote message={error} /></div>}

      {/* Search Header */}
      <div className="mb-8 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="relative flex-1 sm:max-w-md">
          <Search className="absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="Search company, tech stack, domain, or funding..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-10 h-11 text-base"
          />
        </div>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span>{filtered.length} companies discovered</span>
        </div>
      </div>

      {/* Grid of Companies */}
      {filtered.length === 0 ? (
        <EmptyState
          icon={<Building2 className="size-5" />}
          title="No companies found"
          description={
            search
              ? "No companies match your search query."
              : "No companies have been cataloged yet. Run research tasks to populate company intelligence."
          }
        />
      ) : (
        <div className="grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-3">
          {filtered.map((c) => {
            const funding = c.metadata?.funding_stage as string | undefined;
            const location = c.metadata?.location as string | undefined;
            const tech = (c.metadata?.tech_stack as string[]) ?? [];

            return (
              <div
                key={c.id}
                onClick={() => openCompany(c.id)}
                className="group flex flex-col justify-between rounded-2xl border bg-card p-5 shadow-sm transition-all hover:border-primary/50 hover:shadow-md cursor-pointer"
              >
                <div>
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex items-center gap-2.5">
                      <div className="flex size-10 shrink-0 items-center justify-center rounded-xl border bg-muted/40 font-semibold text-primary">
                        {c.name.charAt(0).toUpperCase()}
                      </div>
                      <div>
                        <h3 className="font-semibold text-foreground group-hover:text-primary transition-colors">
                          {c.name}
                        </h3>
                        <p className="font-mono text-xs text-muted-foreground">{c.domain}</p>
                      </div>
                    </div>
                    {funding && (
                      <Badge variant="outline" className="text-[11px] font-medium shrink-0">
                        {funding}
                      </Badge>
                    )}
                  </div>

                  <p className="mt-3.5 line-clamp-2 text-sm text-muted-foreground">
                    {c.summary || "No summary available."}
                  </p>

                  {tech.length > 0 && (
                    <div className="mt-4 flex flex-wrap gap-1.5">
                      {tech.slice(0, 4).map((t) => (
                        <span
                          key={t}
                          className="rounded-md border bg-muted/30 px-2 py-0.5 text-[10px] font-mono text-muted-foreground"
                        >
                          {t}
                        </span>
                      ))}
                      {tech.length > 4 && (
                        <span className="text-[10px] text-muted-foreground">
                          +{tech.length - 4}
                        </span>
                      )}
                    </div>
                  )}
                </div>

                <div className="mt-5 flex items-center justify-between border-t pt-3 text-xs text-muted-foreground">
                  <span>{location ?? "Global"}</span>
                  <div className="flex items-center gap-1 font-medium text-primary">
                    <span>Inspect</span>
                    <ChevronRight className="size-3.5" />
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Detail Modal / Slide-over */}
      {selectedId && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
          <div className="w-full max-w-2xl rounded-2xl border bg-card p-6 shadow-xl max-h-[85vh] overflow-y-auto">
            <div className="flex items-start justify-between border-b pb-4">
              <div className="flex items-center gap-3">
                <div className="flex size-12 shrink-0 items-center justify-center rounded-2xl border bg-muted/50 text-xl font-bold text-primary">
                  {detail?.company.name.charAt(0) ?? "C"}
                </div>
                <div>
                  <h3 className="text-xl font-bold tracking-tight text-foreground">
                    {detail?.company.name ?? "Loading..."}
                  </h3>
                  <p className="font-mono text-xs text-muted-foreground">
                    {detail?.company.canonical_key}
                  </p>
                </div>
              </div>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => {
                  setSelectedId(null);
                  setDetail(null);
                }}
              >
                <X className="size-5" />
              </Button>
            </div>

            {loadingDetail ? (
              <div className="py-12 text-center text-sm text-muted-foreground">
                Loading organization intelligence...
              </div>
            ) : detail ? (
              <div className="mt-5 space-y-6">
                <div>
                  <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-1">
                    Summary
                  </h4>
                  <p className="text-sm text-foreground/90 leading-relaxed">
                    {detail.company.summary || "No overview recorded."}
                  </p>
                </div>

                {/* Metadata properties */}
                {detail.company.metadata && Object.keys(detail.company.metadata).length > 0 && (
                  <div>
                    <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-2">
                      Verified Metadata
                    </h4>
                    <div className="grid grid-cols-2 gap-2 text-xs">
                      {Object.entries(detail.company.metadata).map(([k, v]) => (
                        <div key={k} className="rounded-lg border bg-muted/20 p-2.5">
                          <span className="block font-mono text-[10px] text-muted-foreground capitalize">
                            {k.replace(/_/g, " ")}
                          </span>
                          <span className="font-medium text-foreground">
                            {Array.isArray(v) ? v.join(", ") : String(v)}
                          </span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {/* Graph Relationships */}
                <div>
                  <h4 className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-2">
                    <Users className="size-3.5 text-primary" /> Connected People & Relationships
                  </h4>
                  {detail.relationships.length > 0 ? (
                    <div className="space-y-2">
                      {detail.relationships.map((rel) => (
                        <div
                          key={rel.id}
                          className="flex items-center justify-between rounded-lg border p-2.5 text-xs"
                        >
                          <span className="font-medium text-foreground capitalize">
                            Predicate: {rel.predicate.replace(/_/g, " ")}
                          </span>
                          <Badge variant="outline" className="font-mono text-[10px]">
                            {rel.confidence}% conf
                          </Badge>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground">No graph edges recorded yet.</p>
                  )}
                </div>

                {/* Temporal Observations */}
                <div>
                  <h4 className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-2">
                    <Activity className="size-3.5 text-emerald-500" /> Observation Timeline
                  </h4>
                  {detail.observations.length > 0 ? (
                    <div className="space-y-2">
                      {detail.observations.map((obs) => (
                        <div
                          key={obs.id}
                          className="flex items-center justify-between rounded-lg border bg-muted/20 p-2.5 text-xs"
                        >
                          <div>
                            <span className="font-semibold text-foreground capitalize">
                              {obs.property.replace(/_/g, " ")}
                            </span>
                            <span className="text-muted-foreground ml-2">: {obs.value}</span>
                          </div>
                          <span className="text-[10px] text-muted-foreground">
                            {timeAgo(obs.observed_at)}
                          </span>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <p className="text-xs text-muted-foreground">No temporal observations recorded.</p>
                  )}
                </div>
              </div>
            ) : (
              <div className="py-8 text-center text-sm text-muted-foreground">
                Could not load company details.
              </div>
            )}
          </div>
        </div>
      )}
    </PageContainer>
  );
}
