import { parseBlocks } from "@/lib/jj-chat";

/** Renders assistant text as paragraphs, lists and code. React escapes everything; no raw HTML. */
export function ChatText({ text }: { text: string }) {
  return (
    <div className="space-y-2 break-words text-sm leading-relaxed">
      {parseBlocks(text).map((b, i) =>
        b.type === "p" ? (
          <p key={i} className="whitespace-pre-wrap">
            {b.text}
          </p>
        ) : b.type === "code" ? (
          <pre key={i} className="overflow-x-auto rounded-lg bg-muted px-3 py-2 font-mono text-xs">
            {b.text}
          </pre>
        ) : b.type === "ul" ? (
          <ul key={i} className="list-disc space-y-1 pl-5">
            {b.items.map((it, j) => (
              <li key={j}>{it}</li>
            ))}
          </ul>
        ) : (
          <ol key={i} className="list-decimal space-y-1 pl-5">
            {b.items.map((it, j) => (
              <li key={j}>{it}</li>
            ))}
          </ol>
        ),
      )}
    </div>
  );
}
