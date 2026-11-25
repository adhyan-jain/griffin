"use client";

import { useCallback, useEffect, useState } from "react";
import { AlertTriangle, Check, CircleCheck, CircleDashed, FileText, Loader2, X } from "lucide-react";
import { toast } from "sonner";
import { api, type ApplicationPackage, type PackageBrief } from "@/lib/api";
import { PACKAGE_STATUS_LABEL, canDecide, deadlineLabel, packageBadgeLabel, packageTone, resumeName } from "@/lib/application-package";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ResponsiveDialog } from "@/components/responsive-dialog";

/** The package status on a listing card. */
export function PackageBadge({ brief }: { brief: PackageBrief }) {
  return (
    <Badge variant={packageTone(brief.status)} data-testid="package-status">
      <FileText />
      {packageBadgeLabel(brief)}
    </Badge>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-1.5">
      <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{title}</h3>
      {children}
    </section>
  );
}

function wordCount(s: string): number {
  return s.trim() ? s.trim().split(/\s+/).length : 0;
}

/**
 * One application package, laid out for a decision: the resume, the projects with the posting words that matched, the
 * cover letter, the answers, the documents, the deadline and what is missing. Approve and Reject appear only when the
 * package is complete and waiting; deciding sends nothing.
 */
export function PackageView({
  pkg,
  busy = false,
  onDecide,
  now,
}: {
  pkg: ApplicationPackage;
  busy?: boolean;
  onDecide?: (approve: boolean) => void;
  now?: Date;
}) {
  const deadline = deadlineLabel(pkg.deadline, now);
  const decidable = canDecide(pkg);
  return (
    <div className="space-y-4 text-sm" data-testid="package-view">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={packageTone(pkg.status)}>{PACKAGE_STATUS_LABEL[pkg.status]}</Badge>
        {deadline && <span className="text-xs text-muted-foreground">Deadline: {deadline}</span>}
        {pkg.model_used && <span className="text-xs text-muted-foreground">Letter written with a model, then fact-checked against your profile and the posting</span>}
      </div>

      {pkg.missing_information.length > 0 && (
        <div role="alert" className="rounded-lg border border-amber-500/40 bg-amber-500/10 p-3">
          <p className="flex items-center gap-1.5 font-medium">
            <AlertTriangle className="size-4" /> Only you can supply this
          </p>
          <ul className="mt-1.5 list-disc space-y-1 pl-5" aria-label="Missing information">
            {pkg.missing_information.map((m) => (
              <li key={m}>{m}</li>
            ))}
          </ul>
          <p className="mt-2 text-xs text-muted-foreground">Add it to your profile, then press Check again. Nothing was invented to fill the gap.</p>
        </div>
      )}

      <Section title="Resume">
        <p className="flex items-center gap-1.5 font-mono text-xs" title={pkg.resume_file}>
          <FileText className="size-3.5 text-muted-foreground" /> {resumeName(pkg.resume_file)}
        </p>
      </Section>

      <Section title="Projects to feature">
        {pkg.selected_projects.length === 0 ? (
          <p className="text-muted-foreground">No project in your profile shares a skill with this posting, so none is featured.</p>
        ) : (
          <ul className="space-y-2">
            {pkg.selected_projects.map((p) => (
              <li key={p.project} className="rounded-lg border bg-muted/30 p-2.5">
                <p className="font-medium">{p.project}</p>
                <div className="mt-1 flex flex-wrap gap-1">
                  {p.skills.map((s) => (
                    <Badge key={s} variant="outline">
                      {s}
                    </Badge>
                  ))}
                </div>
                {p.facts.map((f) => (
                  <p key={f.text} className="mt-1.5 text-muted-foreground">
                    {f.text}
                  </p>
                ))}
                <p className="mt-1.5 text-xs">
                  <span className="text-muted-foreground">The posting says: </span>
                  <q>{p.quote}</q>
                </p>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <Section title={`Cover letter${pkg.cover_letter ? ` (${wordCount(pkg.cover_letter)} words)` : ""}`}>
        {pkg.cover_letter ? (
          <p className="whitespace-pre-wrap rounded-lg border bg-background p-3 leading-relaxed">{pkg.cover_letter}</p>
        ) : (
          <p className="text-muted-foreground">No letter was stored: it could not be written without claims outside your profile and the posting.</p>
        )}
      </Section>

      {pkg.answers.length > 0 && (
        <Section title="Answers">
          <ul className="space-y-2">
            {pkg.answers.map((a) => (
              <li key={a.question} className="rounded-lg border p-2.5">
                <p className="font-medium">{a.question}</p>
                <p className="mt-0.5 whitespace-pre-wrap">{a.answer}</p>
                <p className="mt-1 text-xs text-muted-foreground">From {a.source}</p>
              </li>
            ))}
          </ul>
        </Section>
      )}

      <Section title="Required documents">
        <ul className="space-y-1" aria-label="Required documents">
          {pkg.required_documents.map((d) => (
            <li key={d.name} className="flex items-start gap-2">
              {d.status === "missing" ? <CircleDashed className="mt-0.5 size-4 shrink-0 text-amber-600" /> : <CircleCheck className="mt-0.5 size-4 shrink-0 text-primary" />}
              <span>
                <span className="font-medium">{d.name}</span>
                <span className="text-muted-foreground"> · {d.status === "missing" ? "missing" : d.status}</span>
                {d.note && <span className="block break-all text-xs text-muted-foreground">{d.note}</span>}
              </span>
            </li>
          ))}
        </ul>
      </Section>

      {pkg.evidence.length > 0 && (
        <details className="rounded-lg border p-2.5">
          <summary className="cursor-pointer text-xs font-semibold uppercase tracking-wider text-muted-foreground">Evidence from the posting ({pkg.evidence.length})</summary>
          <ul className="mt-2 space-y-1.5">
            {pkg.evidence.map((e, i) => (
              <li key={i}>
                {e.reason}: <q className="text-muted-foreground">{e.quote}</q>
              </li>
            ))}
          </ul>
        </details>
      )}

      {onDecide && (
        <div className="space-y-2 border-t pt-3">
          {decidable ? (
            <>
              <div className="grid grid-cols-2 gap-2">
                <Button variant="outline" className="h-11" disabled={busy} onClick={() => onDecide(false)}>
                  <X /> Reject
                </Button>
                <Button className="h-11" disabled={busy} onClick={() => onDecide(true)}>
                  {busy ? <Loader2 className="animate-spin" /> : <Check />} Approve package
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">Approving does not send anything. Any submission is a separate task that asks you again before it sends.</p>
            </>
          ) : (
            <p className="text-xs text-muted-foreground">
              {pkg.status === "needs_owner"
                ? "This package cannot be approved until the missing items above are in your profile."
                : pkg.status === "approved"
                  ? "You approved this package. Nothing has been sent."
                  : pkg.status === "rejected"
                    ? "You rejected this package. Prepare it again to get a new one."
                    : "This package is not waiting for a decision."}
            </p>
          )}
        </div>
      )}
    </div>
  );
}

/**
 * A listing's package in a dialog. It opens the package of a listing (by listing id) or by the package's own id (from an
 * approval), and decides it through the approval endpoint the Approvals page uses.
 */
export function PackageDialog({
  open,
  onOpenChange,
  title,
  listingId,
  packageId,
  onChanged,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  listingId?: string;
  packageId?: string;
  onChanged?: () => void;
}) {
  const [pkg, setPkg] = useState<ApplicationPackage | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      setPkg(packageId ? await api.getPackage(packageId) : listingId ? await api.getListingPackage(listingId) : null);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not load the package.");
    }
  }, [listingId, packageId]);

  useEffect(() => {
    if (open) void load();
  }, [open, load]);
  // A package left over from another listing is never shown while this one loads.
  const current = pkg && (packageId ? pkg.id === packageId : pkg.listing_id === listingId) ? pkg : null;

  const decide = async (approve: boolean) => {
    if (!pkg?.approval_id) return;
    setBusy(true);
    try {
      await api.respondApproval(pkg.approval_id, approve);
      toast.success(approve ? "Package approved. Nothing was sent." : "Package rejected");
      await load();
      onChanged?.();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not record your decision.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <ResponsiveDialog open={open} onOpenChange={onOpenChange} title={title} description="Everything prepared for this application. Nothing has been sent." contentClassName="sm:max-w-2xl">
      <div className="max-h-[70dvh] overflow-y-auto pr-1">
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        {!current && !error && <Skeleton className="h-48 w-full rounded-xl" />}
        {current && <PackageView pkg={current} busy={busy} onDecide={decide} />}
      </div>
    </ResponsiveDialog>
  );
}
