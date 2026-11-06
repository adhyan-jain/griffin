"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { ChevronRight, ListPlus, Notebook, Plus } from "lucide-react";
import { toast } from "sonner";
import { api, type Artifact } from "@/lib/api";
import { usePolling } from "@/lib/use-polling";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, ErrorNote, PageContainer, PageHeader, formatBytes, timeAgo } from "@/components/page";
import { ResponsiveDialog } from "@/components/responsive-dialog";

/** Living lists: Markdown documents a task or you keep adding to (an internship tracker, a reading list). */
export default function ListsPage() {
  const router = useRouter();
  const [lists, setLists] = useState<Artifact[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);

  usePolling(
    async (signal) => {
      const all = await api.listArtifacts(undefined, { signal });
      setLists(all.filter((a) => a.metadata?.kind === "list"));
      setError(null);
    },
    {
      intervalMs: 30000,
      onError: (e) => {
        setError(e instanceof Error ? e.message : "Could not load your lists.");
        setLists((prev) => prev ?? []);
      },
    },
  );

  const create = async () => {
    const title = name.trim();
    if (!title || busy) return;
    setBusy(true);
    try {
      const made = await api.createList(title);
      toast.success("List created");
      router.push(`/lists/${made.id}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not create the list.");
      setBusy(false);
    }
  };

  return (
    <PageContainer>
      <PageHeader
        title="Lists"
        description="Living documents your tasks keep adding to, like an internship tracker."
        actions={
          <Button className="h-10" onClick={() => setOpen(true)}>
            <Plus /> New list
          </Button>
        }
      />
      {error && <ErrorNote message={error} />}
      {lists === null ? (
        <div className="space-y-2">
          <Skeleton className="h-16 w-full rounded-xl" />
          <Skeleton className="h-16 w-full rounded-xl" />
        </div>
      ) : lists.length === 0 ? (
        <EmptyState
          icon={<Notebook />}
          title="No lists yet"
          description="Start one here, or open any finished task and choose Save to list."
          action={
            <Button onClick={() => setOpen(true)}>
              <ListPlus /> New list
            </Button>
          }
        />
      ) : (
        <ul className="divide-y overflow-hidden rounded-2xl border bg-card">
          {lists.map((l) => (
            <li key={l.id}>
              <Link href={`/lists/${l.id}`} className="flex min-h-16 items-center gap-3 px-4 py-3 transition-colors hover:bg-accent/40">
                <span className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-muted text-muted-foreground">
                  <Notebook className="size-5" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">{l.title}</span>
                  <span className="block text-xs text-muted-foreground">
                    {l.file_size_bytes != null ? `${formatBytes(l.file_size_bytes)} · ` : ""}
                    started {timeAgo(l.created_at)}
                  </span>
                </span>
                <ChevronRight className="size-4 text-muted-foreground" />
              </Link>
            </li>
          ))}
        </ul>
      )}

      <ResponsiveDialog open={open} onOpenChange={(o) => !busy && setOpen(o)} title="New list" description="Give it a name you will recognise. You can add to it by hand or from any task result.">
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            void create();
          }}
        >
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Internship tracker" className="h-11" aria-label="List name" />
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button type="button" variant="outline" className="h-11" disabled={busy} onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button type="submit" className="h-11" disabled={busy || !name.trim()}>
              {busy ? "Creating..." : "Create list"}
            </Button>
          </div>
        </form>
      </ResponsiveDialog>
    </PageContainer>
  );
}
