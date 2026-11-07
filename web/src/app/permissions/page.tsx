"use client";

import { useEffect, useRef, useState } from "react";
import { Folder, Loader2, Plus, Timer, X } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, type Policy, type PolicyMode, type PolicyRule, type PolicyView } from "@/lib/api";
import {
  CLASS_INFO,
  CLASS_ORDER,
  FULL_CHOICES,
  MODE_LABEL,
  NEVER_AUTO,
  appRules,
  classMode,
  contactRules,
  folderError,
  folderKind,
  folderRules,
  fullLabel,
  fullRemainingMs,
  removeRule,
  setAppMode,
  setClassMode,
  setContactMode,
  setFolder,
  setFull,
  type FolderKind,
} from "@/lib/permissions";
import { usePolling } from "@/lib/use-polling";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, ErrorNote, PageContainer, PageHeader } from "@/components/page";

function Segmented({
  value,
  onChange,
  options,
  label,
  disabled,
}: {
  value: string;
  onChange: (v: string) => void;
  options: { value: string; label: string; disabled?: boolean }[];
  label: string;
  disabled?: boolean;
}) {
  return (
    <div role="radiogroup" aria-label={label} className="grid auto-cols-fr grid-flow-col gap-1 rounded-lg bg-muted p-1">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          disabled={disabled || o.disabled}
          onClick={() => onChange(o.value)}
          className={cn(
            "min-h-10 rounded-md px-2 text-sm font-medium transition-colors disabled:opacity-40",
            value === o.value ? "bg-background shadow-sm" : "text-muted-foreground hover:text-foreground",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

const MODE_OPTIONS = (noAuto: boolean) =>
  (["off", "ask", "auto"] as PolicyMode[]).map((m) => ({ value: m, label: MODE_LABEL[m], disabled: noAuto && m === "auto" }));

function Section({ title, description, children }: { title: string; description?: string; children: React.ReactNode }) {
  return (
    <section className="space-y-3 rounded-xl border bg-card/60 p-4 backdrop-blur">
      <div>
        <h2 className="font-medium">{title}</h2>
        {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
      </div>
      {children}
    </section>
  );
}

function AddName({ placeholder, onAdd, label }: { placeholder: string; onAdd: (name: string) => void; label: string }) {
  const [v, setV] = useState("");
  return (
    <form
      className="flex gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        if (!v.trim()) return;
        onAdd(v);
        setV("");
      }}
    >
      <Input className="h-11" value={v} placeholder={placeholder} aria-label={label} maxLength={100} onChange={(e) => setV(e.target.value)} />
      <Button type="submit" variant="outline" className="h-11 shrink-0" disabled={!v.trim()}>
        <Plus />
        Add
      </Button>
    </form>
  );
}

function NameRow({ name, mode, onMode, onRemove }: { name: string; mode: PolicyMode; onMode: (m: PolicyMode) => void; onRemove: () => void }) {
  return (
    <li className="space-y-2 rounded-lg border p-3">
      <div className="flex items-center justify-between gap-2">
        <span className="min-w-0 truncate font-medium">{name}</span>
        <Button variant="ghost" size="icon" aria-label={`Remove ${name}`} onClick={onRemove} className="size-9">
          <X />
        </Button>
      </div>
      <Segmented label={`Sending to ${name}`} value={mode} onChange={(v) => onMode(v as PolicyMode)} options={MODE_OPTIONS(false)} />
    </li>
  );
}

function FolderSection({ policy, save }: { policy: Policy; save: (p: Policy) => void }) {
  const [path, setPath] = useState("");
  const [kind, setKind] = useState<FolderKind>("read");
  const err = path.trim() ? folderError(path) : null;
  const rules = folderRules(policy);
  return (
    <Section
      title="Folders"
      description="Folders the assistant may read without asking, and folders it must never touch. Credentials (ssh keys, cloud logins, password stores) are always off limits."
    >
      {rules.length > 0 && (
        <ul className="space-y-2">
          {rules.map((r) => (
            <li key={r.folder} className="flex items-center gap-2 rounded-lg border p-3">
              <Folder className="size-4 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <p className="truncate font-mono text-sm">{r.folder}</p>
                <p className="text-xs text-muted-foreground">{folderKind(r) === "never" ? "Never touch" : "May read without asking"}</p>
              </div>
              <Button variant="ghost" size="icon" aria-label={`Remove ${r.folder}`} onClick={() => save(removeRule(policy, r))} className="size-9">
                <X />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <form
        className="space-y-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (!path.trim() || err) return;
          save(setFolder(policy, path, kind));
          setPath("");
        }}
      >
        <Input className="h-11 font-mono" value={path} placeholder="/home/you/documents" aria-label="Folder path" onChange={(e) => setPath(e.target.value)} />
        {err && <p className="text-xs text-destructive">{err}</p>}
        <div className="flex gap-2">
          <div className="flex-1">
            <Segmented
              label="Folder rule"
              value={kind}
              onChange={(v) => setKind(v as FolderKind)}
              options={[
                { value: "read", label: "Read freely" },
                { value: "never", label: "Never touch" },
              ]}
            />
          </div>
          <Button type="submit" variant="outline" className="h-12 shrink-0" disabled={!path.trim() || !!err}>
            <Plus />
            Add
          </Button>
        </div>
      </form>
    </Section>
  );
}

export default function PermissionsPage() {
  const [view, setView] = useState<PolicyView | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [off, setOff] = useState(false);
  const [saving, setSaving] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const versionRef = useRef(0);

  const savingRef = useRef(false);

  // Poll so a change made on another device shows up here; never apply a poll that raced a save.
  const { refresh } = usePolling(
    async (signal) => {
      const v = await api.getPermissions({ signal });
      if (savingRef.current) return;
      versionRef.current = v.version;
      setView(v);
      setError(null);
      setOff(false);
    },
    {
      intervalMs: 30000,
      onError: (e) => {
        if (e instanceof ApiError && e.status === 404) setOff(true);
        else setError(e instanceof Error ? e.message : "Could not load your permissions.");
      },
    },
  );

  // Keep the Full-mode countdown honest.
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 30000);
    return () => clearInterval(t);
  }, []);

  /** Saves immediately. A conflict means another device saved first: reload instead of overwriting. */
  const save = async (next: Policy) => {
    if (savingRef.current) return;
    savingRef.current = true;
    setSaving(true);
    try {
      const v = await api.savePermissions(next, versionRef.current);
      versionRef.current = v.version;
      setView(v);
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        toast.error("Changed on another device. Showing the latest; please redo your change.");
        savingRef.current = false;
        refresh();
      } else {
        toast.error(e instanceof Error ? e.message : "Could not save.");
      }
    } finally {
      savingRef.current = false;
      setSaving(false);
    }
  };

  const policy = view?.policy;
  const fullMs = policy ? fullRemainingMs(policy, now) : 0;

  return (
    <PageContainer className="max-w-3xl">
      <PageHeader
        title="Permissions"
        description="What your devices may do on their own, what they must ask about, and what is never allowed. Changes save as you make them."
        actions={saving ? <Loader2 className="size-5 animate-spin text-muted-foreground" aria-label="Saving" /> : undefined}
      />

      {off ? (
        <EmptyState icon={<Folder />} title="Permissions are not switched on yet" description="Your server has devices turned off. Everything else works as before." />
      ) : error ? (
        <ErrorNote message={error} />
      ) : !policy ? (
        <div className="space-y-3">
          <Skeleton className="h-64 w-full rounded-xl" />
          <Skeleton className="h-32 w-full rounded-xl" />
        </div>
      ) : (
        <div className="space-y-5">
          {view?.version === 0 && (
            <p className="rounded-lg border border-dashed px-3 py-2.5 text-sm text-muted-foreground">
              You have not changed anything yet. The safe default applies: looking and preparing are allowed, everything else asks you first.
            </p>
          )}

          <Section title="Everyday actions" description="What happens for each kind of action, on every device.">
            <ul className="space-y-3">
              {CLASS_ORDER.map((c) => (
                <li key={c} className="space-y-1.5">
                  <div>
                    <p className="font-medium">{CLASS_INFO[c].label}</p>
                    <p className="text-xs text-muted-foreground">{CLASS_INFO[c].help}</p>
                  </div>
                  <Segmented
                    label={CLASS_INFO[c].label}
                    value={classMode(policy, c)}
                    onChange={(v) => save(setClassMode(policy, c, v as PolicyMode))}
                    options={MODE_OPTIONS(NEVER_AUTO.includes(c))}
                    disabled={saving}
                  />
                </li>
              ))}
            </ul>
          </Section>

          <Section title="Apps" description="Sending from a specific app can be allowed or blocked on its own, whatever the setting above says.">
            {appRules(policy).length > 0 && (
              <ul className="space-y-2">
                {appRules(policy).map((r: PolicyRule) => (
                  <NameRow key={r.app} name={r.app!} mode={r.mode} onMode={(m) => save(setAppMode(policy, r.app!, m))} onRemove={() => save(removeRule(policy, r))} />
                ))}
              </ul>
            )}
            <AddName label="App name" placeholder="WhatsApp, Slack, Gmail..." onAdd={(n) => save(setAppMode(policy, n, "ask"))} />
          </Section>

          <Section title="People" description="Messages to a specific person can skip the question, or never be sent.">
            {contactRules(policy).length > 0 && (
              <ul className="space-y-2">
                {contactRules(policy).map((r: PolicyRule) => (
                  <NameRow key={r.contact} name={r.contact!} mode={r.mode} onMode={(m) => save(setContactMode(policy, r.contact!, m))} onRemove={() => save(removeRule(policy, r))} />
                ))}
              </ul>
            )}
            <AddName label="Contact name" placeholder="Mom, Alex..." onAdd={(n) => save(setContactMode(policy, n, "ask"))} />
          </Section>

          <FolderSection policy={policy} save={save} />

          <Section
            title="Full access"
            description="Lifts the laptop's folder limits for a set time, then switches off by itself. Credentials stay off limits, and a step still can't do what it did not say it would."
          >
            <div className="flex items-center gap-2 text-sm">
              <Timer className="size-4 text-muted-foreground" />
              <span className={cn(fullMs > 0 && "font-medium text-amber-500")}>{fullLabel(fullMs)}</span>
            </div>
            <div className="grid grid-cols-5 gap-1.5">
              {FULL_CHOICES.map((c) => (
                <Button
                  key={c.label}
                  variant="outline"
                  className="h-11 px-1 text-xs"
                  disabled={saving || (c.hours == null && fullMs === 0)}
                  onClick={() => save(setFull(policy, c.hours))}
                >
                  {c.label}
                </Button>
              ))}
            </div>
          </Section>
        </div>
      )}
    </PageContainer>
  );
}
