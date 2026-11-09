"use client";

import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import { Camera, Download, FileText, KeyRound, Loader2, Lock, LockOpen, MoreVertical, Plus, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { api, type VaultCategory, type VaultEntry, type VaultMiss } from "@/lib/api";
import { CATEGORY_LABELS, addDocument, getPassphrase, isSensitive, openDocument, setPassphrase, subscribePassphrase, type OpenedDocument } from "@/lib/vault";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, ErrorNote, PageContainer, PageHeader, formatBytes, timeAgo } from "@/components/page";
import { ResponsiveDialog } from "@/components/responsive-dialog";

const usePassphrase = () => useSyncExternalStore(subscribePassphrase, getPassphrase, () => null);

function PassphraseBar() {
  const phrase = usePassphrase();
  const [draft, setDraft] = useState("");
  if (phrase) {
    return (
      <div className="flex items-center gap-3 rounded-xl border bg-card p-3">
        <LockOpen className="size-5 shrink-0 text-primary" />
        <p className="min-w-0 flex-1 text-sm">Unlocked on this device until you leave this page.</p>
        <Button variant="outline" className="h-10" onClick={() => setPassphrase(null)}>
          Lock
        </Button>
      </div>
    );
  }
  return (
    <form
      className="space-y-2 rounded-xl border bg-card p-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (draft) {
          setPassphrase(draft);
          setDraft("");
        }
      }}
    >
      <Label htmlFor="vault-passphrase" className="flex items-center gap-2 text-sm font-semibold">
        <KeyRound className="size-4" /> Vault passphrase
      </Label>
      <div className="flex gap-2">
        <Input id="vault-passphrase" type="password" autoComplete="off" className="h-11 min-w-0 flex-1" value={draft} onChange={(e) => setDraft(e.target.value)} placeholder="Opens your ID and bank documents" />
        <Button type="submit" className="h-11" disabled={!draft}>
          Unlock
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        It encrypts ID and bank documents on this device before they are uploaded, and it never leaves the device. If you forget it, those documents cannot be
        recovered by anyone, including Griffin.
      </p>
    </form>
  );
}

/** A decrypted document and the temporary address it is shown at. */
interface Shown {
  doc: OpenedDocument;
  url: string;
}

function Viewer({ shown, onClose }: { shown: Shown | null; onClose: () => void }) {
  return (
    <ResponsiveDialog open={shown !== null} onOpenChange={(o) => !o && onClose()} title={shown?.doc.filename ?? "Document"} description="Decrypted on this device. Closing this removes it from memory.">
      {shown && (
        <div className="space-y-3">
          {shown.doc.mime.startsWith("image/") ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img src={shown.url} alt={shown.doc.filename} className="max-h-[60vh] w-full rounded-lg border object-contain" />
          ) : (
            <p className="text-sm text-muted-foreground">This type of file is opened by saving it to your device.</p>
          )}
          <Button asChild className="h-11 w-full">
            <a href={shown.url} download={shown.doc.filename}>
              <Download /> Save to this device
            </a>
          </Button>
        </div>
      )}
    </ResponsiveDialog>
  );
}

export default function VaultPage() {
  const phrase = usePassphrase();
  const [entries, setEntries] = useState<VaultEntry[] | null>(null);
  const [misses, setMisses] = useState<VaultMiss[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [opening, setOpening] = useState<string | null>(null);
  const [shown, setShown] = useState<Shown | null>(null);
  const closeViewer = useCallback(() => {
    setShown((cur) => {
      if (cur) URL.revokeObjectURL(cur.url); // the decrypted copy does not outlive the viewer
      return null;
    });
  }, []);
  const [toDelete, setToDelete] = useState<VaultEntry | null>(null);
  const [deleting, setDeleting] = useState(false);

  const [addOpen, setAddOpen] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [label, setLabel] = useState("");
  const [category, setCategory] = useState<VaultCategory>("id");
  const [again, setAgain] = useState("");
  const [saving, setSaving] = useState(false);
  const [forMiss, setForMiss] = useState<VaultMiss | null>(null);

  const load = useCallback(async () => {
    try {
      const [e, m] = await Promise.all([api.listVault(), api.listVaultMisses().catch(() => [] as VaultMiss[])]);
      setEntries(e);
      setMisses(m);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load the Vault.");
      setEntries((prev) => prev ?? []);
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  // The first sensitive document fixes the passphrase, so a typo there would lock the owner out later:
  // ask for it twice only then.
  const firstSensitive = isSensitive(category) && !(entries ?? []).some((e) => e.sensitive);
  const needsConfirm = firstSensitive && phrase !== null;

  const startAdd = (miss?: VaultMiss) => {
    setForMiss(miss ?? null);
    setLabel(miss?.query ?? "");
    setFile(null);
    setAgain("");
    setCategory(miss && /pan|aadhaar|aadhar|passport|licen[cs]e|voter|\bid\b/i.test(miss.query) ? "id" : "other");
    setAddOpen(true);
  };

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!file) return;
    if (needsConfirm && again !== phrase) {
      toast.error("The two passphrases do not match.");
      return;
    }
    setSaving(true);
    try {
      const res = await addDocument({ file, label, category });
      toast.success(res.duplicate ? "That document is already in your Vault." : "Added to your Vault.");
      if (forMiss) await api.resolveVaultMiss(forMiss.id).catch(() => {});
      setAddOpen(false);
      await load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not add the document.");
    } finally {
      setSaving(false);
    }
  };

  const open = async (entry: VaultEntry) => {
    setOpening(entry.id);
    try {
      const doc = await openDocument(entry);
      const url = URL.createObjectURL(new Blob([doc.bytes as Uint8Array<ArrayBuffer>], { type: doc.mime }));
      setShown((cur) => {
        if (cur) URL.revokeObjectURL(cur.url);
        return { doc, url };
      });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not open the document.");
    } finally {
      setOpening(null);
    }
  };

  const remove = async () => {
    if (!toDelete) return;
    setDeleting(true);
    try {
      await api.deleteArtifact(toDelete.id);
      toast.success("Deleted from your Vault.");
      setToDelete(null);
      await load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not delete the document.");
    } finally {
      setDeleting(false);
    }
  };

  const dismissMiss = async (m: VaultMiss) => {
    setMisses((prev) => prev.filter((x) => x.id !== m.id));
    await api.resolveVaultMiss(m.id).catch(() => {});
  };

  return (
    <PageContainer className="max-w-3xl">
      <PageHeader
        title="Vault"
        description="Documents you may need on your phone when your laptop is off."
        actions={
          <Button className="h-10" onClick={() => startAdd()}>
            <Plus /> Add
          </Button>
        }
      />

      <div className="space-y-4">
        <PassphraseBar />
        {error && <ErrorNote message={error} />}

        {misses.length > 0 && (
          <section className="space-y-2 rounded-xl border bg-card p-4" aria-label="Documents that were needed">
            <h2 className="text-sm font-semibold">Needed, but not in your Vault</h2>
            <ul className="divide-y">
              {misses.map((m) => (
                <li key={m.id} className="flex items-center gap-2 py-2">
                  <p className="min-w-0 flex-1 truncate text-sm">
                    {m.query}
                    {m.count > 1 && <span className="text-muted-foreground"> · asked {m.count} times</span>}
                  </p>
                  <Button size="sm" className="h-9" onClick={() => startAdd(m)}>
                    Add it
                  </Button>
                  <Button size="icon" variant="ghost" className="size-9" aria-label={`Dismiss ${m.query}`} onClick={() => void dismissMiss(m)}>
                    <X />
                  </Button>
                </li>
              ))}
            </ul>
          </section>
        )}

        {entries === null ? (
          <div className="space-y-2">
            <Skeleton className="h-16 w-full rounded-xl" />
            <Skeleton className="h-16 w-full rounded-xl" />
          </div>
        ) : entries.length === 0 ? (
          <EmptyState icon={<Lock />} title="Your Vault is empty" description="Add the documents you would need if you only had your phone: your resume, ID cards, certificates." />
        ) : (
          <ul className="divide-y overflow-hidden rounded-xl border bg-card">
            {entries.map((e) => (
              <li key={e.id} className="flex items-center gap-3 px-4 py-3">
                <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">{e.encrypted ? <Lock className="size-5" /> : <FileText className="size-5" />}</div>
                <button type="button" className="min-w-0 flex-1 text-left" onClick={() => void open(e)} disabled={opening === e.id}>
                  <span className="block truncate text-sm font-medium">{e.label}</span>
                  <span className="block truncate text-xs text-muted-foreground">
                    {[CATEGORY_LABELS[e.category], e.encrypted ? "encrypted" : null, formatBytes(e.size_bytes), timeAgo(e.created_at)].filter(Boolean).join(" · ")}
                  </span>
                </button>
                {opening === e.id && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="ghost" size="icon" className="size-10 shrink-0" aria-label={`Actions for ${e.label}`}>
                      <MoreVertical />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem className="min-h-10" onSelect={() => void open(e)}>
                      <FileText /> Open
                    </DropdownMenuItem>
                    <DropdownMenuItem className="min-h-10 text-destructive focus:text-destructive" onSelect={() => setToDelete(e)}>
                      <Trash2 /> Delete
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </li>
            ))}
          </ul>
        )}
      </div>

      <Viewer shown={shown} onClose={closeViewer} />

      <ResponsiveDialog open={addOpen} onOpenChange={setAddOpen} title="Add to your Vault" description="Up to 5 MB. ID and bank documents are encrypted on this device first.">
        <form onSubmit={save} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="vault-file">Document</Label>
            <Input id="vault-file" type="file" accept="image/*,application/pdf,.md,.txt,.doc,.docx" required className="h-11 pt-2" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
            <Button type="button" variant="outline" className="h-11 w-full" asChild>
              <label className="cursor-pointer">
                <Camera /> Scan with the camera
                <input type="file" accept="image/*" capture="environment" className="sr-only" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
              </label>
            </Button>
            {file && <p className="truncate text-xs text-muted-foreground">Selected: {file.name}</p>}
          </div>
          <div className="space-y-2">
            <Label htmlFor="vault-label">Name</Label>
            <Input id="vault-label" required maxLength={80} className="h-11" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="PAN card" />
          </div>
          <div className="space-y-2">
            <Label htmlFor="vault-category">Kind</Label>
            <Select value={category} onValueChange={(v) => setCategory(v as VaultCategory)}>
              <SelectTrigger id="vault-category" className="h-11">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(CATEGORY_LABELS) as VaultCategory[]).map((c) => (
                  <SelectItem key={c} value={c}>
                    {CATEGORY_LABELS[c]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {isSensitive(category) && !phrase && <p className="rounded-lg bg-muted p-3 text-sm">Unlock the Vault with your passphrase first, on the page behind this one. It encrypts this document before it is uploaded.</p>}
          {needsConfirm && (
            <div className="space-y-2">
              <Label htmlFor="vault-again">Type the passphrase again</Label>
              <Input id="vault-again" type="password" autoComplete="off" className="h-11" value={again} onChange={(e) => setAgain(e.target.value)} />
              <p className="text-xs text-muted-foreground">This is your first protected document, so check the passphrase now: a typo here would lock it for good.</p>
            </div>
          )}
          <Button type="submit" className="h-11 w-full" disabled={!file || !label.trim() || saving || (isSensitive(category) && !phrase) || (needsConfirm && again !== phrase)}>
            {saving && <Loader2 className="animate-spin" />} Add to Vault
          </Button>
        </form>
      </ResponsiveDialog>

      <ResponsiveDialog
        open={toDelete !== null}
        onOpenChange={(o) => !o && setToDelete(null)}
        title="Delete this document?"
        description={toDelete ? `${toDelete.label} will be removed from your Vault. This can't be undone.` : undefined}
      >
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button variant="outline" className="h-11" onClick={() => setToDelete(null)}>
            Keep it
          </Button>
          <Button variant="destructive" className="h-11" onClick={remove} disabled={deleting}>
            {deleting && <Loader2 className="animate-spin" />} Delete
          </Button>
        </div>
      </ResponsiveDialog>
    </PageContainer>
  );
}
