"use client";

import { useEffect, useRef, useState } from "react";
import { ArrowUp, Loader2, RotateCcw, Sparkles, WifiOff } from "lucide-react";
import { api, type SubmitResult } from "@/lib/api";
import { useOnline } from "@/lib/use-online";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ErrorNote } from "@/components/page";
import { PlanCard } from "@/components/plan-card";

type Msg =
  | { id: number; role: "user"; text: string }
  | { id: number; role: "jj"; result: SubmitResult }
  | { id: number; role: "error"; text: string; goal: string };

const SUGGESTIONS = ["Plan a weekly status report", "Review this repo for security issues", "Draft a launch checklist for next week"];

export default function ChatPage() {
  const [messages, setMessages] = useState<Msg[]>([]);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const online = useOnline();
  const endRef = useRef<HTMLDivElement>(null);
  const nextId = useRef(1);

  useEffect(() => {
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    endRef.current?.scrollIntoView({ behavior: reduce ? "auto" : "smooth", block: "end" });
  }, [messages, busy]);

  const send = async (raw: string, retry = false) => {
    const goal = raw.trim();
    if (!goal || busy || !online) return;
    setText("");
    setBusy(true);
    // A retry replaces the failed turn instead of repeating the user's message.
    setMessages((m) => (retry ? m.filter((x) => x.role !== "error") : [...m, { id: nextId.current++, role: "user", text: goal }]));
    try {
      const result = await api.submitOrchestratorRequest(goal);
      setMessages((m) => [...m, { id: nextId.current++, role: "jj", result }]);
    } catch (e) {
      const text = e instanceof Error ? e.message : "JJ could not take that request.";
      setMessages((m) => [...m, { id: nextId.current++, role: "error", text, goal }]);
    } finally {
      setBusy(false);
    }
  };

  const empty = messages.length === 0;

  return (
    <div className="mx-auto flex h-[calc(100dvh-8rem)] w-full max-w-3xl flex-col px-4 md:h-dvh md:px-8">
      {!online && (
        <div data-testid="offline-banner" role="status" className="mt-3 flex items-center gap-2 rounded-lg border border-warning/40 bg-warning/10 px-3 py-2.5 text-sm text-warning">
          <WifiOff className="size-4 shrink-0" />
          You are offline. JJ will be available again when your connection returns.
        </div>
      )}

      <div className="flex-1 space-y-4 overflow-y-auto py-5" aria-live="polite" aria-busy={busy}>
        {empty && (
          <div className="flex h-full flex-col items-center justify-center gap-5 text-center">
            <div className="flex size-12 items-center justify-center rounded-full bg-accent text-accent-foreground">
              <Sparkles className="size-5" />
            </div>
            <div>
              <h1 className="text-xl font-semibold tracking-tight text-balance">What should JJ take care of?</h1>
              <p className="mt-1 text-sm text-muted-foreground text-pretty">Describe the outcome. JJ plans the steps and hands them to your team.</p>
            </div>
            <div className="flex w-full flex-col gap-2">
              {SUGGESTIONS.map((s) => (
                <button
                  key={s}
                  type="button"
                  disabled={!online || busy}
                  onClick={() => void send(s)}
                  className="min-h-12 rounded-xl border bg-card px-4 text-left text-sm transition-colors active:bg-muted disabled:opacity-50 md:hover:bg-muted"
                >
                  {s}
                </button>
              ))}
            </div>
          </div>
        )}

        {messages.map((m) =>
          m.role === "user" ? (
            <div key={m.id} className="flex justify-end">
              <p className="max-w-[85%] whitespace-pre-wrap break-words rounded-2xl rounded-br-md bg-primary px-4 py-2.5 text-sm text-primary-foreground">{m.text}</p>
            </div>
          ) : m.role === "error" ? (
            <div key={m.id} className="space-y-2">
              <ErrorNote message={m.text} />
              <Button variant="outline" size="sm" className="h-11" disabled={busy || !online} onClick={() => void send(m.goal, true)}>
                <RotateCcw /> Try again
              </Button>
            </div>
          ) : (
            <PlanCard key={m.id} result={m.result} />
          ),
        )}

        {busy && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground" role="status">
            <Loader2 className="size-4 animate-spin motion-reduce:animate-none" /> JJ is planning
          </div>
        )}
        <div ref={endRef} />
      </div>

      <form
        className="flex items-end gap-2 border-t bg-background py-3"
        onSubmit={(e) => {
          e.preventDefault();
          void send(text);
        }}
      >
        <Textarea
          aria-label="Message JJ"
          value={text}
          disabled={!online}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && window.matchMedia("(min-width: 768px)").matches) {
              e.preventDefault();
              void send(text);
            }
          }}
          placeholder={online ? "Message JJ" : "Offline"}
          rows={1}
          className="max-h-32 min-h-11 resize-none"
        />
        <Button type="submit" size="icon" className="size-11 shrink-0 rounded-full" disabled={busy || !online || !text.trim()} aria-label="Send">
          <ArrowUp />
        </Button>
      </form>
    </div>
  );
}
