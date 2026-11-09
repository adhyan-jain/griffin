"use client";

import { Kinetic } from "@/components/fx/kinetic";
import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowUp, Loader2, Mic, RotateCcw, Sparkles, WifiOff } from "lucide-react";
import { api, JJEndpointUnavailable, type Task } from "@/lib/api";
import { useManagerName } from "@/lib/manager-name";
import { buildHistory, pollIntervalMs, reduceReply, type Provider, type ReplyEvent, type Turn } from "@/lib/jj-chat";
import { usePolling } from "@/lib/use-polling";
import { useOnline } from "@/lib/use-online";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { VoicePanel } from "@/components/voice-panel";
import { ErrorNote } from "@/components/page";
import { ChatText } from "@/components/chat-text";
import { PlanCard } from "@/components/plan-card";

const SUGGESTIONS = ["Plan a weekly status report", "Review this repo for security issues", "Draft a launch checklist for next week"];
const CONVERSATION_KEY = "griffin_jj_conversation";
const MAX_POLL_ERRORS = 6;
let memoryConversationId: string | null = null;

/** One id per chat session; sessionStorage keeps the thread across a refresh (memory if blocked). */
function conversationId(): string {
  try {
    const stored = sessionStorage.getItem(CONVERSATION_KEY);
    if (stored) return stored;
    const fresh = crypto.randomUUID();
    sessionStorage.setItem(CONVERSATION_KEY, fresh);
    return fresh;
  } catch {
    return (memoryConversationId ??= crypto.randomUUID());
  }
}

const errText = (e: unknown) => (e instanceof Error && e.message ? e.message : "JJ could not take that message.");
const providerLine = (p: Provider) => [p.name, p.version && `v${p.version}`, p.model].filter(Boolean).join(" · ");

/** The single bounded poll loop for one reply task. Mounted only while that reply is unfinished. */
function ReplyPoller({ taskId, onTask, onGiveUp }: { taskId: string; onTask: (t: Task) => void; onGiveUp: (reason: string) => void }) {
  const [slow, setSlow] = useState(false);
  const errors = useRef(0);
  useEffect(() => {
    const id = setTimeout(() => setSlow(true), 30_000);
    return () => clearTimeout(id);
  }, []);
  usePolling(
    async (signal) => {
      const t = await api.getTask(taskId, { signal });
      errors.current = 0;
      onTask(t);
    },
    {
      intervalMs: pollIntervalMs(slow ? 30_000 : 0),
      onError: () => {
        if (++errors.current >= MAX_POLL_ERRORS) onGiveUp("Lost contact with JJ while waiting for the answer.");
      },
    },
  );
  return null;
}

export default function ChatPage() {
  const managerName = useManagerName();
  const [turns, setTurns] = useState<Turn[]>([]);
  const [text, setText] = useState("");
  const online = useOnline();
  const endRef = useRef<HTMLDivElement>(null);
  const nextId = useRef(1);
  const turnsRef = useRef<Turn[]>([]);
  const endpointMissing = useRef(false); // POST /jj/messages answered 404: never ask again this session
  const [wakeBusy, setWakeBusy] = useState(false);

  useEffect(() => {
    turnsRef.current = turns;
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    endRef.current?.scrollIntoView({ behavior: reduce ? "auto" : "smooth", block: "end" });
  }, [turns]);

  const dispatch = useCallback((id: number, ev: ReplyEvent) => {
    setTurns((ts) => ts.map((t) => (t.id === id ? { ...t, reply: reduceReply(t.reply, ev) } : t)));
  }, []);

  const deliver = async (id: number, message: string, history: ReturnType<typeof buildHistory>) => {
    try {
      if (!endpointMissing.current) {
        try {
          const result = await api.sendJJMessage({ message, conversation_id: conversationId(), history });
          dispatch(id, { type: "accepted", result });
          return;
        } catch (e) {
          if (!(e instanceof JJEndpointUnavailable)) throw e;
          endpointMissing.current = true;
        }
      }
      dispatch(id, { type: "fallback", result: await api.submitOrchestratorRequest(message) });
    } catch (e) {
      dispatch(id, { type: "error", reason: errText(e) });
    }
  };

  const [voiceOpen, setVoiceOpen] = useState(false);

  const send = (raw: string) => {
    const message = raw.trim();
    if (!message || !online) return;
    const id = nextId.current++;
    const history = buildHistory(turnsRef.current);
    setText("");
    setTurns((ts) => [...ts, { id, text: message, reply: { phase: "sending" } }]);
    void deliver(id, message, history);
  };

  const retry = (turn: Turn) => {
    if (!online || turn.reply.phase !== "failed") return;
    const history = buildHistory(turnsRef.current.filter((t) => t.id < turn.id));
    dispatch(turn.id, { type: "retry" });
    void deliver(turn.id, turn.text, history); // a new idempotency key is generated per call
  };

  const wake = async () => {
    setWakeBusy(true);
    try {
      await api.wakeRunner();
    } catch {
      // The reply keeps polling either way.
    } finally {
      setWakeBusy(false);
    }
  };

  const empty = turns.length === 0;
  const anyPending = turns.some((t) => t.reply.phase === "sending" || t.reply.phase === "waiting");

  return (
    <div className="mx-auto flex h-[calc(100dvh-8rem)] w-full max-w-3xl flex-col px-4 md:h-dvh md:px-8">
      {!online && (
        <div data-testid="offline-banner" role="status" className="mt-3 flex items-center gap-2 rounded-lg border border-warning/40 bg-warning/10 px-3 py-2.5 text-sm text-warning">
          <WifiOff className="size-4 shrink-0" />
          You are offline. {managerName} will be available again when your connection returns.
        </div>
      )}

      <div className="flex-1 space-y-4 overflow-y-auto py-5" aria-live="polite" aria-busy={anyPending}>
        {empty && (
          <div className="flex h-full flex-col items-center justify-center gap-5 text-center">
            <div className="flex size-12 items-center justify-center rounded-full bg-accent text-accent-foreground">
              <Sparkles className="size-5" />
            </div>
            <div>
              <Kinetic as="h1" text={`Ask ${managerName} anything`} className="font-display text-3xl font-bold tracking-[-0.04em] text-balance md:text-4xl" />
              <p className="mt-1 text-sm text-muted-foreground text-pretty">{managerName} answers questions directly and plans bigger goals into steps for your team.</p>
            </div>
            <div className="flex w-full flex-col gap-2">
              {SUGGESTIONS.map((s) => (
                <button
                  key={s}
                  type="button"
                  disabled={!online}
                  onClick={() => send(s)}
                  className="min-h-12 rounded-xl border bg-card px-4 text-left text-sm transition-colors active:bg-muted disabled:opacity-50 md:hover:bg-muted"
                >
                  {s}
                </button>
              ))}
            </div>
          </div>
        )}

        {turns.map((t) => (
          <div key={t.id} className="space-y-3" data-testid="chat-turn">
            <div className="flex justify-end">
              <p className="max-w-[85%] whitespace-pre-wrap break-words rounded-2xl rounded-br-md bg-primary px-4 py-2.5 text-sm text-primary-foreground">{t.text}</p>
            </div>

            {(t.reply.phase === "sending" || t.reply.phase === "waiting") && (
              <div className="space-y-1.5" role="status">
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="size-4 animate-spin motion-reduce:animate-none" /> {managerName} is thinking
                </div>
                {t.reply.phase === "waiting" && t.reply.wake?.wake === "failed" && (
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-warning">
                    <span>
                      The worker could not start{t.reply.wake.detail ? `: ${t.reply.wake.detail}` : ""}; {managerName} will keep trying, or tap Wake.
                    </span>
                    <Button variant="outline" size="sm" className="h-11" disabled={wakeBusy || !online} onClick={() => void wake()}>
                      Wake
                    </Button>
                  </div>
                )}
              </div>
            )}

            {t.reply.phase === "waiting" && (
              <ReplyPoller
                taskId={t.reply.taskId}
                onTask={(task) => dispatch(t.id, { type: "task", task })}
                onGiveUp={(reason) => dispatch(t.id, { type: "error", reason })}
              />
            )}

            {t.reply.phase === "done" && (
              <div data-testid="jj-reply" className="w-full max-w-[96%] space-y-1.5 rounded-2xl rounded-bl-md border bg-card px-4 py-3">
                <ChatText text={t.reply.text} />
                {t.reply.provider && providerLine(t.reply.provider) && <p className="text-[11px] text-muted-foreground">{providerLine(t.reply.provider)}</p>}
              </div>
            )}

            {t.reply.phase === "failed" && (
              <div className="space-y-2">
                <ErrorNote message={t.reply.reason} />
                <Button variant="outline" size="sm" className="h-11" disabled={!online} onClick={() => retry(t)}>
                  <RotateCcw /> Retry
                </Button>
              </div>
            )}

            {t.reply.phase === "plan" && (
              <div className="space-y-1.5">
                {t.reply.planOnly && (
                  <p data-testid="plan-only-label" className="text-xs text-warning">
                    Plan only: {managerName}&apos;s direct answers are not available yet, so this is a plan for your team, not a reply.
                  </p>
                )}
                {t.reply.needsDevice && (
                  <div data-testid="needs-device-note" className="rounded-xl border border-warning/40 bg-warning/10 p-3 text-xs leading-relaxed text-foreground">
                    <p className="font-semibold text-warning">Device needed</p>
                    <p className="mt-0.5 text-muted-foreground">{t.reply.needsDevice.message}</p>
                  </div>
                )}
                <PlanCard result={t.reply.result} />
              </div>
            )}
          </div>
        ))}
        <div ref={endRef} />
      </div>

      {voiceOpen && <VoicePanel onHandoff={(t) => send(t)} />}
      <form
        className="flex items-end gap-2 border-t bg-background py-3"
        onSubmit={(e) => {
          e.preventDefault();
          send(text);
        }}
      >
        <Button type="button" size="icon" variant={voiceOpen ? "secondary" : "ghost"} className="size-11 shrink-0 rounded-full" onClick={() => setVoiceOpen((v) => !v)} aria-label={voiceOpen ? "Hide voice" : "Talk instead of typing"} aria-pressed={voiceOpen}>
          <Mic />
        </Button>
        <Textarea
          aria-label={`Message ${managerName}`}
          value={text}
          disabled={!online}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && window.matchMedia("(min-width: 768px)").matches) {
              e.preventDefault();
              send(text);
            }
          }}
          placeholder={online ? `Message ${managerName}` : "Offline"}
          rows={1}
          className="max-h-32 min-h-11 resize-none"
        />
        <Button type="submit" size="icon" className="size-11 shrink-0 rounded-full" disabled={!online || !text.trim()} aria-label="Send">
          <ArrowUp />
        </Button>
      </form>
    </div>
  );
}
