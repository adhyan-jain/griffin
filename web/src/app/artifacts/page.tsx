"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { Download, FileText, Loader2, MoreVertical, Search, Trash2, Upload } from "lucide-react";
import { toast } from "sonner";
import { api, type Artifact } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, ErrorNote, PageContainer, PageHeader, formatBytes, timeAgo } from "@/components/page";
import { ResponsiveDialog } from "@/components/responsive-dialog";

export default function FilesPage() {
  const [files, setFiles] = useState<Artifact[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [uploadOpen, setUploadOpen] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [taskId, setTaskId] = useState("");
  const [uploading, setUploading] = useState(false);
  const [toDelete, setToDelete] = useState<Artifact | null>(null);
  const [deleting, setDeleting] = useState(false);

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
    return (files ?? []).filter((f) => !q || `${f.title} ${f.artifact_type}`.toLowerCase().includes(q));
  }, [files, query]);

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

      <div className="space-y-3">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input type="search" aria-label="Search files" placeholder="Search files" className="h-11 pl-9" value={query} onChange={(e) => setQuery(e.target.value)} />
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
            description={files.length ? "Try a different search." : "Files from your tasks, and ones you upload, show up here."}
          />
        ) : (
          <ul className="divide-y overflow-hidden rounded-xl border bg-card">
            {shown.map((f) => (
              <li key={f.id} className="flex items-center gap-3 px-4 py-3">
                <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                  <FileText className="size-5" />
                </div>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{f.title}</p>
                  <p className="truncate text-xs text-muted-foreground">
                    {[f.artifact_type.toLowerCase(), formatBytes(f.file_size_bytes), timeAgo(f.created_at)].filter(Boolean).join(" · ")}
                  </p>
                </div>
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="ghost" size="icon" className="size-10 shrink-0" aria-label={`Actions for ${f.title}`}>
                      <MoreVertical />
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuItem className="min-h-10" onSelect={() => void download(f)}>
                      <Download /> Download
                    </DropdownMenuItem>
                    {f.task_id && (
                      <DropdownMenuItem asChild className="min-h-10">
                        <Link href={`/tasks/${f.task_id}`}>
                          <FileText /> View task
                        </Link>
                      </DropdownMenuItem>
                    )}
                    <DropdownMenuItem className="min-h-10 text-destructive focus:text-destructive" onSelect={() => setToDelete(f)}>
                      <Trash2 /> Delete
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </li>
            ))}
          </ul>
        )}
      </div>

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
