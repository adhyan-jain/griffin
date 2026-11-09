"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useTheme } from "next-themes";
import { toast } from "sonner";
import { Loader2 } from "lucide-react";
import { api, clearAuthToken, getCurrentUser, type RunnerStatus } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { saveManagerName, useManagerName } from "@/lib/manager-name";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { SystemStatusList, useSystemStatus } from "@/components/system-status";
import { PageContainer, PageHeader } from "@/components/page";
import { useFxMode, type FxMode } from "@/lib/fx";
import { RATES, setVoicePrefs, speak, stopSpeaking, useSpeechSupported, useVoicePrefs, useVoices } from "@/lib/speech";

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-3 rounded-xl border bg-card p-4">
      <h2 className="text-sm font-semibold">{title}</h2>
      {children}
    </section>
  );
}

/** The top manager's name: spoken, shown and used in prompts everywhere. */
function ManagerNameField() {
  const saved = useManagerName();
  const [draft, setDraft] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const value = draft ?? saved;
  const changed = value.trim() !== saved;
  const save = async () => {
    setBusy(true);
    try {
      const name = await saveManagerName(value);
      setDraft(null);
      toast.success(`Your manager is now ${name}.`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save the name.");
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      className="space-y-2"
      onSubmit={(e) => {
        e.preventDefault();
        if (changed && !busy) void save();
      }}
    >
      <div className="flex gap-2">
        <Input
          value={value}
          onChange={(e) => setDraft(e.target.value)}
          maxLength={30}
          autoComplete="off"
          aria-label="Manager name"
          className="h-11 min-w-0 flex-1"
        />
        <Button type="submit" className="h-11" disabled={!changed || busy || value.trim() === ""}>
          {busy && <Loader2 className="animate-spin" />} Save
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        Chat, tasks and voice all use this name. Letters, digits, spaces, hyphens and apostrophes; up to 30 characters.
        Longer names are easier to recognize when spoken.
      </p>
    </form>
  );
}

const FX_OPTIONS: { value: FxMode; label: string; hint: string }[] = [
  { value: "full", label: "Full", hint: "Deep field, live core, glass depth." },
  { value: "lite", label: "Lite", hint: "Fewer particles, 30 fps. Easier on battery." },
  { value: "off", label: "Off", hint: "No 3D at all. Plain interface." },
];

function FxPicker() {
  const [mode, setMode] = useFxMode();
  return (
    <div className="space-y-2">
      <Tabs value={mode} onValueChange={(v) => setMode(v as FxMode)}>
        <TabsList className="grid h-auto w-full grid-cols-3 group-data-[orientation=horizontal]/tabs:h-auto">
          {FX_OPTIONS.map((o) => (
            <TabsTrigger key={o.value} value={o.value} className="h-10">
              {o.label}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <p className="text-xs text-muted-foreground">{FX_OPTIONS.find((o) => o.value === mode)?.hint} Saved on this device only.</p>
    </div>
  );
}

/** Pick the voice results are read in, and the speed; Preview plays a sample in that voice. */
function VoicePicker() {
  const supported = useSpeechSupported();
  const voices = useVoices();
  const prefs = useVoicePrefs();
  if (!supported) return <p className="text-sm text-muted-foreground">This browser cannot read text aloud, so there is nothing to pick here.</p>;
  const sample = "Hi, this is how I will sound when I read your results to you.";
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Select value={prefs.voiceURI ?? "default"} onValueChange={(v) => setVoicePrefs({ voiceURI: v === "default" ? null : v })}>
          <SelectTrigger className="h-11 min-w-0 flex-1" aria-label="Voice">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="default">Device default</SelectItem>
            {voices.map((v) => (
              <SelectItem key={v.voiceURI} value={v.voiceURI}>
                {v.name} ({v.lang})
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button variant="outline" className="h-11 shrink-0" onClick={() => speak("preview", sample)}>
          Preview
        </Button>
        <Button variant="ghost" className="h-11 shrink-0" onClick={stopSpeaking} aria-label="Stop preview">
          Stop
        </Button>
      </div>
      <Tabs value={String(prefs.rate)} onValueChange={(v) => setVoicePrefs({ rate: Number(v) })}>
        <TabsList className="grid h-auto w-full grid-cols-5 p-[3px] group-data-[orientation=horizontal]/tabs:h-auto" aria-label="Speed">
          {RATES.map((r) => (
            <TabsTrigger key={r} value={String(r)} className="h-10 px-1 text-xs">
              {r}x
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <p className="text-xs text-muted-foreground">Voices come from your device. Saved on this device only.</p>
    </div>
  );
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="truncate font-medium">{value}</span>
    </div>
  );
}

export default function SettingsPage() {
  const router = useRouter();
  const { theme, setTheme } = useTheme();
  const [email, setEmail] = useState<string>("");
  const [runner, setRunner] = useState<RunnerStatus | null>(null);
  const [db, setDb] = useState<string>("checking");
  const [api_, setApi] = useState<string>("checking");
  const sys = useSystemStatus(true);
  const [waking, setWaking] = useState(false);
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
    setEmail(getCurrentUser()?.email ?? "");
    api.getRunnerStatus().then(setRunner).catch(() => setRunner(null));
    api.getHealth().then((h) => setApi(h.status === "ok" ? "Online" : h.status)).catch(() => setApi("Unreachable"));
    api.getReadiness().then((r) => setDb(r.database === "ok" || r.status === "ready" ? "Connected" : r.database)).catch(() => setDb("Unreachable"));
  }, []);

  const wake = async () => {
    setWaking(true);
    try {
      await api.wakeRunner();
      toast.success("Waking the runner. It takes about a minute.");
      setRunner(await api.getRunnerStatus());
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not wake the runner.");
    } finally {
      setWaking(false);
    }
  };

  return (
    <PageContainer className="max-w-2xl">
      <PageHeader title="Settings" />
      <div className="space-y-4">
        <Section title="Account">
          <Row label="Signed in as" value={email || "Operator"} />
          <Button
            variant="outline"
            className="h-11 w-full"
            onClick={() => {
              clearAuthToken();
              router.replace("/login");
            }}
          >
            Sign out
          </Button>
        </Section>

        <Section title="Appearance">
          {mounted && (
            <Tabs value={theme ?? "dark"} onValueChange={setTheme}>
              <TabsList className="grid h-11 w-full grid-cols-3">
                <TabsTrigger value="light">Light</TabsTrigger>
                <TabsTrigger value="dark">Dark</TabsTrigger>
                <TabsTrigger value="system">System</TabsTrigger>
              </TabsList>
            </Tabs>
          )}
        </Section>

        <Section title="Your manager">
          <ManagerNameField />
        </Section>

        <Section title="Visual effects">
          <FxPicker />
        </Section>
        <Section title="Voice">
          <VoicePicker />
        </Section>
        <Section title="Runner">
          <Row label="State" value={runner ? runner.state : "Unknown"} />
          <Row label="Sleeps when idle" value={runner ? (runner.scale_to_zero_enabled ? "Yes" : "No") : "Unknown"} />
          {runner?.state === "stopped" && (
            <Button className="h-11 w-full" onClick={wake} disabled={waking}>
              {waking && <Loader2 className="animate-spin" />} Wake runner
            </Button>
          )}
        </Section>

        <Section title="System">
          <Row label="API" value={api_} />
          <Row label="Database" value={db} />
          <SystemStatusList status={sys.status} loading={sys.loading} onRefresh={sys.refresh} />
        </Section>
      </div>
    </PageContainer>
  );
}
