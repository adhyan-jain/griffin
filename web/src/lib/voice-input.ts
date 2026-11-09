"use client";

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";

/**
 * Speaking to Griffin in the browser, with the browser's own speech recognition. Chrome and Edge send the audio to
 * their speech service to be turned into text; Firefox does not offer it at all, so the microphone button simply
 * does not appear there.
 */

interface RecognitionLike {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  onresult: ((e: { resultIndex: number; results: ArrayLike<ArrayLike<{ transcript: string }> & { isFinal: boolean }> }) => void) | null;
  onerror: ((e: { error: string }) => void) | null;
  onend: (() => void) | null;
  start(): void;
  stop(): void;
  abort(): void;
}
type RecognitionCtor = new () => RecognitionLike;

export function recognitionCtor(): RecognitionCtor | null {
  if (typeof window === "undefined") return null;
  const w = window as unknown as { SpeechRecognition?: RecognitionCtor; webkitSpeechRecognition?: RecognitionCtor };
  return w.SpeechRecognition ?? w.webkitSpeechRecognition ?? null;
}

/** What went wrong, in words, for the few errors a person can do something about. */
export function recognitionError(code: string): string | null {
  switch (code) {
    case "not-allowed":
    case "service-not-allowed":
      return "The microphone is blocked. Allow it for this site in the browser's address bar.";
    case "no-speech":
    case "aborted":
      return null; // silence is not an error
    case "audio-capture":
      return "No microphone was found.";
    case "network":
      return "The browser's speech service could not be reached.";
    default:
      return "Speech recognition stopped unexpectedly.";
  }
}

export function useVoiceInput(onFinal: (text: string) => void) {
  const supported = useSyncExternalStore(
    () => () => {},
    () => recognitionCtor() !== null,
    () => false,
  );
  const [listening, setListening] = useState(false);
  const [interim, setInterim] = useState("");
  const [error, setError] = useState<string | null>(null);
  const rec = useRef<RecognitionLike | null>(null);
  const onFinalRef = useRef(onFinal);
  useEffect(() => {
    onFinalRef.current = onFinal;
  });

  const stop = useCallback(() => rec.current?.stop(), []);

  const start = useCallback(() => {
    const Ctor = recognitionCtor();
    if (!Ctor || rec.current) return;
    const r = new Ctor();
    r.lang = navigator.language || "en-US";
    r.continuous = false;
    r.interimResults = true;
    r.onresult = (e) => {
      let finalText = "";
      let partial = "";
      for (let i = e.resultIndex; i < e.results.length; i++) {
        const text = e.results[i][0]?.transcript ?? "";
        if (e.results[i].isFinal) finalText += text;
        else partial += text;
      }
      setInterim(partial);
      if (finalText.trim()) onFinalRef.current(finalText.trim());
    };
    r.onerror = (e) => setError(recognitionError(e.error));
    r.onend = () => {
      rec.current = null;
      setListening(false);
      setInterim("");
    };
    rec.current = r;
    setError(null);
    setListening(true);
    try {
      r.start();
    } catch {
      rec.current = null;
      setListening(false);
    }
  }, []);

  useEffect(() => () => rec.current?.abort(), []);
  return { supported, listening, interim, error, start, stop };
}
