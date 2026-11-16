import { describe, expect, it } from "vitest";
import { parsePeople } from "./outreach-parse";

describe("parsePeople", () => {
  it("reads the three formats, skips blanks and duplicates", () => {
    expect(parsePeople("Ada Lovelace <ada@x.com>\n\nBo, bo@x.com\nsam@x.com\nADA@x.com")).toEqual([
      { name: "Ada Lovelace", handle: "ada@x.com" },
      { name: "Bo", handle: "bo@x.com" },
      { name: "", handle: "sam@x.com" },
    ]);
  });
  it("returns nothing for empty input", () => expect(parsePeople("  \n ")).toEqual([]));
});
