// Pure helpers for the Companies page. Property names and claim origins mirror domain.CompanyProperties and
// domain.Origin* in internal/domain/person.go. Each property is its own fact; nothing here combines them.
import type { CompanyEvidence, CompanyObservation } from "@/lib/api";

/** An observation older than this many days is shown with a stale badge. */
export const STALE_AFTER_DAYS = 90;

export const isStale = (ageDays: number) => ageDays > STALE_AFTER_DAYS;

/** The separate fields the card shows, in display order. Never merged into one score. */
export const KEY_PROPERTIES = [
  "funding",
  "revenue",
  "profitability",
  "advertised_pay",
  "employee_reported_pay",
  "internship_stipend",
  "hiring_signal",
] as const;

const PROPERTY_LABELS: Record<string, string> = {
  funding: "Funding",
  revenue: "Revenue",
  profitability: "Profitability",
  advertised_pay: "Advertised pay",
  employee_reported_pay: "Employee-reported pay",
  internship_stipend: "Internship stipend",
  hiring_signal: "Hiring signals",
  product: "Product",
  market: "Market",
};

export function propertyLabel(property: string): string {
  return PROPERTY_LABELS[property] ?? property.replace(/_/g, " ");
}

/** "company-reported" vs "independent"; anything else is plainly unknown. */
export function originLabel(origin: string): string {
  if (origin === "company_reported") return "company-reported";
  if (origin === "independent") return "independent";
  return "origin unknown";
}

export function ageText(ageDays: number): string {
  if (ageDays <= 0) return "today";
  if (ageDays === 1) return "1 day old";
  return `${ageDays} days old`;
}

/** Only http(s) links are rendered as links; anything else is dropped. */
export function safeHref(url: string | null | undefined): string | null {
  if (!url) return null;
  try {
    const u = new URL(url);
    return u.protocol === "http:" || u.protocol === "https:" ? u.toString() : null;
  } catch {
    return null;
  }
}

export interface ObservationView {
  property: string;
  label: string;
  value: string;
  origin: string;
  age: string;
  stale: boolean;
  sourceUrl: string | null;
}

export function observationView(o: CompanyObservation): ObservationView {
  return {
    property: o.property,
    label: propertyLabel(o.property),
    value: o.value,
    origin: originLabel(o.claim_origin),
    age: ageText(o.age_days),
    stale: isStale(o.age_days),
    sourceUrl: safeHref(o.source_url),
  };
}

export interface KeyField {
  property: string;
  label: string;
  /** null means there is no data: the page shows "unknown". */
  observation: ObservationView | null;
}

/** One entry per key property, each from its own newest observation. */
export function keyFields(latest: CompanyObservation[] | null | undefined): KeyField[] {
  const rows = latest ?? [];
  return KEY_PROPERTIES.map((property) => {
    const newest = rows
      .filter((o) => o.property === property)
      .reduce<CompanyObservation | null>((best, o) => (best === null || o.age_days < best.age_days ? o : best), null);
    return { property, label: propertyLabel(property), observation: newest ? observationView(newest) : null };
  });
}

/** Full history, newest first. */
export function observationHistory(obs: CompanyObservation[] | null | undefined): ObservationView[] {
  return [...(obs ?? [])]
    .sort((a, b) => new Date(b.observed_at).getTime() - new Date(a.observed_at).getTime())
    .map(observationView);
}

export function listingCountText(n: number): string {
  return n === 1 ? "1 open listing" : `${n} open listings`;
}

export function evidenceList(ev: CompanyEvidence[] | null | undefined): CompanyEvidence[] {
  return ev ?? [];
}
