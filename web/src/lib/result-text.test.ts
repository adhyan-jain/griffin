import { describe, expect, it } from "vitest";
import { parseResult, slug } from "@/lib/result-text";

describe("parseResult", () => {
  it("extracts the response text from the worker payload", () => {
    expect(parseResult({ response: "  **Yes**, they hire.  " })).toEqual({ text: "**Yes**, they hire.", rest: null });
  });
  it("keeps the other fields for the raw view", () => {
    expect(parseResult({ response: "ok", tokens: 12 })).toEqual({ text: "ok", rest: { tokens: 12 } });
  });
  it("accepts a bare string and falls back through known keys", () => {
    expect(parseResult("plain").text).toBe("plain");
    expect(parseResult({ result: "from result" }).text).toBe("from result");
  });
  it("prefers response over later keys", () => {
    expect(parseResult({ summary: "s", response: "r" }).text).toBe("r");
  });
  it("returns no text for empty or structured payloads", () => {
    expect(parseResult({ response: "   " })).toEqual({ text: null, rest: { response: "   " } });
    expect(parseResult({ items: [1, 2] })).toEqual({ text: null, rest: { items: [1, 2] } });
    expect(parseResult(null)).toEqual({ text: null, rest: null });
    expect(parseResult([1])).toEqual({ text: null, rest: null });
  });
});

describe("slug", () => {
  it("makes a safe filename", () => {
    expect(slug("Check if inVideo has any interns opening or not!")).toBe("check-if-invideo-has-any-interns-opening-or-not");
    expect(slug("???")).toBe("result");
  });
});
