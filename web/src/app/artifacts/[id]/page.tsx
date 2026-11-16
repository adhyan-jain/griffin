"use client";

import { use, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ChevronLeft, Download, ExternalLink, FileText, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, type Artifact } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorNote, PageContainer, formatBytes, timeAgo } from "@/components/page";
import { Kinetic } from "@/components/fx/kinetic";
import { MarkdownText } from "@/components/result-view";
import { ResponsiveDialog } from "@/components/responsive-dialog";

type Kind = "markdown" | "json" | "text" | "image" | "pdf" | "other";

/** How a file can be shown in the app. Anything else is offered as a download. */
function kindOf(a: Artifact): Kind {
  const mt = (a.mime_type ?? "").toLowerCase();
  const fn = (a.filename ?? "").toLowerCase();
  if (a.artifact_type === "MARKDOWN" || mt.includes("markdown") || fn.endsWith(".md")) return "markdown";
  if (a.artifact_type === "JSON" || mt.includes("json") || fn.endsWith(".json")) return "json";
  if (mt.startsWith("image/")) return "image";
  if (mt === "application/pdf" || fn.endsWith(".pdf")) return "pdf";
  if (mt.startsWith("text/") || /\.(txt|csv|tsv|log|ya?ml|xml)$/.test(fn)) return "text";
  return "other";
}

const prettyJSON = (raw: string) => {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
};

export default function FilePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const router = useRouter();
  const [file, setFile] = useState<Artifact | null>(null);
  const [text, setText] = useState<string | null>(null);
  const [url, setUrl] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const c = new AbortController();
    (async () => {
      try {
        const meta = await api.getArtifact(id, { signal: c.signal });
        setFile(meta);
        const k = kindOf(meta);
        if (k === "markdown" || k === "json" || k === "text") setText(await api.getArtifactText(id, { signal: c.signal }));
        if (k === "image" || k === "pdf") setUrl((await api.getPresignedUrl(id)).url);
      } catch (e) {
        if ((e as { name?: string })?.name !== "AbortError") setError(e instanceof Error ? e.message : "Could not open this file.");
      }
    })();
    return () => c.abort();
  }, [id]);

  const download = async () => {
    try {
      const { url: link } = await api.getPresignedUrl(id);
      window.open(link, "_blank", "noopener,noreferrer");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not create a download link.");
    }
  };

  const remove = async () => {
    setBusy(true);
    try {
      await api.deleteArtifact(id);
      toast.success("File deleted");
      router.replace("/artifacts");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not delete the file.");
      setBusy(false);
    }
  };

  const kind = file ? kindOf(file) : null;
  const loadingBody = !error && file !== null && ((kind === "markdown" || kind === "json" || kind === "text") ? text === null : (kind === "image" || kind === "pdf") ? url === null : false);

  return (
    <PageContainer>
      <Link href="/artifacts" className="mb-3 inline-flex min-h-11 items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
        <ChevronLeft className="size-4" /> Files
      </Link>

      {error && <ErrorNote message={error} />}
      {!file ? (
        !error && (
          <div className="space-y-3">
            <Skeleton className="h-10 w-2/3" />
            <Skeleton className="h-64 w-full rounded-2xl" />
          </div>
        )
      ) : (
        <>
          <Kinetic as="h1" text={file.title} className="font-display text-2xl font-bold leading-[1.05] tracking-[-0.035em] text-balance md:text-4xl" />
          <p className="mt-2 text-sm text-muted-foreground">
            {[file.artifact_type.toLowerCase(), formatBytes(file.file_size_bytes), `saved ${timeAgo(file.created_at)}`].filter(Boolean).join(" · ")}
          </p>

          <div className="mt-5 flex flex-wrap gap-2">
            <Button className="h-11" onClick={download}>
              <Download /> Download
            </Button>
            {file.task_id && (
              <Button asChild variant="outline" className="h-11">
                <Link href={`/tasks/${file.task_id}`}>
                  <ExternalLink /> Open the task
                </Link>
              </Button>
            )}
            <Button variant="outline" className="h-11 text-destructive" onClick={() => setConfirm(true)}>
              <Trash2 /> Delete
            </Button>
          </div>

          <article className="mt-6 overflow-hidden rounded-2xl border bg-card">
            {loadingBody ? (
              <div className="space-y-2 p-5">
                <Skeleton className="h-4 w-11/12" />
                <Skeleton className="h-4 w-10/12" />
                <Skeleton className="h-4 w-8/12" />
              </div>
            ) : kind === "markdown" ? (
              text && text.trim() ? (
                <div className="break-words px-4 py-5 text-[15px] md:px-8 md:py-7">
                  <MarkdownText text={text} />
                </div>
              ) : (
                <p className="px-5 py-6 text-sm text-muted-foreground">This file is empty.</p>
              )
            ) : kind === "json" || kind === "text" ? (
              text && text.trim() ? (
                <pre className="whitespace-pre-wrap break-words p-5 font-mono text-[13px] leading-6">{kind === "json" ? prettyJSON(text) : text}</pre>
              ) : (
                <p className="px-5 py-6 text-sm text-muted-foreground">This file is empty.</p>
              )
            ) : kind === "image" && url ? (
              // eslint-disable-next-line @next/next/no-img-element -- a short-lived signed URL, not a static asset next/image could optimise
              <img src={url} alt={file.title} className="mx-auto max-h-[75vh] w-auto" />
            ) : kind === "pdf" && url ? (
              <iframe src={url} title={file.title} className="h-[75vh] w-full" />
            ) : (
              <div className="flex flex-col items-center gap-3 px-6 py-10 text-center">
                <FileText className="size-8 text-muted-foreground" />
                <p className="text-sm text-muted-foreground">This kind of file opens outside the app.</p>
                <Button variant="outline" className="h-11" onClick={download}>
                  <Download /> Download to open it
                </Button>
              </div>
            )}
          </article>
        </>
      )}

      <ResponsiveDialog open={confirm} onOpenChange={(o) => !busy && setConfirm(o)} title="Delete this file?" description="It is removed from Files and from storage. This cannot be undone.">
        <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <Button variant="outline" className="h-11" disabled={busy} onClick={() => setConfirm(false)}>
            Keep
          </Button>
          <Button variant="destructive" className="h-11" disabled={busy} onClick={remove}>
            {busy ? "Deleting..." : "Delete file"}
          </Button>
        </div>
      </ResponsiveDialog>
    </PageContainer>
  );
}
