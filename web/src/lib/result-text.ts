/**
 * Pulls the human-readable answer out of a task's `output_payload`. Workers return
 * `{ "response": "<markdown>" }`; other providers may use a different key or a bare string. Anything
 * that is not clearly prose stays out of the reader view and is available as raw JSON instead.
 */
const TEXT_KEYS = ["response", "result", "output", "answer", "summary", "text", "message", "content"] as const;

export interface ParsedResult {
  /** The prose to render, or null when the payload has no readable text. */
  text: string | null;
  /** Whatever else the payload carried, for the raw view. null when the text was the whole payload. */
  rest: Record<string, unknown> | null;
  /** The run answered, but with nothing in it: show "no information was found", not an empty card. */
  noInfo: boolean;
  /** The answer was shortened after the task aged; the full text is kept as the task's file. */
  trimmed: boolean;
}

export function parseResult(payload: unknown): ParsedResult {
  if (typeof payload === "string") {
    const t = payload.trim();
    return { text: t || null, rest: null, noInfo: !t, trimmed: false };
  }
  if (payload === null || typeof payload !== "object" || Array.isArray(payload)) return { text: null, rest: null, noInfo: false, trimmed: false };

  const { trimmed: rawTrimmed, ...obj } = payload as Record<string, unknown>;
  const trimmed = rawTrimmed === true;
  for (const key of TEXT_KEYS) {
    const v = obj[key];
    if (typeof v === "string" && v.trim()) {
      const { [key]: _text, ...others } = obj;
      void _text;
      return { text: v.trim(), rest: Object.keys(others).length ? others : null, noInfo: false, trimmed };
    }
  }
  // A text field that is present but blank means the run came back empty-handed.
  const blank = TEXT_KEYS.some((k) => typeof obj[k] === "string") && Object.keys(obj).every((k) => (TEXT_KEYS as readonly string[]).includes(k));
  if (blank) return { text: null, rest: null, noInfo: true, trimmed };
  return { text: null, rest: Object.keys(obj).length ? obj : null, noInfo: false, trimmed };
}

/** True when a task's failure reason means "the run returned nothing", which reads better as "no information found". */
export const isEmptyAnswerFailure = (reason?: string | null) => !!reason && /no result|empty output|returned no/i.test(reason);

/** A filename-safe slug for downloads. */
export const slug = (s: string) =>
  s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 60) || "result";
