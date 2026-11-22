import type { Eligibility, Listing, ListingAssessment, ValueComponent } from "@/lib/api";

/** The tabs of the Internships page, best first. A listing is in exactly one. */
export const BUCKETS = [
  { id: "exceptional", label: "Exceptional" },
  { id: "strong", label: "Strong" },
  { id: "good", label: "Good" },
  { id: "exploratory", label: "Exploratory" },
  { id: "low", label: "Low" },
  { id: "review", label: "Needs review" },
  { id: "ineligible", label: "Not eligible" },
] as const;
export type BucketId = (typeof BUCKETS)[number]["id"];

/**
 * The tab a listing belongs to. A listing whose eligibility is unknown, or that has not been assessed yet, is in
 * Needs review whatever it scores: it is waiting for a person, never discarded. A listing the owner may not apply to
 * is in Not eligible, with the quoted reason on its card.
 */
export function bucketOf(l: Listing): BucketId {
  const a = l.assessment;
  if (!a || a.eligibility === "unknown") return "review";
  if (a.eligibility === "ineligible") return "ineligible";
  return a.value_class;
}

/** Best value first; listings not assessed yet last, newest first. */
export function byValue(a: Listing, b: Listing): number {
  const sa = a.assessment?.value_score ?? -1;
  const sb = b.assessment?.value_score ?? -1;
  return sb - sa || b.created_at.localeCompare(a.created_at);
}

export const COMPONENT_LABEL: Record<string, string> = {
  depth: "Engineering depth",
  employer: "Employer",
  compensation: "Pay",
  relevance: "Fit with your skills",
  mentorship: "Mentorship",
  ppo: "Full-time offer",
  feasibility: "Feasibility",
  cap: "Held back",
};

export interface Why {
  label: string;
  reason: string;
  points: number;
  max: number;
}

/**
 * The reasons shown on a card: the class cap when there is one, then the components the listing really informed that
 * earned the largest share of their maximum. Unknown components are never offered as a reason.
 */
export function whyLines(a: ListingAssessment, limit = 3): Why[] {
  const parts = a.value_breakdown ?? [];
  const lines: Why[] = [];
  const cap = parts.find((c) => c.name === "cap" && !c.unknown);
  if (cap) lines.push({ label: COMPONENT_LABEL.cap, reason: cap.reason, points: 0, max: 0 });
  const known = parts
    .filter((c: ValueComponent) => c.name !== "cap" && c.max > 0 && !c.unknown)
    .sort((x, y) => y.points / y.max - x.points / x.max);
  for (const c of known) {
    if (lines.length >= limit) break;
    lines.push({ label: COMPONENT_LABEL[c.name] ?? c.name, reason: c.reason, points: c.points, max: c.max });
  }
  return lines;
}

/** How many of the 100 points could not be read from the listing (scored at the midpoint of their range). */
export function unknownWeight(a: ListingAssessment): number {
  return (a.value_breakdown ?? []).filter((c) => c.name !== "cap" && c.unknown).reduce((n, c) => n + c.max, 0);
}

export const ELIGIBILITY_LABEL: Record<Eligibility, string> = {
  confirmed: "Eligible",
  likely: "Likely eligible",
  unknown: "Eligibility unknown",
  ineligible: "Not eligible",
};

export function eligibilityTone(e: Eligibility): "default" | "secondary" | "outline" | "destructive" {
  return e === "confirmed" ? "default" : e === "likely" ? "secondary" : e === "ineligible" ? "destructive" : "outline";
}

export const ROLE_LABEL: Record<string, string> = {
  backend: "Backend",
  frontend: "Frontend",
  fullstack: "Full-stack",
  ai_ml: "AI / ML",
  research: "Research",
  infra: "Infra",
  devtools: "Devtools",
  data: "Data",
  mobile: "Mobile",
  security: "Security",
  other_tech: "Software",
  non_tech: "Not engineering",
};

/** Pay as the listing states it: "₹60,000 / month", "Unpaid", or null when it says nothing. */
export function statedPay(a: ListingAssessment | null | undefined): string | null {
  const v = a?.compensation_monthly_inr;
  if (v === null || v === undefined) return null;
  if (v === 0) return "Unpaid";
  return `₹${Math.round(v).toLocaleString("en-IN")} / month`;
}

/** Employer-verified postings come from the employer's own board or site; everything else is a lead. */
export const isEmployerVerified = (l: Listing) => l.verification === "employer_verified";
