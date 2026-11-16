"use client";

import { use, useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { ArrowRight, Loader2, Pause, Play, RotateCcw } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { ErrorNote, PageContainer } from "@/components/page";
import { speak, stopSpeaking, useSpeakingId, useSpeechSupported } from "@/lib/speech";
import { useManagerName } from "@/lib/manager-name";

type Summary = Awaited<ReturnType<typeof api.getSpokenSummary>>;

const HEADINGS: Record<Summary["case"], string> = {
  answer: "Here is the answer",
  summary: "The short version",
  empty: "Finished",
  failed: "This needs attention",
  approval: "Waiting for your OK",
  running: "Still working",
};

/**
 * Where a finished-task notification lands. It says a few seconds about the task out loud. Browsers only let
 * a page speak after the person has touched it, and a tap on a notification does not count, so the page
 * tries once on its own and otherwise offers one big Play button.
 */
export default function ListenPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const managerName = useManagerName();
  const supported = useSpeechSupported();
  const speakingId = useSpeakingId();
  const [summary, setSummary] = useState<Summary | null>(null);
  const [error, setError] = useState<string | null>(null);
  const triedAuto = useRef(false);
  const listenId = `listen:${id}`;
  const speaking = speakingId === listenId;

  useEffect(() => {
    let alive = true;
    api
      .getSpokenSummary(id)
      .then((s) => alive && setSummary(s))
      .catch((e) => alive && setError(e instanceof Error ? e.message : "Could not load that task."));
    return () => {
      alive = false;
      stopSpeaking(); // leaving the page must not leave the phone talking
    };
  }, [id]);

  const play = useCallback(() => {
    if (summary) speak(listenId, summary.text);
  }, [summary, listenId]);

  useEffect(() => {
    if (summary && supported && !triedAuto.current) {
      triedAuto.current = true;
      play(); // may be refused until the page is touched; the button below always works
    }
  }, [summary, supported, play]);

  return (
    <PageContainer className="max-w-xl">
      {error && <ErrorNote message={error} />}
      {!summary && !error && (
        <div className="flex items-center gap-2 py-16 text-muted-foreground" role="status">
          <Loader2 className="size-5 animate-spin motion-reduce:animate-none" /> {managerName} is getting the summary
        </div>
      )}
      {summary && (
        <section className="space-y-6 py-6" aria-live="polite">
          <div className="space-y-1">
            <p className="text-sm text-muted-foreground">{HEADINGS[summary.case]}</p>
            <h1 className="font-display text-2xl font-bold tracking-tight text-balance">{summary.title}</h1>
          </div>
          <p className="text-xl leading-relaxed text-pretty">{summary.text}</p>
          <div className="flex flex-col gap-3 sm:flex-row">
            {supported ? (
              <Button className="h-14 flex-1 text-base" onClick={speaking ? stopSpeaking : play}>
                {speaking ? <Pause /> : summary ? <Play /> : <RotateCcw />} {speaking ? "Stop" : "Play summary"}
              </Button>
            ) : (
              <p className="text-sm text-muted-foreground">This browser cannot read aloud. The summary is above.</p>
            )}
            <Button asChild variant="outline" className="h-14 flex-1 text-base">
              <Link href={`/tasks/${summary.task_id}`}>
                Open the task <ArrowRight />
              </Link>
            </Button>
          </div>
        </section>
      )}
    </PageContainer>
  );
}
