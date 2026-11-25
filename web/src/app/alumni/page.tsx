"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import {
  GraduationCap,
  Search,
  Send,
  Building2,
  ExternalLink,
  ShieldCheck,
  CheckCircle2,
  Sparkles,
  UserCheck,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { api, type AlumniCard } from "@/lib/api";
import { usePolling } from "@/lib/use-polling";
import { cn } from "@/lib/utils";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, ErrorNote, PageContainer, PageHeader } from "@/components/page";

const FUNCTIONS = ["all", "engineering", "product", "operations", "founder"];

export default function AlumniExplorerPage() {
  const [alumni, setAlumni] = useState<AlumniCard[]>([]);
  const [search, setSearch] = useState("");
  const [fnFilter, setFnFilter] = useState("all");
  const [onlyEligible, setOnlyEligible] = useState(true); // <= 2022 cutoff
  const [selectedAlumnus, setSelectedAlumnus] = useState<AlumniCard | null>(null);
  const [draftText, setDraftText] = useState("");
  const [channel, setChannel] = useState("linkedin_dm");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Poll alumni explorer data
  usePolling(
    async (signal) => {
      try {
        const list = await api.listAlumni({ signal });
        setAlumni(list);
        setError(null);
      } catch (e) {
        if (!signal.aborted) {
          setError(e instanceof Error ? e.message : "Failed to load alumni");
        }
      }
    },
    { intervalMs: 15000 },
  );

  const filtered = useMemo(() => {
    return alumni.filter((a) => {
      // 1. Cutoff filter: <= 2022
      if (onlyEligible && a.graduation_year && a.graduation_year > 2022) {
        return false;
      }
      // 2. Function filter
      if (fnFilter !== "all") {
        const f = a.function?.toLowerCase() ?? "";
        if (!f.includes(fnFilter)) return false;
      }
      // 3. Search query
      if (search.trim()) {
        const q = search.trim().toLowerCase();
        const n = a.name.toLowerCase();
        const c = a.company.toLowerCase();
        const t = a.title.toLowerCase();
        if (!n.includes(q) && !c.includes(q) && !t.includes(q)) return false;
      }
      return true;
    });
  }, [alumni, onlyEligible, fnFilter, search]);

  function openOutreachModal(a: AlumniCard) {
    setSelectedAlumnus(a);
    setDraftText(
      `Hi ${a.name},\n\nI am a VIT student following your impactful work in ${a.function || "tech"} at ${a.company}. Would love to connect and learn more about your team's engineering challenges!\n\nBest,\nAdhyan`,
    );
  }

  async function handleProposeOutreach() {
    if (!selectedAlumnus) return;
    setSubmitting(true);
    try {
      await api.proposeAlumniOutreach(selectedAlumnus.id, {
        draft_text: draftText,
        channel,
      });
      toast.success("Outreach draft submitted to Approvals", {
        description: "Review and approve the outbound message on the Approvals page.",
        action: {
          label: "View Approvals",
          onClick: () => {
            window.location.href = "/approvals";
          },
        },
      });
      setSelectedAlumnus(null);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to propose outreach draft");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <PageContainer className="max-w-7xl">
      <PageHeader
        title="Alumni Intelligence"
        description="Verified alumni network from official and permitted sources. Filtered by organizational seniority (graduating class <= 2022) across tech, product, and operations roles."
      />

      {error && <div className="mb-6"><ErrorNote message={error} /></div>}

      {/* Filter and Search Bar */}
      <div className="mb-8 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="relative flex-1 sm:max-w-md">
          <Search className="absolute left-3.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            placeholder="Search alumni by name, company, or role..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-10 h-11 text-base"
          />
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {/* Function pills */}
          <div className="flex rounded-lg border bg-card p-1">
            {FUNCTIONS.map((f) => (
              <button
                key={f}
                type="button"
                onClick={() => setFnFilter(f)}
                className={cn(
                  "rounded-md px-3 py-1 text-xs font-medium capitalize transition-colors",
                  fnFilter === f
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {f}
              </button>
            ))}
          </div>

          {/* Cutoff Toggle */}
          <button
            type="button"
            onClick={() => setOnlyEligible(!onlyEligible)}
            className={cn(
              "flex items-center gap-1.5 rounded-lg border px-3 py-2 text-xs font-medium transition-colors",
              onlyEligible
                ? "border-primary bg-primary/10 text-primary font-semibold"
                : "border-border text-muted-foreground hover:bg-accent/40",
            )}
          >
            <ShieldCheck className="size-3.5" />
            <span>Class &le; 2022</span>
          </button>
        </div>
      </div>

      {/* Alumni Cards Grid */}
      {filtered.length === 0 ? (
        <EmptyState
          icon={<GraduationCap className="size-5" />}
          title="No alumni found"
          description={
            search || fnFilter !== "all"
              ? "No alumni match your current filter criteria."
              : "No alumni entities recorded yet. Run alumni intelligence research tasks or import records from permitted sources."
          }
        />
      ) : (
        <div className="grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-3">
          {filtered.map((a) => (
            <div
              key={a.id}
              className="flex flex-col justify-between rounded-2xl border bg-card p-5 shadow-sm transition-all hover:border-primary/40 hover:shadow-md"
            >
              <div>
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <h3 className="font-semibold text-foreground text-base">{a.name}</h3>
                    <p className="text-xs font-medium text-muted-foreground">{a.title}</p>
                  </div>
                  {a.graduation_year ? (
                    <Badge variant="outline" className="font-mono text-[11px] shrink-0">
                      VIT '{String(a.graduation_year).slice(-2)}
                    </Badge>
                  ) : null}
                </div>

                <div className="mt-3 flex items-center gap-2 text-xs text-foreground/80 font-medium">
                  <Building2 className="size-3.5 text-muted-foreground" />
                  <span>{a.company}</span>
                </div>

                {a.function && (
                  <div className="mt-2.5">
                    <span className="rounded-md border bg-muted/30 px-2 py-0.5 text-[10px] font-mono capitalize text-muted-foreground">
                      {a.function}
                    </span>
                  </div>
                )}
              </div>

              <div className="mt-5 flex items-center justify-between border-t pt-3">
                <span className="text-[11px] text-muted-foreground">
                  {a.graduation_year && a.graduation_year <= 2022 ? (
                    <span className="text-emerald-600 dark:text-emerald-400 font-medium flex items-center gap-1">
                      <CheckCircle2 className="size-3" /> Senior Alumni
                    </span>
                  ) : (
                    <span>Recent Grad</span>
                  )}
                </span>

                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => openOutreachModal(a)}
                  className="h-8 gap-1.5 text-xs font-medium"
                >
                  <Send className="size-3" /> Draft Outreach
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Human-in-the-Loop Outreach Modal */}
      {selectedAlumnus && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
          <div className="w-full max-w-lg rounded-2xl border bg-card p-6 shadow-xl">
            <div className="flex items-start justify-between border-b pb-4">
              <div>
                <h3 className="text-lg font-bold tracking-tight text-foreground flex items-center gap-2">
                  <Sparkles className="size-4 text-primary" /> Propose Outreach Message
                </h3>
                <p className="text-xs text-muted-foreground mt-0.5">
                  Target: <span className="font-semibold text-foreground">{selectedAlumnus.name}</span> ({selectedAlumnus.company})
                </p>
              </div>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => setSelectedAlumnus(null)}
              >
                <X className="size-5" />
              </Button>
            </div>

            <div className="mt-4 space-y-4">
              <div>
                <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                  Outreach Channel
                </label>
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={() => setChannel("linkedin_dm")}
                    className={cn(
                      "flex-1 rounded-lg border py-2 text-xs font-medium text-center transition-colors",
                      channel === "linkedin_dm"
                        ? "border-primary bg-primary/10 text-primary font-semibold"
                        : "border-border text-muted-foreground hover:bg-accent/40",
                    )}
                  >
                    LinkedIn DM
                  </button>
                  <button
                    type="button"
                    onClick={() => setChannel("email")}
                    className={cn(
                      "flex-1 rounded-lg border py-2 text-xs font-medium text-center transition-colors",
                      channel === "email"
                        ? "border-primary bg-primary/10 text-primary font-semibold"
                        : "border-border text-muted-foreground hover:bg-accent/40",
                    )}
                  >
                    Email
                  </button>
                </div>
              </div>

              <div>
                <label className="block text-xs font-medium text-muted-foreground mb-1.5">
                  Draft Content
                </label>
                <textarea
                  rows={6}
                  value={draftText}
                  onChange={(e) => setDraftText(e.target.value)}
                  className="w-full rounded-xl border bg-background p-3 text-sm font-sans focus:outline-none focus:ring-2 focus:ring-primary/40 leading-relaxed"
                />
              </div>

              <div className="rounded-xl border border-amber-500/20 bg-amber-500/10 p-3 text-xs text-amber-700 dark:text-amber-300">
                <span className="font-semibold">Human Approval Gate:</span> Submitting creates a pending approval item in Griffin's approval queue. No message will be dispatched without explicit human sign-off.
              </div>

              <div className="flex items-center justify-end gap-3 pt-2">
                <Button
                  variant="outline"
                  onClick={() => setSelectedAlumnus(null)}
                  disabled={submitting}
                >
                  Cancel
                </Button>
                <Button
                  onClick={handleProposeOutreach}
                  disabled={submitting || !draftText.trim()}
                >
                  {submitting ? "Submitting..." : "Submit for Approval"}
                </Button>
              </div>
            </div>
          </div>
        </div>
      )}
    </PageContainer>
  );
}
