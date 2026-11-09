"use client";

import { useRef, useState } from "react";
import { Loader2, Mic, MicOff, Volume2, VolumeX } from "lucide-react";
import { api, type VoiceTurnResult } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { speak, stopSpeaking } from "@/lib/speech";
import { useManagerName } from "@/lib/manager-name";
import { useVoiceInput } from "@/lib/voice-input";

const KEY = "griffin_voice_speak";
const readPref = () => {
  try {
    return window.localStorage.getItem(KEY) !== "off";
  } catch {
    return true;
  }
};

/**
 * Talk to Griffin from the browser. What is said goes to the same voice route the phone uses: small talk is
 * answered at once, task and approval commands ("list my tasks", "delete finished tasks") are run, and real work is
 * handed to the chat as an ordinary message. A change always asks for a yes first.
 */
export function VoicePanel({ onHandoff }: { onHandoff: (text: string) => void }) {
  const managerName = useManagerName();
  const [busy, setBusy] = useState(false);
  const [card, setCard] = useState<{ said: string; reply: string; token?: string } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [speakOn, setSpeakOn] = useState(true);
  const history = useRef<{ role: "user" | "assistant"; text: string }[]>([]);

  const show = (said: string, r: VoiceTurnResult) => {
    setCard({ said, reply: r.reply, token: r.confirm_token });
    if (r.reply && speakOn) speak("voice-panel", r.reply);
  };

  const handle = async (text: string) => {
    stopSpeaking();
    setBusy(true);
    setError(null);
    try {
      const r = await api.voiceTurn({ text, history: history.current.slice(-8) });
      show(text, r);
      if (r.kind === "conversation" && r.reply) history.current.push({ role: "user", text }, { role: "assistant", text: r.reply });
      if (r.handoff) onHandoff(text);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not reach Griffin.");
    } finally {
      setBusy(false);
    }
  };

  const confirm = async (yes: boolean) => {
    if (!card?.token) return;
    setBusy(true);
    try {
      const r = await api.voiceTurn({ confirm: { token: card.token, yes } });
      show(card.said, r);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not reach Griffin.");
    } finally {
      setBusy(false);
    }
  };

  const mic = useVoiceInput((t) => void handle(t));
  if (!mic.supported) return null; // browsers without speech recognition simply do not get the button

  const toggleSpeak = () => {
    const next = !speakOn;
    setSpeakOn(next);
    if (!next) stopSpeaking();
    try {
      window.localStorage.setItem(KEY, next ? "on" : "off");
    } catch {
      /* private mode: the choice lasts for this visit */
    }
  };

  return (
    <section className="space-y-3 border-t bg-card/60 px-1 py-3" aria-label="Voice">
      <div className="flex items-center gap-3">
        <Button
          type="button"
          size="icon"
          className="size-12 shrink-0 rounded-full"
          variant={mic.listening ? "destructive" : "default"}
          disabled={busy}
          onClick={() => {
            // The first tap also reads the saved "speak answers" choice.
            if (!card) setSpeakOn(readPref());
            if (mic.listening) mic.stop();
            else {
              stopSpeaking();
              mic.start();
            }
          }}
          aria-label={mic.listening ? "Stop listening" : `Talk to ${managerName}`}
          aria-pressed={mic.listening}
        >
          {busy ? <Loader2 className="animate-spin motion-reduce:animate-none" /> : mic.listening ? <MicOff /> : <Mic />}
        </Button>
        <div className="min-w-0 flex-1 text-sm" aria-live="polite">
          {mic.listening ? <span className="text-muted-foreground">{mic.interim || "Listening…"}</span> : busy ? <span className="text-muted-foreground">{managerName} is on it…</span> : <span className="text-muted-foreground">Tap the microphone and speak.</span>}
        </div>
        <Button type="button" size="icon" variant="ghost" className="size-10 shrink-0" onClick={toggleSpeak} aria-label={speakOn ? "Turn spoken answers off" : "Turn spoken answers on"} aria-pressed={speakOn}>
          {speakOn ? <Volume2 /> : <VolumeX />}
        </Button>
      </div>

      {(mic.error || error) && <p className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">{mic.error || error}</p>}

      {card && (
        <div className="space-y-2 rounded-xl border bg-background px-4 py-3 text-sm">
          <p className="text-muted-foreground">You said: {card.said || "(a confirmation)"}</p>
          {card.reply && <p>{card.reply}</p>}
          {card.token && (
            <div className="flex gap-2 pt-1">
              <Button type="button" className="h-11 flex-1" disabled={busy} onClick={() => void confirm(true)}>
                Yes
              </Button>
              <Button type="button" variant="outline" className="h-11 flex-1" disabled={busy} onClick={() => void confirm(false)}>
                No
              </Button>
            </div>
          )}
        </div>
      )}
      <p className="text-xs text-muted-foreground">Your browser&apos;s speech service turns your voice into text (in Chrome that means Google&apos;s). Nothing is recorded by Griffin.</p>
    </section>
  );
}
