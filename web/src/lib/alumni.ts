// Pure helpers for the Alumni page. Contact states, channels and limits mirror internal/domain/person.go.
import type { AlumniCard, AlumniFilters, AlumniImportResult, ContactState, OutreachChannel, RoleFamily } from "@/lib/api";

export const MAX_DRAFT_CHARS = 2000;

export const OUTREACH_CHANNELS: { id: OutreachChannel; label: string }[] = [
  { id: "linkedin", label: "LinkedIn" },
  { id: "email", label: "Email" },
  { id: "other", label: "Other" },
];

/** States the owner may set by hand (drafted and approved come only from the outreach flow). */
export const OWNER_STATES: ContactState[] = ["not_contacted", "sent_manually", "replied", "opted_out"];

const STATE_LABELS: Record<ContactState, string> = {
  not_contacted: "Not contacted",
  drafted: "Draft saved",
  approved: "Draft approved",
  sent_manually: "Sent by me",
  replied: "Replied",
  opted_out: "Opted out",
};

export const contactStateLabel = (s: string): string => STATE_LABELS[s as ContactState] ?? s.replace(/_/g, " ");

export interface GradYearView {
  /** "2021" or "Unknown". */
  text: string;
  /** Caveat shown next to the year, when there is one. */
  note: string | null;
  known: boolean;
}

/** Graduation year wording. A year from the tracker's batch cell is a headline guess, not a verified fact. */
export function gradYearView(p: Pick<AlumniCard, "graduation_year" | "graduation_year_source">): GradYearView {
  if (p.graduation_year == null || p.graduation_year <= 0) return { text: "Unknown", note: null, known: false };
  const note = p.graduation_year_source === "tracker_batch" ? "from LinkedIn headline, unverified" : null;
  return { text: String(p.graduation_year), note, known: true };
}

const ROLE_LABELS: Record<string, string> = { tech: "Tech", operations: "Operations" };

/** "Tech (inferred)" when the family was guessed from the title; "Unknown" when there is none. */
export function roleFamilyText(p: Pick<AlumniCard, "role_family" | "role_family_inferred">): string {
  if (!p.role_family) return "Unknown";
  const label = ROLE_LABELS[p.role_family] ?? p.role_family;
  return p.role_family_inferred ? `${label} (inferred)` : label;
}

export interface OutreachGate {
  disabled: boolean;
  reason: string | null;
}

/** Outreach is off for a do-not-contact or opted-out person; the server refuses it with 409 as well. */
export function outreachGate(p: Pick<AlumniCard, "do_not_contact" | "contact_state">): OutreachGate {
  if (p.do_not_contact || p.contact_state === "opted_out") {
    return { disabled: true, reason: "Marked do not contact" };
  }
  return { disabled: false, reason: null };
}

/** Length as the server counts it: characters (runes), after trimming. */
export const draftLength = (text: string): number => Array.from(text.trim()).length;

export type DraftProblem = "empty" | "too_long" | null;

export function draftProblem(text: string): DraftProblem {
  const n = draftLength(text);
  if (n === 0) return "empty";
  return n > MAX_DRAFT_CHARS ? "too_long" : null;
}

export interface OwnerInfo {
  name?: string | null;
  college?: string | null;
}

export const NAME_PLACEHOLDER = "[your name]";
export const COLLEGE_PLACEHOLDER = "[your college]";

/** A starting draft. The owner's name and college come from the profile when it has them; otherwise placeholders. */
export function draftTemplate(p: Pick<AlumniCard, "name" | "company" | "title">, owner: OwnerInfo = {}): string {
  const name = owner.name?.trim() || NAME_PLACEHOLDER;
  const college = owner.college?.trim() || COLLEGE_PLACEHOLDER;
  const at = p.company ? ` at ${p.company}` : "";
  return `Hi ${p.name},\n\nI am a student at ${college} and came across your work${at}. I would love to hear how you got started and what your team works on.\n\nBest,\n${name}`;
}

export interface FilterState {
  /** Cutoff year typed by the owner; empty means no cutoff. */
  gradYearMax: string;
  roleFamily: "all" | RoleFamily;
  campus: "all" | "vellore" | "other";
  acm: "all" | "confirmed" | "likely";
  priority: "all" | "P1" | "P2" | "P3" | "P4" | "REVIEW";
  graduation: "all" | "eligible" | "borderline_2022" | "ineligible" | "unknown";
  fn: "all" | "technology" | "operations" | "other";
  contactability: "all" | "profile_only" | "unknown";
  /** Minimum classification confidence typed by the owner (0 to 100); empty means none. */
  minConfidence: string;
  search: string;
  company: string;
  country: string;
  dnc: "any" | "only" | "hide";
}

export const EMPTY_FILTERS: FilterState = {
  gradYearMax: "",
  roleFamily: "all",
  campus: "all",
  acm: "all",
  priority: "all",
  graduation: "all",
  fn: "all",
  contactability: "all",
  minConfidence: "",
  search: "",
  company: "",
  country: "",
  dnc: "any",
};

/** The query params the server reads for the filters added with the stored classification columns. */
export interface AlumniClassificationQuery {
  graduation_eligibility?: string;
  professional_function?: string;
  contactability_status?: string;
  min_classification_confidence?: number;
}

/** Maps the page's filter state onto the server's query params. An out-of-range year is left out. */
export function toQuery(f: Partial<FilterState>): AlumniFilters & AlumniClassificationQuery {
  const q: AlumniFilters & AlumniClassificationQuery = { limit: 200 };
  if (f.gradYearMax) {
    const y = Number.parseInt(f.gradYearMax, 10);
    if (Number.isInteger(y) && y >= 1950 && y <= 2100) q.grad_year_max = y;
  }
  if (f.roleFamily && f.roleFamily !== "all") q.role_family = f.roleFamily;
  if (f.campus && f.campus !== "all") q.campus = f.campus;
  if (f.acm && f.acm !== "all") q.acm_vit_affiliation = f.acm;
  if (f.priority && f.priority !== "all") q.priority_tier = f.priority;
  if (f.graduation && f.graduation !== "all") q.graduation_eligibility = f.graduation;
  if (f.fn && f.fn !== "all") q.professional_function = f.fn;
  if (f.contactability && f.contactability !== "all") q.contactability_status = f.contactability;
  if (f.minConfidence) {
    const c = Number.parseInt(f.minConfidence, 10);
    if (Number.isInteger(c) && c >= 0 && c <= 100) q.min_classification_confidence = c;
  }
  if (f.search?.trim()) q.search = f.search.trim();
  if (f.company?.trim()) q.company = f.company.trim();
  if (f.country?.trim()) q.country = f.country.trim();
  if (f.dnc === "only") q.do_not_contact = true;
  if (f.dnc === "hide") q.do_not_contact = false;
  return q;
}

export function priorityTierBadge(tier?: string): { label: string; color: string } {
  switch (tier) {
    case "P1":
      return { label: "P1 · ACM + High Potential", color: "bg-emerald-500/10 text-emerald-400 border-emerald-500/20" };
    case "P2":
      return { label: "P2 · ACM + Tech/Ops", color: "bg-sky-500/10 text-sky-400 border-sky-500/20" };
    case "P3":
      return { label: "P3 · VIT Vellore + High Potential", color: "bg-amber-500/10 text-amber-400 border-amber-500/20" };
    case "P4":
      return { label: "P4 · VIT Vellore + Tech/Ops", color: "bg-purple-500/10 text-purple-400 border-purple-500/20" };
    default:
      return { label: "REVIEW · Ambiguous", color: "bg-zinc-500/10 text-zinc-400 border-zinc-500/20" };
  }
}

export function gradEligibilityBadge(elig?: string): { label: string; color: string } {
  switch (elig) {
    case "eligible":
      return { label: "Eligible (<=2022)", color: "text-emerald-400 bg-emerald-500/10 border-emerald-500/20" };
    case "borderline_2022":
      return { label: "Borderline 2022", color: "text-amber-400 bg-amber-500/10 border-amber-500/20" };
    case "ineligible":
      return { label: "Ineligible (>2022)", color: "text-rose-400 bg-rose-500/10 border-rose-500/20" };
    default:
      return { label: "Unknown Cohort", color: "text-zinc-400 bg-zinc-500/10 border-zinc-500/20" };
  }
}

export function importSummary(r: AlumniImportResult): string {
  return `Imported: ${r.created} new, ${r.updated} updated, ${r.skipped} skipped.`;
}

export const APPROVAL_NOTICE = "Saved for your approval. Griffin never sends this; you send it yourself.";

/** A readable message for a failed outreach call; 409 means the person is do-not-contact. */
export function outreachErrorMessage(e: unknown): string {
  if ((e as { status?: number } | null)?.status === 409) {
    return "This person is marked do not contact, so no draft was saved.";
  }
  return e instanceof Error ? e.message : "Could not save the draft.";
}

/** Replace one person in a list with the server's updated copy. */
export function replacePerson(list: AlumniCard[], updated: AlumniCard): AlumniCard[] {
  return list.map((p) => (p.id === updated.id ? updated : p));
}
