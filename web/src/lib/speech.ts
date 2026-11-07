"use client";

import { useEffect, useSyncExternalStore } from "react";

/**
 * Speaking results aloud with the phone's own voices (the browser's speech synthesis). It is instant, free
 * and works offline. The text is spoken sentence by sentence, so the first words start at once instead of
 * after the whole answer is processed.
 */

export const VOICE_KEY = "griffin_voice";
export const RATES = [0.8, 1, 1.15, 1.3, 1.5] as const;

export interface VoicePrefs {
  voiceURI: string | null;
  rate: number;
}
const DEFAULT_PREFS: VoicePrefs = { voiceURI: null, rate: 1 };

/** True once the page is on the client and the browser can speak (false while server rendering, so markup matches). */
export function useSpeechSupported(): boolean {
  return useSyncExternalStore(
    () => () => {},
    speechSupported,
    () => false,
  );
}

export const speechSupported = () => typeof window !== "undefined" && "speechSynthesis" in window && typeof SpeechSynthesisUtterance !== "undefined";

// ---- turning Markdown into something worth hearing ------------------------------------------------

/** Plain speakable text: no code blocks, no link addresses, no symbols read out loud. */
export function toSpeakable(markdown: string): string {
  return markdown
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/`([^`]*)`/g, "$1")
    .replace(/!\[[^\]]*\]\([^)]*\)/g, " ")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/https?:\/\/\S+/g, " ")
    .replace(/^\s{0,3}#{1,6}\s*/gm, "")
    .replace(/^\s*>\s?/gm, "")
    .replace(/^\s*[-*+]\s+/gm, "")
    .replace(/^\s*\d+[.)]\s+/gm, "")
    .replace(/^\s*\|?[\s:|-]{3,}\|?\s*$/gm, " ")
    .replace(/\|/g, ", ")
    .replace(/[*_~]{1,3}/g, "")
    .replace(/<[^>]+>/g, " ")
    .replace(/[ \t]+/g, " ")
    .replace(/\n{2,}/g, ". ")
    .replace(/\n/g, " ")
    .replace(/\s+([.,;:!?])/g, "$1")
    .replace(/([.!?])\s*\./g, "$1")
    .trim();
}

/** Splits into sentences, merging fragments that are too short to sound natural and capping long ones. */
export function splitSentences(text: string, maxLen = 220): string[] {
  const raw = text.match(/[^.!?]+[.!?]+["')\]]*|[^.!?]+$/g) ?? [];
  const out: string[] = [];
  for (const piece of raw.map((s) => s.trim()).filter(Boolean)) {
    const last = out[out.length - 1];
    if (last && (last.length < 25 || piece.length < 12) && last.length + piece.length < maxLen) out[out.length - 1] = `${last} ${piece}`;
    else out.push(piece);
  }
  const capped: string[] = [];
  for (const s of out) {
    if (s.length <= maxLen) {
      capped.push(s);
      continue;
    }
    let rest = s;
    while (rest.length > maxLen) {
      const cut = rest.lastIndexOf(" ", maxLen);
      const at = cut > maxLen / 2 ? cut : maxLen;
      capped.push(rest.slice(0, at).trim());
      rest = rest.slice(at).trim();
    }
    if (rest) capped.push(rest);
  }
  return capped;
}

// ---- preferences (per device) ------------------------------------------------------------------------

const prefListeners = new Set<() => void>();
let prefs: VoicePrefs | null = null;

function readPrefs(): VoicePrefs {
  try {
    const raw = localStorage.getItem(VOICE_KEY);
    if (raw) {
      const p = JSON.parse(raw) as Partial<VoicePrefs>;
      return { voiceURI: typeof p.voiceURI === "string" ? p.voiceURI : null, rate: typeof p.rate === "number" && p.rate >= 0.5 && p.rate <= 2 ? p.rate : 1 };
    }
  } catch {
    /* storage blocked or corrupt: fall back to defaults */
  }
  return DEFAULT_PREFS;
}

export function setVoicePrefs(next: Partial<VoicePrefs>) {
  prefs = { ...(prefs ?? readPrefs()), ...next };
  try {
    localStorage.setItem(VOICE_KEY, JSON.stringify(prefs));
  } catch {
    /* the choice just will not persist */
  }
  prefListeners.forEach((l) => l());
}

export function useVoicePrefs(): VoicePrefs {
  return useSyncExternalStore(
    (l) => {
      prefListeners.add(l);
      return () => prefListeners.delete(l);
    },
    () => (prefs ??= readPrefs()),
    () => DEFAULT_PREFS,
  );
}

// ---- available voices ---------------------------------------------------------------------------------

const noVoices: SpeechSynthesisVoice[] = [];
let voiceCache: SpeechSynthesisVoice[] = noVoices;

function loadVoices() {
  if (!speechSupported()) return noVoices;
  const list = window.speechSynthesis.getVoices();
  // Keep a stable reference unless the list really changed (useSyncExternalStore compares by identity).
  if (list.length !== voiceCache.length || list.some((v, i) => v.voiceURI !== voiceCache[i]?.voiceURI)) voiceCache = [...list];
  return voiceCache;
}

/** Voices installed on this device, best first: the user's language, then the rest by name. */
export function useVoices(): SpeechSynthesisVoice[] {
  const voices = useSyncExternalStore(
    (l) => {
      if (!speechSupported()) return () => {};
      window.speechSynthesis.addEventListener("voiceschanged", l);
      return () => window.speechSynthesis.removeEventListener("voiceschanged", l);
    },
    loadVoices,
    () => noVoices,
  );
  // Some browsers only populate the list after the first call.
  useEffect(() => {
    if (speechSupported()) window.speechSynthesis.getVoices();
  }, []);
  const lang = typeof navigator !== "undefined" ? navigator.language.toLowerCase() : "en";
  return [...voices].sort((a, b) => {
    const am = a.lang.toLowerCase().startsWith(lang.slice(0, 2)) ? 0 : 1;
    const bm = b.lang.toLowerCase().startsWith(lang.slice(0, 2)) ? 0 : 1;
    return am - bm || a.name.localeCompare(b.name);
  });
}

// ---- speaking -----------------------------------------------------------------------------------------

const speakingListeners = new Set<() => void>();
let speakingId: string | null = null;
let queueToken = 0;

const setSpeaking = (id: string | null) => {
  speakingId = id;
  speakingListeners.forEach((l) => l());
};

/** The id being read right now, for code outside React (cleanup on unmount). */
export const currentSpeakingId = () => speakingId;

/** Which result (by id) is being read aloud right now, or null. */
export function useSpeakingId(): string | null {
  return useSyncExternalStore(
    (l) => {
      speakingListeners.add(l);
      return () => speakingListeners.delete(l);
    },
    () => speakingId,
    () => null,
  );
}

export function stopSpeaking() {
  queueToken++;
  if (speechSupported()) window.speechSynthesis.cancel();
  setSpeaking(null);
}

function pickVoice(uri: string | null): SpeechSynthesisVoice | undefined {
  if (!uri || !speechSupported()) return undefined;
  return window.speechSynthesis.getVoices().find((v) => v.voiceURI === uri);
}

/**
 * Speaks `text` (already speakable) sentence by sentence. Starting a new one stops the previous. Each
 * sentence is queued only when the one before ends, which keeps pause and stop responsive and avoids a
 * known issue where a long queue stalls in some browsers.
 */
export function speak(id: string, text: string, opts?: { voiceURI?: string | null; rate?: number }) {
  if (!speechSupported()) return;
  stopSpeaking();
  const sentences = splitSentences(text);
  if (sentences.length === 0) return;
  const token = ++queueToken;
  const p = prefs ?? readPrefs();
  const voice = pickVoice(opts?.voiceURI === undefined ? p.voiceURI : opts.voiceURI);
  const rate = opts?.rate ?? p.rate;
  setSpeaking(id);

  let i = 0;
  const next = () => {
    if (token !== queueToken) return;
    if (i >= sentences.length) {
      setSpeaking(null);
      return;
    }
    const u = new SpeechSynthesisUtterance(sentences[i++]);
    if (voice) {
      u.voice = voice;
      u.lang = voice.lang;
    }
    u.rate = rate;
    u.onend = next;
    u.onerror = () => {
      if (token === queueToken) setSpeaking(null);
    };
    window.speechSynthesis.speak(u);
  };
  next();
}
