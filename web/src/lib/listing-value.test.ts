import { describe, expect, it } from "vitest";
import type { Listing, ListingAssessment, ValueComponent } from "@/lib/api";
import { bucketOf, byValue, isEmployerVerified, statedPay, unknownWeight, whyLines } from "@/lib/listing-value";

const comp = (name: string, points: number, max: number, reason: string, unknown = false): ValueComponent => ({ name, points, max, reason, unknown });

const assessment = (over: Partial<ListingAssessment> = {}): ListingAssessment => ({
  employment_class: "internship",
  role_class: "backend",
  eligibility: "confirmed",
  eligibility_reasons: [],
  location_fit: "india",
  grad_year_fit: "fit",
  compensation_monthly_inr: null,
  duration: null,
  skills_matched: [],
  value_score: 70,
  value_class: "strong",
  value_breakdown: [],
  assessed_at: "2026-11-22T09:00:00Z",
  ...over,
});

const listing = (over: Partial<Listing> = {}): Listing =>
  ({ id: "1", source: "greenhouse", company: "Acme", role: "Backend Intern", url: "https://a/1", location: "Pune", pay: "", tier: "C", stage: "verified", stage_note: "", created_at: "2026-11-20T00:00:00Z", ...over }) as Listing;

describe("bucketOf", () => {
  it("puts a listing in its value class when eligibility is known", () => {
    expect(bucketOf(listing({ assessment: assessment({ value_class: "exceptional" }) }))).toBe("exceptional");
    expect(bucketOf(listing({ assessment: assessment({ eligibility: "likely", value_class: "good" }) }))).toBe("good");
  });
  it("keeps unknown eligibility, and listings not assessed yet, in Needs review whatever they score", () => {
    expect(bucketOf(listing({ assessment: assessment({ eligibility: "unknown", value_class: "exceptional", value_score: 90 }) }))).toBe("review");
    expect(bucketOf(listing({ assessment: null }))).toBe("review");
    expect(bucketOf(listing())).toBe("review");
  });
  it("keeps an ineligible listing visible in its own tab instead of ranking it", () => {
    expect(bucketOf(listing({ assessment: assessment({ eligibility: "ineligible", value_class: "low" }) }))).toBe("ineligible");
  });
});

describe("byValue", () => {
  it("sorts by score, best first, with unassessed listings last", () => {
    const a = listing({ id: "a", assessment: assessment({ value_score: 40 }) });
    const b = listing({ id: "b", assessment: assessment({ value_score: 90 }) });
    const c = listing({ id: "c" });
    expect([a, c, b].sort(byValue).map((l) => l.id)).toEqual(["b", "a", "c"]);
  });
});

describe("whyLines", () => {
  const breakdown = [
    comp("depth", 25, 25, "Describes engineering work in detail"),
    comp("employer", 10, 20, "Employer not known to Griffin", true),
    comp("compensation", 7.5, 15, "The listing does not state pay", true),
    comp("relevance", 12, 15, "Asks for 4 of your skills"),
    comp("mentorship", 10, 10, "Mentions mentorship"),
    comp("ppo", 5, 10, "Does not say", true),
    comp("feasibility", 5, 5, "Eligibility confirmed"),
  ];
  it("gives the best-earning known reasons, at most three, and never an unknown one", () => {
    const lines = whyLines(assessment({ value_breakdown: breakdown }));
    expect(lines).toHaveLength(3);
    expect(lines.map((l) => l.label)).toEqual(["Engineering depth", "Mentorship", "Feasibility"]);
    expect(lines.some((l) => l.reason.includes("not known") || l.reason.includes("does not"))).toBe(false);
  });
  it("leads with the cap when the class was held back", () => {
    const capped = whyLines(assessment({ value_breakdown: [...breakdown, { name: "cap", max: 0, points: 0, reason: "Held at exploratory", unknown: false }] }), 2);
    expect(capped[0]).toMatchObject({ label: "Held back", reason: "Held at exploratory" });
    expect(capped).toHaveLength(2);
  });
  it("adds up the points that were not stated in the listing", () => {
    expect(unknownWeight(assessment({ value_breakdown: breakdown }))).toBe(45);
  });
});

describe("statedPay and verification", () => {
  it("shows pay only as the listing stated it", () => {
    expect(statedPay(assessment({ compensation_monthly_inr: 60000 }))).toBe("₹60,000 / month");
    expect(statedPay(assessment({ compensation_monthly_inr: 0 }))).toBe("Unpaid");
    expect(statedPay(assessment({ compensation_monthly_inr: null }))).toBeNull();
    expect(statedPay(null)).toBeNull();
  });
  it("marks only an employer's own posting as verified", () => {
    expect(isEmployerVerified(listing({ verification: "employer_verified" }))).toBe(true);
    expect(isEmployerVerified(listing({ verification: "lead" }))).toBe(false);
    expect(isEmployerVerified(listing())).toBe(false);
  });
});
