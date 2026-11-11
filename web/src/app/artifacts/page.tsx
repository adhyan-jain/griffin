"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Download, Eye, FileText, Loader2, MoreVertical, Search, Sparkles, Trash2, Upload } from "lucide-react";
import { toast } from "sonner";
import { api, type Artifact } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { EmptyState, ErrorNote, PageContainer, PageHeader, formatBytes, timeAgo } from "@/components/page";
import { ResponsiveDialog } from "@/components/responsive-dialog";
import { MarkdownText } from "@/components/result-view";

export default function FilesPage() {
  const router = useRouter();
  const [files, setFiles] = useState<Artifact[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [filterTab, setFilterTab] = useState<"all" | "trackers" | "resumes" | "outputs">("all");
  const [uploadOpen, setUploadOpen] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [taskId, setTaskId] = useState("");
  const [uploading, setUploading] = useState(false);
  const [toDelete, setToDelete] = useState<Artifact | null>(null);
  const [deleting, setDeleting] = useState(false);

  // Interactive File Preview & Action state
  const [previewFile, setPreviewFile] = useState<Artifact | null>(null);
  const [previewText, setPreviewText] = useState<string | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [actionFile, setActionFile] = useState<Artifact | null>(null);
  const [actionInstruction, setActionInstruction] = useState("");
  const [actionBusy, setActionBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      setFiles(await api.listArtifacts());
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not load files.");
      setFiles((prev) => prev ?? []);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    let base = (files ?? []).filter((f) => f.metadata?.kind !== "vault");
    if (filterTab === "trackers") {
      base = base.filter((f) => f.metadata?.kind === "list" || f.title.toLowerCase().includes("tracker") || f.title.toLowerCase().includes("list"));
    } else if (filterTab === "resumes") {
      base = base.filter((f) => {
        const t = f.title.toLowerCase();
        return t.includes("resume") || t.includes("cv") || t.includes("context") || t.includes("profile");
      });
    } else if (filterTab === "outputs") {
      base = base.filter((f) => Boolean(f.task_id));
    }
    return base.filter((f) => !q || `${f.title} ${f.artifact_type}`.toLowerCase().includes(q));
  }, [files, query, filterTab]);

  const download = async (a: Artifact) => {
    try {
      const { url } = await api.getPresignedUrl(a.id);
      window.open(url, "_blank", "noopener");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not create a download link.");
    }
  };

  const upload = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!file) return;
    setUploading(true);
    try {
      await api.uploadArtifact(file, taskId.trim() || undefined);
      toast.success("File uploaded");
      setUploadOpen(false);
      setFile(null);
      setTaskId("");
      await load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Upload failed.");
    } finally {
      setUploading(false);
    }
  };

  const remove = async () => {
    if (!toDelete) return;
    setDeleting(true);
    try {
      await api.deleteArtifact(toDelete.id);
      toast.success("File deleted");
      setToDelete(null);
      await load();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not delete the file.");
    } finally {
      setDeleting(false);
    }
  };

  const openPreview = async (a: Artifact) => {
    setPreviewFile(a);
    setPreviewText(null);
    setPreviewLoading(true);
    try {
      const text = await api.getArtifactText(a.id);
      setPreviewText(text);
    } catch {
      setPreviewText(null);
    } finally {
      setPreviewLoading(false);
    }
  };

  const runFileAction = async () => {
    if (!actionFile || !actionInstruction.trim() || actionBusy) return;
    setActionBusy(true);
    try {
      const goal = `${actionInstruction.trim()} (Referencing file: ${actionFile.title}, ID: ${actionFile.id})`;
      const task = await api.createTask({
        goal,
        title: `Action on ${actionFile.title}`.slice(0, 80),
        manager_domain: "JJ",
      });
      toast.success("Task dispatched to Griffin");
      setActionFile(null);
      setActionInstruction("");
      router.push(`/tasks/${task.id}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not launch task.");
    } finally {
      setActionBusy(false);
    }
  };

  return (
    <PageContainer className="max-w-3xl">
      <PageHeader
        title="Files"
        description="Everything your tasks have produced, plus anything you add."
        actions={
          <Button className="h-10" onClick={() => setUploadOpen(true)}>
            <Upload /> Upload
          </Button>
        }
      />

      <div className="space-y-4">
        <Tabs value={filterTab} onValueChange={(v) => setFilterTab(v as any)}>
          <TabsList className="h-auto flex-wrap gap-1 bg-transparent p-0">
            {[
              ["all", "All Files"],
              ["trackers", "Trackers & Lists"],
              ["resumes", "Resumes & Career"],
              ["outputs", "Task Outputs"],
            ].map(([v, label]) => (
              <TabsTrigger
                key={v}
                value={v}
                className="h-9 rounded-full border px-3 text-xs data-[state=active]:border-primary data-[state=active]:bg-accent"
              >
                {label}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input type="search" aria-label="Search files" placeholder="Search files by name or type..." className="h-11 pl-9" value={query} onChange={(e) => setQuery(e.target.value)} />
        </div>

        {error && <ErrorNote message={error} />}

        {files === null ? (
          <div className="space-y-2">
            <Skeleton className="h-16 w-full rounded-xl" />
            <Skeleton className="h-16 w-full rounded-xl" />
          </div>
        ) : shown.length === 0 ? (
          <EmptyState
            icon={<FileText />}
            title={files.length ? "Nothing matches" : "No files yet"}
            description={files.length ? "Try a different search or filter." : "Files from your tasks, and ones you upload, show up here."}
          />
        ) : (
          <ul className="divide-y overflow-hidden rounded-xl border bg-card">
            {shown.map((f) => (
              <li key={f.id} className="flex items-center gap-2.5 px-3.5 py-3 sm:gap-3 sm:px-4">
                <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                  <FileText className="size-5" />
                </div>
                <div className="min-w-0 flex-1">
                  <button
                    type="button"
                    onClick={() => void openPreview(f)}
                    className="block truncate text-left text-sm font-medium hover:underline focus:outline-none"
                  >
                    {f.title}
                  </button>
                  <p className="truncate text-xs text-muted-foreground">
                    {[f.artifact_type.toLowerCase(), formatBytes(f.file_size_bytes), timeAgo(f.created_at)].filter(Boolean).join(" · ")}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 gap-1 px-2 text-xs text-muted-foreground hover:text-foreground hidden sm:inline-flex"
                    onClick={() => void openPreview(f)}
                  >
                    <Eye className="size-3.5" /> View
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-8 gap-1 px-2.5 text-xs text-primary border-primary/20 hover:bg-primary/10"
                    onClick={() => {
                      setActionFile(f);
                      setActionInstruction("");
                    }}
                  >
                    <Sparkles className="size-3.5" /> Action
                  </Button>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button variant="ghost" size="icon" className="size-8 shrink-0" aria-label={`Actions for ${f.title}`}>
                        <MoreVertical className="size-4" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem className="min-h-9" onSelect={() => void openPreview(f)}>
                        <Eye /> Quick preview
                      </DropdownMenuItem>
                      <DropdownMenuItem asChild className="min-h-9">
                        <Link href={`/artifacts/${f.id}`}>
                          <FileText /> Open page
                        </Link>
                      </DropdownMenuItem>
                      <DropdownMenuItem className="min-h-9" onSelect={() => void download(f)}>
                        <Download /> Download
                      </DropdownMenuItem>
                      {f.task_id && (
                        <DropdownMenuItem asChild className="min-h-9">
                          <Link href={`/tasks/${f.task_id}`}>
                            <FileText /> View task
                          </Link>
                        </DropdownMenuItem>
                      )}
                      <DropdownMenuItem className="min-h-9 text-destructive focus:text-destructive" onSelect={() => setToDelete(f)}>
                        <Trash2 /> Delete
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>

      {/* Interactive Quick Preview Dialog */}
      <ResponsiveDialog
        open={previewFile !== null}
        onOpenChange={(o) => !o && setPreviewFile(null)}
        title={previewFile?.title ?? "File Preview"}
        description={previewFile ? `${previewFile.artifact_type} · ${formatBytes(previewFile.file_size_bytes)}` : undefined}
      >
        <div className="space-y-4">
          <div className="max-h-[60vh] overflow-y-auto rounded-xl border bg-card p-4 text-sm">
            {previewLoading ? (
              <div className="flex h-32 items-center justify-center text-muted-foreground">
                <Loader2 className="size-6 animate-spin" />
              </div>
            ) : previewText ? (
              <MarkdownText text={previewText} />
            ) : (
              <p className="py-8 text-center text-sm text-muted-foreground">
                Binary or non-text document. Use download to inspect locally.
              </p>
            )}
          </div>
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-between">
            <Button variant="outline" className="h-10" onClick={() => setPreviewFile(null)}>
              Close
            </Button>
            <div className="flex gap-2">
              {previewFile && (
                <Button variant="outline" className="h-10" onClick={() => void download(previewFile)}>
                  <Download className="size-4" /> Download
                </Button>
              )}
              {previewFile && (
                <Button
                  className="h-10 gap-1.5"
                  onClick={() => {
                    const target = previewFile;
                    setPreviewFile(null);
                    setActionFile(target);
                    setActionInstruction("");
                  }}
                >
                  <Sparkles className="size-4" /> Run Action
                </Button>
              )}
            </div>
          </div>
        </div>
      </ResponsiveDialog>

      {/* Interactive File Action Runner Dialog */}
      <ResponsiveDialog
        open={actionFile !== null}
        onOpenChange={(o) => !actionBusy && !o && setActionFile(null)}
        title="Interactive Action with Griffin"
        description={actionFile ? `Ask Griffin to work with ${actionFile.title}` : undefined}
      >
        <div className="space-y-4">
          <div className="space-y-2">
            <Label>Presets for this file</Label>
            <div className="flex flex-wrap gap-1.5">
              {[
                "Tailor resume with Claude for an SDE/AI Intern role",
                "Review against career context and list improvements",
                "Scrape & append open 30k+ internships to tracker",
                "Extract email list and verify domains",
                "Summarize key deliverables and action items",
              ].map((preset) => (
                <button
                  key={preset}
                  type="button"
                  onClick={() => setActionInstruction(preset)}
                  className="rounded-lg border bg-muted/50 px-2.5 py-1 text-xs text-muted-foreground hover:border-primary/40 hover:bg-accent hover:text-foreground"
                >
                  {preset}
                </button>
              ))}
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="instruction">Instructions for Griffin</Label>
            <Textarea
              id="instruction"
              rows={3}
              placeholder="e.g. Tailor this resume with Claude for the Procureyard SDE Intern opening..."
              value={actionInstruction}
              onChange={(e) => setActionInstruction(e.target.value)}
              className="text-sm"
            />
          </div>
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button variant="outline" className="h-10" disabled={actionBusy} onClick={() => setActionFile(null)}>
              Cancel
            </Button>
            <Button className="h-10 gap-1.5" disabled={actionBusy || !actionInstruction.trim()} onClick={() => void runFileAction()}>
              {actionBusy ? <Loader2 className="size-4 animate-spin" /> : <Sparkles className="size-4" />}
              Launch Task
            </Button>
          </div>
        </div>
      </ResponsiveDialog>

      <ResponsiveDialog open={uploadOpen} onOpenChange={setUploadOpen} title="Upload a file" description="Max 5 MB. Attach it to a task if you like.">
        <form onSubmit={upload} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="file">File</Label>
            <Input id="file" type="file" required className="h-11 pt-2" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="task">Task ID (optional)</Label>
            <Input id="task" className="h-11" value={taskId} onChange={(e) => setTaskId(e.target.value)} placeholder="Paste a task ID" />
          </div>
          <Button type="submit" className="h-11 w-full" disabled={!file || uploading}>
            {uploading && <Loader2 className="animate-spin" />} Upload
          </Button>
        </form>
      </ResponsiveDialog>

      <ResponsiveDialog
        open={toDelete !== null}
        onOpenChange={(o) => !o && setToDelete(null)}
        title="Delete this file?"
        description={toDelete ? `${toDelete.title} will be removed from storage. This can't be undone.` : undefined}
      >
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button variant="outline" className="h-11" onClick={() => setToDelete(null)}>
            Keep file
          </Button>
          <Button variant="destructive" className="h-11" onClick={remove} disabled={deleting}>
            {deleting && <Loader2 className="animate-spin" />} Delete
          </Button>
        </div>
      </ResponsiveDialog>
    </PageContainer>
  );
}
