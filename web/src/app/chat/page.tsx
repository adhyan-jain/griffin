"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { ArrowUp, Loader2, Sparkles } from "lucide-react";
import { api, type SubmitResult } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ErrorNote } from "@/components/page";

type Msg =
  | { id: number; role: "user"; text: string }
  | { id: number; role: "jj"; result: SubmitResult }
  | { id: number; role: "error"; text: string };

const SUGGESTIONS = ["Plan a weekly status report", "Review this repo for security issues", "Draft a launch checklist for next week"];

export default function ChatPage() {
  const [messages, setMessages] = useState<Msg[]>([]);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const endRef = useRef<HTMLDivElement>(null);
  const nextId = useRef(1);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [messages, busy]);

  const send = async (raw: string) => {
    const goal = raw.trim();
    if (!goal || busy) return;
    setText("");
    setBusy(true);
    setMessages((m) => [...m, { id: nextId.current++, role: "user", text: goal }]);
    try {
      const result = await api.submitOrchestratorRequest(goal);
      setMessages((m) => [...m, { id: nextId.current++, role: "jj", result }]);
    } catch (e) {
      setMessages((m) => [...m, { id: nextId.current++, role: "error", text: e instanceof Error ? e.message : "JJ could not take that request." }]);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto flex h-[calc(100dvh-8rem)] w-full max-w-3xl flex-col px-4 md:h-dvh md:px-8">
      <div className="flex-1 space-y-4 overflow-y-auto py-5" aria-live="polite">
        {messages.length === 0 && (
          <div className="flex h-full flex-col items-center justify-center gap-5 text-center">
            <div className="flex size-12 items-center justify-center rounded-full bg-accent text-accent-foreground">
              <Sparkles className="size-5" />
            </div>
            <div>
              <h1 className="text-xl font-semibold tracking-tight">What should JJ take care of?</h1>
              <p className="mt-1 text-sm text-muted-foreground">Describe the outcome. JJ plans the steps and hands them to your team.</p>
            </div>
            <div className="flex w-full flex-col gap-2">
              {SUGGESTIONS.map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => void send(s)}
                  className="min-h-12 rounded-xl border bg-card px-4 text-left text-sm transition-colors active:bg-muted md:hover:bg-muted"
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
              <p className="max-w-[85%] whitespace-pre-wrap rounded-2xl rounded-br-md bg-primary px-4 py-2.5 text-sm text-primary-foreground">{m.text}</p>
            </div>
          ) : m.role === "error" ? (
            <ErrorNote key={m.id} message={m.text} />
          ) : (
            <div key={m.id} className="max-w-[92%] space-y-3 rounded-2xl rounded-bl-md border bg-card px-4 py-3">
              <p className="text-sm">
                Planned as {(m.result.plan.plan_payload.child_tasks ?? []).length || 1} step
                {(m.result.plan.plan_payload.child_tasks ?? []).length === 1 ? "" : "s"}, led by{" "}
                <span className="font-medium">{m.result.plan.manager_id.replaceAll("_", " ")}</span>.
              </p>
              {(m.result.plan.plan_payload.child_tasks ?? []).length > 0 && (
                <ul className="space-y-1.5 text-sm text-muted-foreground">
                  {(m.result.plan.plan_payload.child_tasks ?? []).map((c) => (
                    <li key={c.key} className="flex gap-2">
                      <span className="mt-2 size-1.5 shrink-0 rounded-full bg-primary" aria-hidden />
                      <span>
                        {c.title}
                        {c.approval_required && <span className="ml-1.5 text-xs text-accent-foreground">needs your OK</span>}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
              <Button asChild variant="outline" size="sm" className="h-9">
                <Link href={`/tasks/${m.result.orchestration.root_task_id}`}>Open task</Link>
              </Button>
            </div>
          ),
        )}

        {busy && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> JJ is planning
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
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && window.matchMedia("(min-width: 768px)").matches) {
              e.preventDefault();
              void send(text);
            }
          }}
          placeholder="Message JJ"
          rows={1}
          className="max-h-32 min-h-11 resize-none"
        />
        <Button type="submit" size="icon" className="size-11 shrink-0 rounded-full" disabled={busy || !text.trim()} aria-label="Send">
          <ArrowUp />
        </Button>
      </form>
    </div>
  );
}
