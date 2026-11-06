"use client";

import { useState } from "react";
import Markdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { Check, ChevronDown, ChevronUp, Code2, Copy, Download, FileText } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { parseResult, slug } from "@/lib/result-text";

/** Past this many characters the reader view folds, so a long answer never takes over the page. */
const FOLD_AT = 1400;

/** Only plain web links and mail links are clickable; anything else (javascript:, data:) renders as text. */
const safeHref = (href?: string) => (href && /^(https?:|mailto:)/i.test(href) ? href : undefined);

const components: Components = {
  h1: ({ children }) => <h3 className="mt-7 mb-2 font-display text-xl font-bold tracking-[-0.02em] first:mt-0">{children}</h3>,
  h2: ({ children }) => <h3 className="mt-7 mb-2 font-display text-lg font-bold tracking-[-0.02em] first:mt-0">{children}</h3>,
  h3: ({ children }) => <h4 className="mt-5 mb-1.5 text-base font-semibold first:mt-0">{children}</h4>,
  h4: ({ children }) => <h4 className="mt-4 mb-1 text-sm font-semibold first:mt-0">{children}</h4>,
  p: ({ children }) => <p className="my-3 leading-7 first:mt-0 last:mb-0">{children}</p>,
  ul: ({ children }) => <ul className="my-3 list-disc space-y-1.5 pl-5 marker:text-primary">{children}</ul>,
  ol: ({ children }) => <ol className="my-3 list-decimal space-y-1.5 pl-5 marker:text-muted-foreground">{children}</ol>,
  li: ({ children }) => <li className="pl-1 leading-7">{children}</li>,
  strong: ({ children }) => <strong className="font-semibold text-foreground">{children}</strong>,
  blockquote: ({ children }) => <blockquote className="my-4 border-l-2 border-primary/60 pl-4 text-muted-foreground">{children}</blockquote>,
  hr: () => <hr className="my-6 border-border" />,
  a: ({ href, children }) => {
    const safe = safeHref(href);
    return safe ? (
      <a href={safe} target="_blank" rel="noopener noreferrer nofollow" className="font-medium text-primary underline decoration-primary/40 underline-offset-4 hover:decoration-primary">
        {children}
      </a>
    ) : (
      <span>{children}</span>
    );
  },
  pre: ({ children }) => <pre className="my-4 whitespace-pre-wrap break-words rounded-xl border bg-muted/60 p-4 font-mono text-[13px] leading-6">{children}</pre>,
  code: ({ children }) => <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-[0.86em] [pre_&]:bg-transparent [pre_&]:p-0">{children}</code>,
  table: ({ children }) => (
    <div className="my-4 overflow-x-auto rounded-xl border">
      <table className="w-full text-left text-sm">{children}</table>
    </div>
  ),
  thead: ({ children }) => <thead className="bg-muted/60">{children}</thead>,
  th: ({ children }) => <th className="px-3 py-2 font-semibold">{children}</th>,
  td: ({ children }) => <td className="border-t px-3 py-2 align-top">{children}</td>,
};

const pretty = (v: unknown) => JSON.stringify(v, null, 2);

/**
 * The result of a task, as a reader: the answer rendered as formatted text with copy, download and a
 * raw-data toggle, folded when long. The raw payload is always one tap away, and a payload with no
 * readable text falls back to wrapped JSON (never a sideways scrollbar).
 */
export function ResultView({ payload, title }: { payload: unknown; title: string }) {
  const { text, rest } = parseResult(payload);
  const [raw, setRaw] = useState(false);
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  const long = !!text && text.length > FOLD_AT;
  const showRaw = raw || text === null;
  const body = showRaw ? pretty(payload) : text!;

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(body);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      toast.error("Could not copy. Select the text instead.");
    }
  };
  const download = () => {
    const url = URL.createObjectURL(new Blob([body], { type: showRaw ? "application/json" : "text/markdown" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `${slug(title)}.${showRaw ? "json" : "md"}`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <section aria-label="Result" className="overflow-hidden rounded-2xl border bg-card">
      <div className="flex items-center justify-between gap-2 border-b px-4 py-2.5">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <FileText className="size-4 text-primary" /> Result
        </h2>
        <div className="flex items-center gap-1">
          {text !== null && (
            <Button variant="ghost" size="sm" onClick={() => setRaw((v) => !v)} aria-pressed={raw} className={cn("h-8 gap-1.5", raw && "bg-accent text-accent-foreground")}>
              <Code2 className="size-4" /> <span className="hidden sm:inline">Raw</span>
            </Button>
          )}
          <Button variant="ghost" size="sm" onClick={copy} className="h-8 gap-1.5" aria-label="Copy result">
            {copied ? <Check className="size-4 text-success" /> : <Copy className="size-4" />}
            <span className="hidden sm:inline">{copied ? "Copied" : "Copy"}</span>
          </Button>
          <Button variant="ghost" size="sm" onClick={download} className="h-8 gap-1.5" aria-label="Download result">
            <Download className="size-4" /> <span className="hidden sm:inline">Download</span>
          </Button>
        </div>
      </div>

      <div className="relative">
        <div className={cn("px-4 py-4 text-[15px] md:px-6 md:py-5", long && !open && !showRaw && "max-h-[24rem] overflow-hidden")}>
          {showRaw ? (
            <pre className="whitespace-pre-wrap break-words font-mono text-[13px] leading-6">{body}</pre>
          ) : (
            <div className="max-w-prose break-words">
              <Markdown remarkPlugins={[remarkGfm]} components={components} skipHtml>
                {text!}
              </Markdown>
            </div>
          )}
          {!showRaw && rest && (
            <details className="mt-5 rounded-xl border bg-muted/40 px-3 py-2 text-sm">
              <summary className="cursor-pointer font-medium text-muted-foreground">Other details</summary>
              <pre className="mt-2 whitespace-pre-wrap break-words font-mono text-xs leading-5">{pretty(rest)}</pre>
            </details>
          )}
        </div>
        {long && !open && !showRaw && <div aria-hidden className="pointer-events-none absolute inset-x-0 bottom-0 h-24 bg-gradient-to-t from-card to-transparent" />}
      </div>

      {long && !showRaw && (
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="flex h-11 w-full items-center justify-center gap-1.5 border-t text-sm font-medium text-primary transition-colors hover:bg-accent/40"
          aria-expanded={open}
        >
          {open ? (
            <>
              <ChevronUp className="size-4" /> Show less
            </>
          ) : (
            <>
              <ChevronDown className="size-4" /> Show full result
            </>
          )}
        </button>
      )}
    </section>
  );
}
