import { describe, expect, it } from "vitest";
import type { CompanyObservation } from "@/lib/api";
import {
  ageText,
  isStale,
  keyFields,
  observationHistory,
  observationView,
  originLabel,
  propertyLabel,
  safeHref,
} from "@/lib/companies";

const obs = (over: Partial<CompanyObservation>): CompanyObservation => ({
  property: "funding",
  value: "Series A",
  source_url: "https://example.com/a",
  claim_origin: "independent",
  evidence_id: null,
  observed_at: "2026-11-01T00:00:00Z",
  age_days: 10,
  ...over,
});

describe("isStale", () => {
  it("is stale only when older than 90 days", () => {
    expect(isStale(90)).toBe(false);
    expect(isStale(91)).toBe(true);
    expect(isStale(0)).toBe(false);
  });
  it("flows into the observation view", () => {
    expect(observationView(obs({ age_days: 120 })).stale).toBe(true);
    expect(observationView(obs({ age_days: 5 })).stale).toBe(false);
  });
});

describe("originLabel / ageText", () => {
  it("separates company-reported from independent and never guesses", () => {
    expect(originLabel("company_reported")).toBe("company-reported");
    expect(originLabel("independent")).toBe("independent");
    expect(originLabel("unknown")).toBe("origin unknown");
    expect(originLabel("")).toBe("origin unknown");
  });
  it("words the age", () => {
    expect(ageText(0)).toBe("today");
    expect(ageText(1)).toBe("1 day old");
    expect(ageText(45)).toBe("45 days old");
  });
});

describe("keyFields keeps every property separate", () => {
  const fields = keyFields([
    obs({ property: "advertised_pay", value: "40k" }),
    obs({ property: "employee_reported_pay", value: "55k" }),
    obs({ property: "internship_stipend", value: "30k" }),
  ]);
  it("lists the seven properties in order with their own labels", () => {
    expect(fields.map((f) => f.label)).toEqual([
      "Funding",
      "Revenue",
      "Profitability",
      "Advertised pay",
      "Employee-reported pay",
      "Internship stipend",
      "Hiring signals",
    ]);
  });
  it("never lets one property's value land in another", () => {
    const by = Object.fromEntries(fields.map((f) => [f.property, f.observation?.value ?? null]));
    expect(by.advertised_pay).toBe("40k");
    expect(by.employee_reported_pay).toBe("55k");
    expect(by.internship_stipend).toBe("30k");
    expect(by.funding).toBeNull();
    expect(by.hiring_signal).toBeNull();
  });
  it("reports no data as null so the page shows unknown", () => {
    expect(keyFields(null).every((f) => f.observation === null)).toBe(true);
    expect(keyFields(undefined)).toHaveLength(7);
  });
  it("takes the newest observation of a property", () => {
    const f = keyFields([obs({ value: "old", age_days: 200 }), obs({ value: "new", age_days: 3 })]);
    expect(f[0].observation?.value).toBe("new");
  });
});

describe("propertyLabel", () => {
  it("labels known properties and falls back readably", () => {
    expect(propertyLabel("employee_reported_pay")).toBe("Employee-reported pay");
    expect(propertyLabel("some_new_thing")).toBe("some new thing");
  });
});

describe("observationHistory", () => {
  it("sorts newest first without mutating the input", () => {
    const input = [obs({ value: "a", observed_at: "2026-01-01T00:00:00Z" }), obs({ value: "b", observed_at: "2026-06-01T00:00:00Z" })];
    expect(observationHistory(input).map((o) => o.value)).toEqual(["b", "a"]);
    expect(input[0].value).toBe("a");
    expect(observationHistory(null)).toEqual([]);
  });
});

describe("safeHref", () => {
  it("allows http(s) only", () => {
    expect(safeHref("https://acme.com/x")).toBe("https://acme.com/x");
    expect(safeHref("javascript:alert(1)")).toBeNull();
    expect(safeHref("")).toBeNull();
    expect(safeHref(null)).toBeNull();
  });
});
