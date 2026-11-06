import { describe, expect, it } from "vitest";
import { isEmptyAnswerFailure, parseResult, slug } from "@/lib/result-text";

describe("parseResult", () => {
  it("extracts the response text from the worker payload", () => {
    expect(parseResult({ response: "  **Yes**, they hire.  " })).toEqual({ text: "**Yes**, they hire.", rest: null, noInfo: false, trimmed: false });
  });
  it("keeps the other fields for the raw view", () => {
    expect(parseResult({ response: "ok", tokens: 12 })).toEqual({ text: "ok", rest: { tokens: 12 }, noInfo: false, trimmed: false });
  });
  it("accepts a bare string and falls back through known keys", () => {
    expect(parseResult("plain").text).toBe("plain");
    expect(parseResult({ result: "from result" }).text).toBe("from result");
  });
  it("prefers response over later keys", () => {
    expect(parseResult({ summary: "s", response: "r" }).text).toBe("r");
  });
  it("reports an empty answer as no information, not as raw data", () => {
    expect(parseResult({ response: "   " })).toEqual({ text: null, rest: null, noInfo: true, trimmed: false });
    expect(parseResult("")).toMatchObject({ noInfo: true });
  });
  it("keeps structured payloads for the raw view", () => {
    expect(parseResult({ items: [1, 2] })).toEqual({ text: null, rest: { items: [1, 2] }, noInfo: false, trimmed: false });
    expect(parseResult(null)).toEqual({ text: null, rest: null, noInfo: false, trimmed: false });
    expect(parseResult([1])).toEqual({ text: null, rest: null, noInfo: false, trimmed: false });
  });
  it("flags a trimmed answer and keeps the flag out of the details", () => {
    expect(parseResult({ response: "short…", trimmed: true })).toEqual({ text: "short…", rest: null, noInfo: false, trimmed: true });
  });
  it("recognises an empty-answer failure", () => {
    expect(isEmptyAnswerFailure("execution reported success but produced no result")).toBe(true);
    expect(isEmptyAnswerFailure('step "x" completed with an empty output')).toBe(true);
    expect(isEmptyAnswerFailure("agy timed out")).toBe(false);
    expect(isEmptyAnswerFailure(null)).toBe(false);
  });
});

describe("slug", () => {
  it("makes a safe filename", () => {
    expect(slug("Check if inVideo has any interns opening or not!")).toBe("check-if-invideo-has-any-interns-opening-or-not");
    expect(slug("???")).toBe("result");
  });
});
