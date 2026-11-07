import { describe, expect, it } from "vitest";
import { splitSentences, toSpeakable } from "@/lib/speech";

describe("toSpeakable", () => {
  it("drops symbols, link addresses and code so nothing silly is read aloud", () => {
    const md = "## Summary\n\n**Yes**, InVideo hires interns. See [the careers page](https://invideo.io/careers) or https://example.com/x.\n\n```text\nSubject: hi\n```\n\n- Roles: engineering\n- Pay: `25k`";
    const out = toSpeakable(md);
    expect(out).not.toMatch(/[#*`]|https?:|\]\(/);
    expect(out).toContain("Summary");
    expect(out).toContain("Yes, InVideo hires interns");
    expect(out).toContain("the careers page");
    expect(out).not.toContain("Subject: hi");
    expect(out).toContain("Roles: engineering");
    expect(out).toContain("Pay: 25k");
  });

  it("reads table rows as short phrases, not pipes and dashes", () => {
    const out = toSpeakable("| Role | Stipend |\n|---|---|\n| Backend | 40k |");
    expect(out).not.toContain("|");
    expect(out).not.toContain("---");
    expect(out).toContain("Backend");
    expect(out).toContain("40k");
  });

  it("returns an empty string for nothing to say", () => {
    expect(toSpeakable("```js\nonly code\n```")).toBe("");
    expect(toSpeakable("   \n\n ")).toBe("");
  });
});

describe("splitSentences", () => {
  it("splits on sentence ends", () => {
    expect(splitSentences("First thing happened here today. Second thing happened there! Is a third one coming now?")).toEqual([
      "First thing happened here today.",
      "Second thing happened there!",
      "Is a third one coming now?",
    ]);
  });

  it("merges fragments that are too short to sound natural", () => {
    const out = splitSentences("Yes. It works well across the whole system today.");
    expect(out).toEqual(["Yes. It works well across the whole system today."]);
  });

  it("caps very long sentences without cutting words", () => {
    const long = Array.from({ length: 80 }, (_, i) => `word${i}`).join(" ");
    const out = splitSentences(long, 100);
    expect(out.every((s) => s.length <= 100)).toBe(true);
    expect(out.join(" ")).toBe(long);
  });

  it("handles text with no final punctuation and empty input", () => {
    expect(splitSentences("no punctuation at all in this line")).toEqual(["no punctuation at all in this line"]);
    expect(splitSentences("")).toEqual([]);
  });
});
