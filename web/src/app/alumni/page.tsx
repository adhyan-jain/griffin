"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import {
  Building2,
  CheckCircle2,
  Download,
  GraduationCap,
  Loader2,
  Search,
  Send,
  Sparkles,
  ExternalLink,
  Copy,
  Check,
  Award,
  ShieldCheck,
  FileText,
} from "lucide-react";
import { toast } from "sonner";
import { api, type AlumniCard, type OutreachChannel, type DraftProposal } from "@/lib/api";
import {
  APPROVAL_NOTICE,
  EMPTY_FILTERS,
  MAX_DRAFT_CHARS,
  OUTREACH_CHANNELS,
  OWNER_STATES,
  contactStateLabel,
  draftLength,
  draftProblem,
  draftTemplate,
  gradYearView,
  importSummary,
  outreachErrorMessage,
  outreachGate,
  priorityTierBadge,
  gradEligibilityBadge,
  replacePerson,
  roleFamilyText,
  toQuery,
  type FilterState,
} from "@/lib/alumni";
import { safeHref } from "@/lib/companies";
import { cn } from "@/lib/utils";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { AlumniVerifyDialog } from "@/components/alumni-verify-dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState, ErrorNote, PageContainer, PageHeader } from "@/components/page";

const ROLE_OPTIONS: { id: FilterState["roleFamily"]; label: string }[] = [
  { id: "all", label: "All Roles" },
  { id: "tech", label: "Tech" },
  { id: "operations", label: "Operations" },
];

const CAMPUS_OPTIONS: { id: FilterState["campus"]; label: string }[] = [
  { id: "all", label: "All Campuses" },
  { id: "vellore", label: "Vellore Campus" },
  { id: "other", label: "Other Campuses" },
];

const ACM_OPTIONS: { id: FilterState["acm"]; label: string }[] = [
  { id: "all", label: "All Clubs" },
  { id: "confirmed", label: "Confirmed ACM" },
  { id: "likely", label: "Likely ACM" },
];

const PRIORITY_OPTIONS: { id: FilterState["priority"]; label: string }[] = [
  { id: "all", label: "All Tiers" },
  { id: "P1", label: "P1 (ACM + High Pot.)" },
  { id: "P2", label: "P2 (ACM)" },
  { id: "P3", label: "P3 (Vellore High Pot.)" },
  { id: "P4", label: "P4 (Vellore)" },
  { id: "REVIEW", label: "Review" },
];

const GRADUATION_OPTIONS: { id: FilterState["graduation"]; label: string }[] = [
  { id: "all", label: "Any Cohort" },
  { id: "eligible", label: "Eligible" },
  { id: "borderline_2022", label: "Borderline 2022" },
  { id: "ineligible", label: "Ineligible" },
  { id: "unknown", label: "Unknown" },
];

const FUNCTION_OPTIONS: { id: FilterState["fn"]; label: string }[] = [
  { id: "all", label: "All Functions" },
  { id: "technology", label: "Technology" },
  { id: "operations", label: "Operations" },
  { id: "other", label: "Other" },
];

const CONTACTABILITY_OPTIONS: { id: FilterState["contactability"]; label: string }[] = [
  { id: "all", label: "Any Contact" },
  { id: "profile_only", label: "Profile only" },
  { id: "unknown", label: "Unknown" },
];

const DNC_OPTIONS: { id: FilterState["dnc"]; label: string }[] = [
  { id: "any", label: "Everyone" },
  { id: "hide", label: "Hide DNC" },
  { id: "only", label: "Only DNC" },
];

function Pills<T extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: T;
  options: { id: T; label: string }[];
  onChange: (v: T) => void;
  label: string;
}) {
  return (
    <div role="group" aria-label={label} className="flex flex-wrap rounded-lg border bg-card p-1">
      {options.map((o) => (
        <button
          key={o.id}
          type="button"
          aria-pressed={value === o.id}
          onClick={() => onChange(o.id)}
          className={cn(
            "rounded-md px-2.5 py-1 text-xs font-medium transition-colors",
            value === o.id ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:text-foreground",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** The owner's recorded campus evidence, shown as a tooltip on the campus badge. */
function campusSource(a: AlumniCard): string | undefined {
  const c = (a.metadata?.verification as { campus?: { source_url?: string; excerpt?: string } } | undefined)?.campus;
  return c ? `${c.excerpt ?? ""} (${c.source_url ?? ""})` : undefined;
}

export default function AlumniExplorerPage() {
  const [alumni, setAlumni] = useState<AlumniCard[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filters, setFilters] = useState<FilterState>(EMPTY_FILTERS);
  const [reloadTick, setReloadTick] = useState(0);
  const [busyIds, setBusyIds] = useState<Set<string>>(new Set());
  const [importing, setImporting] = useState(false);
  const [importNote, setImportNote] = useState<string | null>(null);
  const [verifying, setVerifying] = useState<AlumniCard | null>(null);

  // Manual Outreach Modal State
  const [target, setTarget] = useState<AlumniCard | null>(null);
  const [draftText, setDraftText] = useState("");
  const [channel, setChannel] = useState<OutreachChannel>("linkedin");
  const [submitting, setSubmitting] = useState(false);
  const [saved, setSaved] = useState(false);
  const [draftError, setDraftError] = useState<string | null>(null);

  // Dual-Variant Draft Preview Modal State
  const [previewTarget, setPreviewTarget] = useState<AlumniCard | null>(null);
  const [generatedDraft, setGeneratedDraft] = useState<DraftProposal | null>(null);
  const [draftLoading, setDraftLoading] = useState(false);
  const [copiedType, setCopiedType] = useState<"short" | "warm" | null>(null);

  // Debounce filters
  const [debounced, setDebounced] = useState<FilterState>(EMPTY_FILTERS);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(filters), 300);
    return () => clearTimeout(t);
  }, [filters]);

  const latestReq = useRef(0);
  useEffect(() => {
    const token = ++latestReq.current;
    const ctrl = new AbortController();
    setLoading(true);
    api
      .listAlumni(toQuery(debounced), { signal: ctrl.signal })
      .then((list) => {
        if (latestReq.current !== token) return;
        setAlumni(list);
        setError(null);
      })
      .catch((e) => {
        if (ctrl.signal.aborted || latestReq.current !== token) return;
        setError(e instanceof Error ? e.message : "Failed to load alumni");
      })
      .finally(() => {
        if (latestReq.current === token) setLoading(false);
      });
    return () => ctrl.abort();
  }, [debounced, reloadTick]);

  const reload = useCallback(() => setReloadTick((n) => n + 1), []);
  const patch = (key: keyof FilterState) => (v: string) => setFilters((f) => ({ ...f, [key]: v }));

  async function update(p: AlumniCard, body: Parameters<typeof api.updateAlumnus>[1]) {
    setBusyIds((s) => new Set(s).add(p.id));
    try {
      const updated = await api.updateAlumnus(p.id, body);
      setAlumni((list) => replacePerson(list, updated));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not update this person");
    } finally {
      setBusyIds((s) => {
        const n = new Set(s);
        n.delete(p.id);
        return n;
      });
    }
  }

  async function handleImport() {
    setImporting(true);
    setImportNote(null);
    try {
      const res = await api.importAlumni();
      setImportNote(importSummary(res));
      reload();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Import failed");
    } finally {
      setImporting(false);
    }
  }

  function openOutreach(p: AlumniCard) {
    setTarget(p);
    setDraftText(draftTemplate(p));
    setChannel("linkedin");
    setSaved(false);
    setDraftError(null);
  }

  async function openGeneratedDraft(p: AlumniCard) {
    setPreviewTarget(p);
    setDraftLoading(true);
    setCopiedType(null);
    try {
      const draft = await api.getAlumniDraft(p.id);
      setGeneratedDraft(draft);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not generate personalized draft");
      setPreviewTarget(null);
    } finally {
      setDraftLoading(false);
    }
  }

  function copyText(text: string, type: "short" | "warm") {
    navigator.clipboard.writeText(text);
    setCopiedType(type);
    toast.success("Draft copied to clipboard");
    setTimeout(() => setCopiedType(null), 2000);
  }

  async function submitOutreach() {
    if (!target || draftProblem(draftText)) return;
    setSubmitting(true);
    setDraftError(null);
    try {
      const res = await api.proposeAlumniOutreach(target.id, { draft_text: draftText.trim(), channel });
      setAlumni((list) => replacePerson(list, res.person));
      setSaved(true);
    } catch (e) {
      setDraftError(outreachErrorMessage(e));
      if ((e as { status?: number } | null)?.status === 409) reload();
    } finally {
      setSubmitting(false);
    }
  }

  const problem = draftProblem(draftText);
  const count = draftLength(draftText);
  const targetGate = useMemo(() => (target ? outreachGate(target) : null), [target]);

  return (
    <PageContainer className="max-w-7xl">
      <PageHeader
        title="VIT Alumni Intelligence & Prospecting"
        description="Alumni from your tracker and research, classified from stored evidence. Unconfirmed campus or graduation stays in REVIEW. Griffin never sends automated messages: drafts are copy-ready for manual review."
      />

      {error && (
        <div className="mb-6">
          <ErrorNote message={error} />
        </div>
      )}

      {/* Filter Toolbar */}
      <div className="mb-6 flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-48 flex-1 sm:max-w-xs">
            <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              aria-label="Search"
              placeholder="Search name, title, or company..."
              value={filters.search}
              onChange={(e) => patch("search")(e.target.value)}
              className="pl-9"
            />
          </div>
          <Pills label="Campus" value={filters.campus} options={CAMPUS_OPTIONS} onChange={(v) => setFilters((f) => ({ ...f, campus: v }))} />
          <Pills label="ACM Affiliation" value={filters.acm} options={ACM_OPTIONS} onChange={(v) => setFilters((f) => ({ ...f, acm: v }))} />
          <Pills label="Priority Tier" value={filters.priority} options={PRIORITY_OPTIONS} onChange={(v) => setFilters((f) => ({ ...f, priority: v }))} />
          <Pills label="Graduation Eligibility" value={filters.graduation} options={GRADUATION_OPTIONS} onChange={(v) => setFilters((f) => ({ ...f, graduation: v }))} />
          <Pills label="Professional Function" value={filters.fn} options={FUNCTION_OPTIONS} onChange={(v) => setFilters((f) => ({ ...f, fn: v }))} />
          <Pills label="Contactability" value={filters.contactability} options={CONTACTABILITY_OPTIONS} onChange={(v) => setFilters((f) => ({ ...f, contactability: v }))} />
          <Input
            aria-label="Minimum classification confidence"
            type="number"
            min={0}
            max={100}
            placeholder="Min confidence"
            value={filters.minConfidence}
            onChange={(e) => patch("minConfidence")(e.target.value)}
            className="w-36"
          />
          <Pills label="Role Family" value={filters.roleFamily} options={ROLE_OPTIONS} onChange={(v) => setFilters((f) => ({ ...f, roleFamily: v }))} />
          <Pills label="Do Not Contact" value={filters.dnc} options={DNC_OPTIONS} onChange={(v) => setFilters((f) => ({ ...f, dnc: v }))} />
          <Button variant="outline" onClick={handleImport} disabled={importing} className="ml-auto gap-1.5">
            {importing ? <Loader2 className="size-4 animate-spin" /> : <Download className="size-4" />}
            Import from tracker
          </Button>
        </div>
        <div className="flex items-center justify-between text-xs text-muted-foreground" aria-live="polite">
          <span>{importNote ?? ""}</span>
          <span className="font-medium">{loading ? "Loading..." : `${alumni.length} alumni prospects found`}</span>
        </div>
      </div>

      {/* Alumni Cards Grid */}
      {loading && alumni.length === 0 && !error ? (
        <div className="grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-3" aria-busy="true">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-64 rounded-2xl" />
          ))}
        </div>
      ) : alumni.length === 0 ? (
        <EmptyState
          icon={<GraduationCap className="size-5" />}
          title="No alumni found"
          description="Nobody matches these filters. Try broadening your criteria or reset filters."
        />
      ) : (
        <div className={cn("grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-3", loading && "opacity-60")}>
          {alumni.map((a) => {
            const year = gradYearView(a);
            const gate = outreachGate(a);
            const busy = busyIds.has(a.id);
            const profile = safeHref(a.profile_url);
            const tierBadge = priorityTierBadge(a.priority_tier);
            const eligBadge = gradEligibilityBadge(a.graduation_eligibility);
            const isACM = a.acm_vit_affiliation === "confirmed";
            const isLikelyACM = a.acm_vit_affiliation === "likely";

            return (
              <div key={a.id} className="flex flex-col justify-between rounded-2xl border bg-card p-5 shadow-sm hover:border-primary/30 transition-colors">
                <div>
                  {/* Header: Name, Title & Tier Badge */}
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="flex items-center gap-1.5 flex-wrap">
                        <h3 className="truncate text-base font-semibold text-foreground">{a.name}</h3>
                        {isACM && (
                          <Badge variant="outline" className="border-sky-500/30 bg-sky-500/10 text-sky-400 text-[10px] gap-1 py-0">
                            <Award className="size-3" /> ACM-VIT
                          </Badge>
                        )}
                        {isLikelyACM && (
                          <Badge variant="outline" className="text-muted-foreground text-[10px] gap-1 py-0">
                            <Award className="size-3" /> ACM-VIT (likely)
                          </Badge>
                        )}
                      </div>
                      <p className="text-xs font-medium text-muted-foreground truncate">{a.title || "Title unknown"}</p>
                    </div>
                    {a.do_not_contact ? (
                      <Badge className="shrink-0 bg-destructive/15 text-destructive text-[10px]">DNC</Badge>
                    ) : (
                      <Badge variant="outline" className={cn("shrink-0 text-[10px] font-medium border", tierBadge.color)}>
                        {tierBadge.label.split("·")[0].trim()}
                      </Badge>
                    )}
                  </div>

                  {/* Company & Priority Details */}
                  <div className="mt-3 flex items-center justify-between text-xs text-foreground/80">
                    <div className="flex items-center gap-1.5 font-medium truncate">
                      <Building2 className="size-3.5 text-muted-foreground shrink-0" />
                      <span className="truncate">{a.company || "Company unknown"}</span>
                    </div>
                    {a.seniority && (
                      <span className="text-[10px] uppercase font-semibold text-muted-foreground bg-muted/40 px-1.5 py-0.5 rounded">
                        {a.seniority}
                      </span>
                    )}
                  </div>

                  {/* Cohort & Qualification Attributes */}
                  <dl className="mt-3 space-y-1.5 text-xs">
                    <div className="flex flex-wrap items-center gap-x-2">
                      <dt className="text-muted-foreground">Campus & Batch:</dt>
                      <dd className="font-medium flex items-center gap-1">
                        <span className="capitalize">{a.campus || "unknown"}</span> · {year.text}
                      </dd>
                      {a.vit_affiliation && a.vit_affiliation !== "unconfirmed" && (
                        <Badge variant="outline" className="text-[9px] py-0 px-1" title={campusSource(a)}>
                          {a.vit_affiliation.replace("_", " ")}
                        </Badge>
                      )}
                      <Badge variant="outline" className={cn("text-[9px] py-0 px-1 border", eligBadge.color)}>
                        {eligBadge.label}
                      </Badge>
                    </div>

                    <div className="flex items-center gap-2">
                      <dt className="text-muted-foreground">Role Family:</dt>
                      <dd className="font-medium">{roleFamilyText(a)}</dd>
                    </div>

                    {a.acm_role && (
                      <div className="flex items-start gap-1.5 text-sky-600 dark:text-sky-400 bg-sky-500/5 p-1.5 rounded border border-sky-500/10 text-[11px]">
                        <ShieldCheck className="size-3.5 shrink-0 mt-0.5" />
                        <span>
                          ACM {a.acm_vit_affiliation}: {a.acm_role} {a.acm_tenure ? `(${a.acm_tenure})` : ""}
                          {a.acm_source && safeHref(a.acm_source) && (
                            <a href={safeHref(a.acm_source)!} target="_blank" rel="noopener noreferrer" title={a.acm_evidence_excerpt} className="ml-1 underline">
                              source
                            </a>
                          )}
                        </span>
                      </div>
                    )}
                  </dl>

                  <Button variant="outline" size="sm" className="mt-2 h-7 text-[11px]" onClick={() => setVerifying(a)}>
                    Record evidence
                  </Button>

                  {/* Profile link */}
                  {profile && (
                    <a
                      href={profile}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="mt-2.5 inline-flex items-center gap-1 text-xs text-primary hover:underline font-medium"
                    >
                      View LinkedIn Profile <ExternalLink className="size-3" />
                    </a>
                  )}
                </div>

                {/* Footer Actions */}
                <div className="mt-4 space-y-3 border-t pt-3">
                  <div className="flex flex-wrap items-center gap-1" role="group" aria-label={`Contact state for ${a.name}`}>
                    {OWNER_STATES.map((s) => (
                      <button
                        key={s}
                        type="button"
                        disabled={busy || a.contact_state === s}
                        aria-pressed={a.contact_state === s}
                        onClick={() => update(a, { contact_state: s })}
                        className={cn(
                          "rounded-md border px-2 py-0.5 text-[10px] font-medium transition-colors disabled:cursor-default",
                          a.contact_state === s ? "border-primary bg-primary/10 text-primary" : "text-muted-foreground hover:bg-accent/40",
                        )}
                      >
                        {contactStateLabel(s)}
                      </button>
                    ))}
                    {(a.contact_state === "drafted" || a.contact_state === "approved") && (
                      <Badge variant="outline" className="text-[10px]">{contactStateLabel(a.contact_state)}</Badge>
                    )}
                  </div>

                  <div className="flex items-center justify-between gap-2">
                    <label className="flex items-center gap-2 text-xs text-muted-foreground">
                      <Switch
                        checked={a.do_not_contact}
                        disabled={busy}
                        onCheckedChange={(v) => update(a, { do_not_contact: v })}
                        aria-label={`Do not contact ${a.name}`}
                      />
                      DNC
                    </label>

                    <div className="flex items-center gap-1.5">
                      <Button
                        size="sm"
                        variant="secondary"
                        disabled={gate.disabled}
                        onClick={() => openGeneratedDraft(a)}
                        className="h-7 text-[11px] gap-1 px-2.5"
                      >
                        <Sparkles className="size-3 text-primary" /> Personalized Draft
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={gate.disabled}
                        title={gate.reason ?? undefined}
                        onClick={() => openOutreach(a)}
                        className="h-7 text-[11px] gap-1 px-2.5"
                      >
                        <Send className="size-3" /> Manual
                      </Button>
                    </div>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Generated Dual Draft Modal */}
      <Dialog open={previewTarget !== null} onOpenChange={(open) => !open && setPreviewTarget(null)}>
        <DialogContent className="sm:max-w-xl max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-base">
              <Sparkles className="size-4 text-primary" /> Personalized Outreach Drafts
            </DialogTitle>
            <DialogDescription>
              {previewTarget?.name} · {previewTarget?.title} at {previewTarget?.company}
            </DialogDescription>
          </DialogHeader>

          {draftLoading ? (
            <div className="flex items-center justify-center p-8">
              <Loader2 className="size-6 animate-spin text-primary" />
            </div>
          ) : generatedDraft ? (
            <div className="space-y-4 text-xs">
              {/* Selection Rationale */}
              <div className="rounded-lg bg-muted/40 p-3 border border-border">
                <span className="font-semibold text-foreground">Selection Reason: </span>
                <span className="text-muted-foreground">{generatedDraft.selection_reason}</span>
              </div>

              {/* Verified Facts */}
              {generatedDraft.verified_facts.length > 0 && (
                <div className="rounded-lg bg-emerald-500/5 p-3 border border-emerald-500/15">
                  <span className="font-semibold text-emerald-400 block mb-1">Verified Facts:</span>
                  <ul className="list-disc pl-4 space-y-0.5 text-muted-foreground text-[11px]">
                    {generatedDraft.verified_facts.map((fact, idx) => (
                      <li key={idx}>{fact}</li>
                    ))}
                  </ul>
                </div>
              )}

              {/* Variant A: Short LinkedIn Connection Note */}
              <div className="rounded-xl border border-border p-3.5 space-y-2 bg-card">
                <div className="flex items-center justify-between">
                  <span className="font-semibold text-foreground flex items-center gap-1.5">
                    <FileText className="size-3.5 text-sky-400" /> Variant A: Short LinkedIn Note
                  </span>
                  <Badge variant="outline" className="text-[10px]">
                    {generatedDraft.short_note.length} / 300 chars
                  </Badge>
                </div>
                <div className="rounded bg-muted/30 p-2.5 font-mono text-[11px] leading-relaxed whitespace-pre-wrap">
                  {generatedDraft.short_note}
                </div>
                <div className="flex justify-end">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => copyText(generatedDraft.short_note, "short")}
                    className="h-7 text-xs gap-1.5"
                  >
                    {copiedType === "short" ? <Check className="size-3 text-emerald-500" /> : <Copy className="size-3" />}
                    {copiedType === "short" ? "Copied Note" : "Copy Note"}
                  </Button>
                </div>
              </div>

              {/* Variant B: Warmer InMail / Email Draft */}
              <div className="rounded-xl border border-border p-3.5 space-y-2 bg-card">
                <div className="flex items-center justify-between">
                  <span className="font-semibold text-foreground flex items-center gap-1.5">
                    <FileText className="size-3.5 text-emerald-400" /> Variant B: Warmer InMail / Email Draft
                  </span>
                </div>
                <div className="rounded bg-muted/30 p-2.5 font-mono text-[11px] leading-relaxed whitespace-pre-wrap">
                  {generatedDraft.warm_draft}
                </div>
                <div className="flex justify-end">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => copyText(generatedDraft.warm_draft, "warm")}
                    className="h-7 text-xs gap-1.5"
                  >
                    {copiedType === "warm" ? <Check className="size-3 text-emerald-500" /> : <Copy className="size-3" />}
                    {copiedType === "warm" ? "Copied Warm Draft" : "Copy Warm Draft"}
                  </Button>
                </div>
              </div>

              {/* Safe Dispatch Notice */}
              <p className="rounded-lg border border-amber-500/20 bg-amber-500/10 p-2.5 text-[11px] text-amber-700 dark:text-amber-300">
                Griffin operates strictly human-in-the-loop. Copy these pre-verified drafts to send manually from your professional profile.
              </p>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>

      <AlumniVerifyDialog
        key={verifying?.id ?? "none"}
        person={verifying}
        onClose={() => setVerifying(null)}
        onSaved={(p) => setAlumni((list) => replacePerson(list, p))}
      />

      {/* Manual Draft / Approval Dialog */}
      <Dialog open={target !== null} onOpenChange={(open) => !open && setTarget(null)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Sparkles className="size-4 text-primary" /> Draft outreach
            </DialogTitle>
            <DialogDescription>
              {target?.name} ({target?.company || "company unknown"})
            </DialogDescription>
          </DialogHeader>

          {saved ? (
            <div className="space-y-4">
              <div role="status" className="rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-3 text-sm text-emerald-700 dark:text-emerald-300">
                {APPROVAL_NOTICE}
              </div>
              <div className="flex justify-end gap-2">
                <Button variant="outline" onClick={() => setTarget(null)}>Close</Button>
                <Button asChild>
                  <Link href="/approvals">Open approvals</Link>
                </Button>
              </div>
            </div>
          ) : (
            <div className="space-y-4">
              {targetGate?.disabled && (
                <ErrorNote message="This person is marked do not contact. Outreach is off." />
              )}
              <div role="group" aria-label="Channel" className="flex gap-2">
                {OUTREACH_CHANNELS.map((c) => (
                  <button
                    key={c.id}
                    type="button"
                    aria-pressed={channel === c.id}
                    onClick={() => setChannel(c.id)}
                    className={cn(
                      "flex-1 rounded-lg border py-2 text-center text-xs font-medium transition-colors",
                      channel === c.id ? "border-primary bg-primary/10 font-semibold text-primary" : "border-border text-muted-foreground hover:bg-accent/40",
                    )}
                  >
                    {c.label}
                  </button>
                ))}
              </div>
              <div>
                <Textarea
                  aria-label="Outreach draft"
                  rows={8}
                  value={draftText}
                  onChange={(e) => setDraftText(e.target.value)}
                  aria-invalid={problem === "too_long"}
                />
                <p className={cn("mt-1 text-right text-[11px]", problem === "too_long" ? "text-destructive" : "text-muted-foreground")}>
                  {count}/{MAX_DRAFT_CHARS}
                </p>
              </div>
              {draftError && <ErrorNote message={draftError} />}
              <p className="rounded-xl border border-amber-500/20 bg-amber-500/10 p-3 text-xs text-amber-700 dark:text-amber-300">
                Fill in the bracketed parts. Submitting saves this for your approval. Griffin never sends it; you send it yourself.
              </p>
              <div className="flex justify-end gap-2">
                <Button variant="outline" onClick={() => setTarget(null)} disabled={submitting}>Cancel</Button>
                <Button onClick={submitOutreach} disabled={submitting || problem !== null || !!targetGate?.disabled}>
                  {submitting ? "Saving..." : "Save for approval"}
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </PageContainer>
  );
}
