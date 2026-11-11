"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Check, Copy, Laptop, Loader2, Pause, Play, Plus, Smartphone, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, type HostCapability, type HostKind, type HostOverview, type HostView, type RegisteredHost } from "@/lib/api";
import { usePolling } from "@/lib/use-polling";
import {
  BACKED_BY_BUILD,
  CAPABILITY_INFO,
  capabilityLabel,
  describeMode,
  deviceSetup,
  effectiveSummary,
  hostStatus,
  laptopSetup,
} from "@/lib/hosts";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, ErrorNote, PageContainer, PageHeader } from "@/components/page";
import { ResponsiveDialog } from "@/components/responsive-dialog";

const API_ORIGIN = process.env.NEXT_PUBLIC_API_URL || "";

const TONE: Record<"ok" | "warn" | "idle", string> = {
  ok: "bg-emerald-500",
  warn: "bg-amber-500",
  idle: "bg-muted-foreground/40",
};

function Dot({ tone }: { tone: "ok" | "warn" | "idle" }) {
  return <span aria-hidden className={cn("inline-block size-2.5 shrink-0 rounded-full", TONE[tone])} />;
}

function ModeCard({ overview }: { overview: HostOverview }) {
  const m = describeMode(overview.mode);
  return (
    <section aria-label="Primary device" className="rounded-xl border bg-card/60 p-4 backdrop-blur">
      <div className="flex items-center gap-2">
        <Dot tone={m.tone} />
        <h2 className="font-medium">{m.title}</h2>
      </div>
      <p className="mt-1.5 text-sm text-muted-foreground">{m.detail}</p>
    </section>
  );
}

function HostCard({ host, onChanged }: { host: HostView; onChanged: () => void }) {
  const [busy, setBusy] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);

  const st = hostStatus(host);
  const Icon = host.kind === "phone" ? Smartphone : Laptop;

  const togglePause = async () => {
    setBusy(true);
    try {
      await api.updateHost(host.id, { paused: !host.paused });
      toast.success(host.paused ? "Resumed" : "Paused: it will take no new work");
      onChanged();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not update the device.");
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!confirming) {
      setConfirming(true);
      timer.current = setTimeout(() => setConfirming(false), 4000);
      return;
    }
    setBusy(true);
    try {
      await api.revokeHost(host.id);
      toast.success("Device removed. Its token no longer works.");
      onChanged();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not remove the device.");
      setBusy(false);
    }
  };

  return (
    <li className="rounded-xl border bg-card/60 p-4 backdrop-blur">
      <div className="flex items-start gap-3">
        <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
          <Icon className="size-5" />
        </div>
        <div className="min-w-0 flex-1">
          <p className="truncate font-medium">{host.name}</p>
          <p className="mt-0.5 flex items-center gap-1.5 text-sm text-muted-foreground">
            <Dot tone={st.tone} />
            <span>{st.label}</span>
          </p>
          <p className="mt-2 text-sm">{effectiveSummary(host)}</p>
          {host.version && <p className="mt-1 text-xs text-muted-foreground">Version {host.version}</p>}
        </div>
      </div>
      <div className="mt-3 grid grid-cols-2 gap-2">
        <Button variant="outline" className="h-11" onClick={togglePause} disabled={busy}>
          {host.paused ? <Play /> : <Pause />}
          {host.paused ? "Resume" : "Pause"}
        </Button>
        <Button variant={confirming ? "destructive" : "outline"} className="h-11" onClick={remove} disabled={busy}>
          <Trash2 />
          {confirming ? "Tap to confirm" : "Remove"}
        </Button>
      </div>
    </li>
  );
}

function CopyBlock({ label, text }: { label: string; text: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      toast.error("Could not copy. Select the text and copy it by hand.");
    }
  };
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between gap-2">
        <Label>{label}</Label>
        <Button type="button" variant="ghost" size="sm" onClick={copy} className="h-9">
          {copied ? <Check /> : <Copy />}
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-all rounded-lg border bg-muted/50 p-3 text-xs">{text}</pre>
    </div>
  );
}

function RegisterForm({ onCreated }: { onCreated: (r: RegisteredHost) => void }) {
  const [name, setName] = useState("");
  const [kind, setKind] = useState<HostKind>("laptop");
  const [caps, setCaps] = useState<HostCapability[]>(["agent"]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const offered = BACKED_BY_BUILD[kind];

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      onCreated(await api.registerHost({ name: name.trim(), kind, capabilities: caps }));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not add the device.");
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="host-name">Name</Label>
        <Input id="host-name" className="h-11" value={name} maxLength={100} placeholder="My laptop" onChange={(e) => setName(e.target.value)} />
      </div>

      <div className="space-y-2">
        <Label id="host-kind">Kind</Label>
        <div role="radiogroup" aria-labelledby="host-kind" className="grid grid-cols-2 gap-2">
          {(["laptop", "phone"] as HostKind[]).map((k) => {
            const soon = BACKED_BY_BUILD[k].length === 0;
            return (
              <button
                key={k}
                type="button"
                role="radio"
                aria-checked={kind === k}
                disabled={soon}
                onClick={() => {
                  setKind(k);
                  setCaps(BACKED_BY_BUILD[k]);
                }}
                className={cn(
                  "min-h-11 rounded-lg border px-2 text-sm font-medium transition-colors disabled:opacity-50",
                  kind === k ? "border-primary bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-muted",
                )}
              >
                {k === "laptop" ? "Laptop" : "Phone"}
                {soon && <span className="block text-xs font-normal">coming soon</span>}
              </button>
            );
          })}
        </div>
      </div>

      <div className="space-y-2">
        <Label>Allow it to</Label>
        <ul className="space-y-2">
          {offered.map((c) => (
            <li key={c} className="flex items-start gap-3 rounded-lg border p-3">
              <Checkbox
                id={`cap-${c}`}
                checked={caps.includes(c)}
                onCheckedChange={(v) => setCaps((prev) => (v ? [...new Set([...prev, c])] : prev.filter((x) => x !== c)))}
                className="mt-0.5"
              />
              <Label htmlFor={`cap-${c}`} className="flex-1 flex-col items-start gap-0.5 font-normal">
                <span className="font-medium">{CAPABILITY_INFO[c].label}</span>
                <span className="text-xs text-muted-foreground">{CAPABILITY_INFO[c].help}</span>
              </Label>
            </li>
          ))}
        </ul>
        <p className="text-xs text-muted-foreground">
          Browser, files, commands and phone apps arrive with the sandbox. You will grant them here, one at a time.
        </p>
      </div>

      {error && <ErrorNote message={error} />}
      <Button type="submit" className="h-11 w-full" disabled={busy || !name.trim()}>
        {busy && <Loader2 className="animate-spin" />}
        Add device
      </Button>
    </form>
  );
}

function RegisterDialog({ open, onOpenChange, onDone }: { open: boolean; onOpenChange: (o: boolean) => void; onDone: () => void }) {
  const [created, setCreated] = useState<RegisteredHost | null>(null);
  // The dialog body unmounts when closed, so the form starts fresh each time; only the result is kept here.
  const handleOpen = (o: boolean) => {
    if (!o) setCreated(null);
    onOpenChange(o);
  };
  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={handleOpen}
      title={created ? "Device added" : "Add a device"}
      description={created ? "Copy the token now. It is shown once." : "A device connects out to Griffin and does work you allow."}
    >
      {created ? (
        <div className="space-y-4">
          <CopyBlock label="Token" text={created.token} />
          <CopyBlock
            label={deviceSetup(created.host.kind, API_ORIGIN).label}
            text={deviceSetup(created.host.kind, API_ORIGIN).text}
          />
          <p className="text-sm text-muted-foreground">
            Keep the token in a file only you can read. If it leaks, remove the device here and add it again: the old token stops working at once.
          </p>
          <Button className="h-11 w-full" onClick={() => handleOpen(false)}>
            Done
          </Button>
        </div>
      ) : (
        <RegisterForm
          onCreated={(r) => {
            setCreated(r);
            onDone();
          }}
        />
      )}
    </ResponsiveDialog>
  );
}

export default function DevicesPage() {
  const [overview, setOverview] = useState<HostOverview | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [off, setOff] = useState(false);
  const [adding, setAdding] = useState(false);

  const { refresh } = usePolling(
    async (signal) => {
      setOverview(await api.getHosts({ signal }));
      setError(null);
      setOff(false);
    },
    {
      intervalMs: 10000,
      onError: (e) => {
        if (e instanceof ApiError && e.status === 404) setOff(true);
        else setError(e instanceof Error ? e.message : "Could not load your devices.");
      },
    },
  );
  const changed = useCallback(() => refresh(), [refresh]);

  return (
    <PageContainer className="max-w-3xl">
      <PageHeader
        title="Devices"
        description="Your laptop and phone do the work that needs them. The cloud only thinks."
        actions={
          !off && (
            <Button className="h-11" onClick={() => setAdding(true)}>
              <Plus />
              Add
            </Button>
          )
        }
      />

      {off ? (
        <EmptyState
          icon={<Laptop />}
          title="Devices are not switched on yet"
          description="Your server has this feature turned off. Everything else works as before."
        />
      ) : error ? (
        <ErrorNote message={error} />
      ) : !overview ? (
        <div className="space-y-3">
          <Skeleton className="h-24 w-full rounded-xl" />
          <Skeleton className="h-36 w-full rounded-xl" />
        </div>
      ) : (
        <div className="space-y-5">
          <ModeCard overview={overview} />

          {overview.hosts.length === 0 ? (
            <EmptyState
              icon={<Laptop />}
              title="No devices yet"
              description="Add your laptop so work runs there, with its own setup, instead of waiting in the cloud."
              action={
                <Button className="h-11" onClick={() => setAdding(true)}>
                  <Plus />
                  Add a device
                </Button>
              }
            />
          ) : (
            <ul className="space-y-3" aria-label="Your devices">
              {overview.hosts.map((h) => (
                <HostCard key={h.id} host={h} onChanged={changed} />
              ))}
            </ul>
          )}

          {overview.waiting.length > 0 && (
            <section aria-label="Waiting for a device" className="space-y-2">
              <h2 className="text-sm font-medium text-muted-foreground">Waiting for a device</h2>
              <ul className="space-y-2">
                {overview.waiting.map((w) => (
                  <li key={w.id} className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5 text-sm">
                    <span className="min-w-0 truncate">{w.title}</span>
                    <Badge variant="outline" className="shrink-0">
                      {w.covered ? "Starting" : `Needs ${w.requires.map(capabilityLabel).join(", ").toLowerCase()}`}
                    </Badge>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      )}

      <RegisterDialog open={adding} onOpenChange={setAdding} onDone={changed} />
    </PageContainer>
  );
}
