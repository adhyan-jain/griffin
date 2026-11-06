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
}

export function parseResult(payload: unknown): ParsedResult {
  if (typeof payload === "string") return { text: payload.trim() || null, rest: null };
  if (payload === null || typeof payload !== "object" || Array.isArray(payload)) return { text: null, rest: null };

  const obj = payload as Record<string, unknown>;
  for (const key of TEXT_KEYS) {
    const v = obj[key];
    if (typeof v === "string" && v.trim()) {
      const { [key]: _text, ...others } = obj;
      void _text;
      return { text: v.trim(), rest: Object.keys(others).length ? others : null };
    }
  }
  return { text: null, rest: obj };
}

/** A filename-safe slug for downloads. */
export const slug = (s: string) =>
  s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 60) || "result";
