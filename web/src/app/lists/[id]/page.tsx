"use client";

import { use, useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ChevronLeft, Download, Loader2, Pencil, Plus, Sparkles, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, type Artifact } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { ErrorNote, PageContainer, timeAgo } from "@/components/page";
import { Kinetic } from "@/components/fx/kinetic";
import { MarkdownText } from "@/components/result-view";
import { ResponsiveDialog } from "@/components/responsive-dialog";

type Editor = null | "add" | "edit" | "delete";

/** The page already shows the list's title, so a leading "# Title" line in the document is dropped. */
function withoutTitleHeading(text: string, title: string): string {
  const m = text.match(/^#\s+(.+?)\s*\n+/);
  return m && m[1].trim().toLowerCase() === title.trim().toLowerCase() ? text.slice(m[0].length) : text;
}

export default function ListPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const router = useRouter();
  const [meta, setMeta] = useState<Artifact | null>(null);
  const [text, setText] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [editor, setEditor] = useState<Editor>(null);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [actionOpen, setActionOpen] = useState(false);
  const [actionInstruction, setActionInstruction] = useState("");
  const [actionBusy, setActionBusy] = useState(false);

  const load = useCallback(
    async (signal?: AbortSignal) => {
      try {
        const [all, body] = await Promise.all([api.listArtifacts(undefined, { signal }), api.getArtifactText(id, { signal })]);
        const found = all.find((a) => a.id === id);
        if (!found) throw new Error("This list no longer exists.");
        setMeta(found);
        setText(body);
        setError(null);
      } catch (e) {
        if ((e as { name?: string })?.name === "AbortError") return;
        setError(e instanceof Error ? e.message : "Could not load this list.");
      }
    },
    [id],
  );

  useEffect(() => {
    const c = new AbortController();
    void load(c.signal);
    return () => c.abort();
  }, [load]);

  const open = (kind: Exclude<Editor, null>) => {
    setDraft(kind === "edit" ? (text ?? "") : "");
    setEditor(kind);
  };

  const save = async () => {
    if (busy || editor === null || editor === "delete") return;
    setBusy(true);
    try {
      await api.updateArtifactText(id, draft, editor === "add" ? "append" : "replace");
      toast.success(editor === "add" ? "Added to the list" : "List saved");
      setEditor(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not save.");
    } finally {
      setBusy(false);
    }
  };

  const remove = async () => {
    setBusy(true);
    try {
      await api.deleteArtifact(id);
      toast.success("List deleted");
      router.replace("/lists");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not delete the list.");
      setBusy(false);
    }
  };

  const download = () => {
    if (text === null || !meta) return;
    const url = URL.createObjectURL(new Blob([text], { type: "text/markdown" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = meta.filename || "list.md";
    a.click();
    URL.revokeObjectURL(url);
  };

  const runTask = async () => {
    if (!meta || !actionInstruction.trim() || actionBusy) return;
    setActionBusy(true);
    try {
      const goal = `${actionInstruction.trim()} (Referencing list: ${meta.title}, ID: ${meta.id})`;
      const task = await api.createTask({
        goal,
        title: `List task: ${meta.title}`.slice(0, 80),
        manager_domain: "JJ",
      });
      toast.success("Task dispatched to Griffin");
      setActionOpen(false);
      setActionInstruction("");
      router.push(`/tasks/${task.id}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not launch task.");
    } finally {
      setActionBusy(false);
    }
  };

  return (
    <PageContainer>
      <Link href="/lists" className="mb-3 inline-flex min-h-11 items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ChevronLeft className="size-4" /> Lists
      </Link>

      {error && <ErrorNote message={error} />}
      {!meta || text === null ? (
        !error && (
          <div className="space-y-3">
            <Skeleton className="h-10 w-2/3" />
            <Skeleton className="h-64 w-full rounded-2xl" />
          </div>
        )
      ) : (
        <>
          <Kinetic as="h1" text={meta.title} className="font-display text-3xl font-bold leading-[1.05] tracking-[-0.035em] text-balance md:text-5xl" />
          <p className="mt-2 text-sm text-muted-foreground">Started {timeAgo(meta.created_at)}. Each task you save to it adds a dated section.</p>

          <div className="mt-5 flex flex-wrap gap-2">
            <Button className="h-11 gap-2 bg-primary font-medium text-primary-foreground shadow-sm hover:bg-primary/90" onClick={() => setActionOpen(true)}>
              <Sparkles className="size-4" /> Run Task on List
            </Button>
            <Button variant="outline" className="h-11" onClick={() => open("add")}>
              <Plus /> Add entry
            </Button>
            <Button variant="outline" className="h-11" onClick={() => open("edit")}>
              <Pencil /> Edit
            </Button>
            <Button variant="outline" className="h-11" onClick={download}>
              <Download /> Download
            </Button>
            <Button variant="outline" className="h-11 text-destructive" onClick={() => setEditor("delete")}>
              <Trash2 /> Delete
            </Button>
          </div>

          <article className="mt-6 overflow-hidden rounded-2xl border bg-card px-4 py-5 text-[15px] md:px-8 md:py-7">
            <div className="break-words">
              <MarkdownText text={withoutTitleHeading(text, meta.title)} />
            </div>
          </article>
        </>
      )}

      <ResponsiveDialog
        open={editor === "add" || editor === "edit"}
        onOpenChange={(o) => !busy && !o && setEditor(null)}
        title={editor === "edit" ? "Edit list" : "Add an entry"}
        description={editor === "edit" ? "You are editing the whole document. Markdown works, including tables." : "Added to the end of the list. Markdown works, including table rows."}
      >
        <div className="space-y-4">
          <Textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            rows={editor === "edit" ? 14 : 6}
            className="font-mono text-sm"
            aria-label={editor === "edit" ? "List content" : "New entry"}
            placeholder={editor === "add" ? "| Acme | Backend intern | Applied | 6 Nov |" : undefined}
          />
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button variant="outline" className="h-11" disabled={busy} onClick={() => setEditor(null)}>
              Cancel
            </Button>
            <Button className="h-11" disabled={busy || (editor === "add" && !draft.trim())} onClick={save}>
              {busy ? "Saving..." : editor === "edit" ? "Save changes" : "Add to list"}
            </Button>
          </div>
        </div>
      </ResponsiveDialog>

      <ResponsiveDialog open={editor === "delete"} onOpenChange={(o) => !busy && !o && setEditor(null)} title="Delete this list?" description="The list and everything in it is removed. This cannot be undone.">
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button variant="outline" className="h-11" disabled={busy} onClick={() => setEditor(null)}>
            Keep
          </Button>
          <Button variant="destructive" className="h-11" disabled={busy} onClick={remove}>
            {busy ? "Deleting..." : "Delete list"}
          </Button>
        </div>
      </ResponsiveDialog>

      {/* Interactive Task Runner Dialog for this List */}
      <ResponsiveDialog
        open={actionOpen}
        onOpenChange={(o) => !actionBusy && setActionOpen(o)}
        title="Run Task on this List"
        description={meta ? `Ask Griffin to process or update ${meta.title}` : undefined}
      >
        <div className="space-y-4">
          <div className="space-y-2">
            <Label>Suggested Tasks</Label>
            <div className="flex flex-wrap gap-1.5">
              {[
                "Search open internships and add new 30k+ roles to this tracker",
                "Review open roles and tailor resumes with Claude (draft first)",
                "Draft cold outreach messages for alumni in this list",
                "Verify email responses and application statuses",
              ].map((preset) => (
                <button
                  key={preset}
                  type="button"
                  onClick={() => setActionInstruction(preset)}
                  className="rounded-lg border bg-muted/50 px-2.5 py-1 text-xs text-muted-foreground hover:border-primary/40 hover:bg-accent hover:text-foreground text-left"
                >
                  {preset}
                </button>
              ))}
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="list-instruction">Instructions for Griffin</Label>
            <Textarea
              id="list-instruction"
              rows={3}
              placeholder="e.g. Scrape Unstop and LinkedIn for new 30k+ summer SDE internships and update this table..."
              value={actionInstruction}
              onChange={(e) => setActionInstruction(e.target.value)}
              className="text-sm"
            />
          </div>
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button variant="outline" className="h-10" disabled={actionBusy} onClick={() => setActionOpen(false)}>
              Cancel
            </Button>
            <Button className="h-10 gap-1.5" disabled={actionBusy || !actionInstruction.trim()} onClick={() => void runTask()}>
              {actionBusy ? <Loader2 className="size-4 animate-spin" /> : <Sparkles className="size-4" />}
              Launch Task
            </Button>
          </div>
        </div>
      </ResponsiveDialog>
    </PageContainer>
  );
}
