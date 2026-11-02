"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { api, Artifact } from "@/lib/api";
import {
  FolderGit2,
  Download,
  Trash2,
  Upload,
  RefreshCw,
  Search,
  ExternalLink,
  ShieldCheck,
  HardDrive,
  FileText,
  X,
  Loader2,
  AlertCircle,
  CheckCircle2,
} from "lucide-react";

export default function ArtifactsPage() {
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [loading, setLoading] = useState(true);
  const [searchQuery, setSearchQuery] = useState("");

  // Upload modal state
  const [uploadModalOpen, setUploadModalOpen] = useState(false);
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [uploadTaskId, setUploadTaskId] = useState("");
  const [uploading, setUploading] = useState(false);
  const [feedback, setFeedback] = useState<{ type: "success" | "error"; text: string } | null>(null);

  const loadArtifacts = async () => {
    setLoading(true);
    try {
      const data = await api.listArtifacts();
      setArtifacts(data);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadArtifacts();
  }, []);

  const handleDownload = async (art: Artifact) => {
    try {
      const presign = await api.getPresignedUrl(art.id);
      window.open(presign.download_url, "_blank");
    } catch (err: any) {
      alert(`Download failed: ${err.message}`);
    }
  };

  const handleDelete = async (id: string, name: string) => {
    if (!confirm(`Are you sure you want to delete ${name} from AWS S3?`)) return;
    try {
      await api.deleteArtifact(id);
      setFeedback({ type: "success", text: `Artifact ${name} deleted successfully.` });
      await loadArtifacts();
    } catch (err: any) {
      setFeedback({ type: "error", text: `Delete failed: ${err.message}` });
    }
  };

  const handleUpload = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedFile || !uploadTaskId.trim()) return;

    setUploading(true);
    setFeedback(null);
    try {
      await api.uploadArtifact(uploadTaskId.trim(), selectedFile);
      setFeedback({ type: "success", text: `File "${selectedFile.name}" uploaded to AWS S3 successfully.` });
      setUploadModalOpen(false);
      setSelectedFile(null);
      setUploadTaskId("");
      await loadArtifacts();
    } catch (err: any) {
      setFeedback({ type: "error", text: err.message || "Upload failed." });
    } finally {
      setUploading(false);
    }
  };

  const filteredArtifacts = artifacts.filter((a) => {
    if (!searchQuery.trim()) return true;
    const q = searchQuery.toLowerCase();
    return (
      a.filename.toLowerCase().includes(q) ||
      a.task_id.toLowerCase().includes(q) ||
      a.object_key.toLowerCase().includes(q)
    );
  });

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8 space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-slate-800">
        <div>
          <div className="flex items-center gap-2.5">
            <div className="p-2 rounded-xl bg-blue-600/20 text-blue-400 border border-blue-500/30">
              <FolderGit2 className="w-6 h-6" />
            </div>
            <div>
              <h1 className="text-2xl font-bold tracking-tight text-white">
                AWS S3 Artifacts & File Browser
              </h1>
              <p className="text-xs text-slate-400">
                Task-scoped object storage in <code className="text-blue-300">griffin-artifacts-220115213279-ap-south-1</code> with SHA-256 integrity.
              </p>
            </div>
          </div>
        </div>

        <div className="flex items-center gap-3">
          <button
            onClick={loadArtifacts}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-slate-900 hover:bg-slate-800 text-slate-300 border border-slate-800 transition"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? "animate-spin" : ""}`} />
            <span>Refresh</span>
          </button>
          <button
            onClick={() => setUploadModalOpen(true)}
            className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-lg text-xs font-semibold bg-blue-600 hover:bg-blue-500 text-white transition shadow-sm"
          >
            <Upload className="w-3.5 h-3.5" />
            <span>Upload Artifact</span>
          </button>
        </div>
      </div>

      {feedback && (
        <div
          className={`p-3 rounded-lg text-xs flex items-center gap-2 border ${
            feedback.type === "success"
              ? "bg-emerald-950/40 text-emerald-300 border-emerald-800"
              : "bg-rose-950/40 text-rose-300 border-rose-800"
          }`}
        >
          {feedback.type === "success" ? (
            <CheckCircle2 className="w-4 h-4 shrink-0" />
          ) : (
            <AlertCircle className="w-4 h-4 shrink-0" />
          )}
          <span>{feedback.text}</span>
        </div>
      )}

      {/* S3 Storage Overview Banner */}
      <div className="p-4 rounded-xl bg-slate-900/40 border border-slate-800 flex flex-col md:flex-row md:items-center justify-between gap-4 text-xs">
        <div className="flex items-center gap-3">
          <HardDrive className="w-5 h-5 text-blue-400 shrink-0" />
          <div>
            <div className="font-semibold text-white">Target AWS S3 Storage Provider</div>
            <div className="text-[11px] font-mono text-slate-400">
              s3://griffin-artifacts-220115213279-ap-south-1 (AWS SDK v2 • AES256)
            </div>
          </div>
        </div>

        {/* Search */}
        <div className="relative w-full md:w-72">
          <Search className="w-3.5 h-3.5 text-slate-500 absolute left-3 top-2.5" />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Search filename, key, task ID..."
            className="w-full pl-9 pr-3 py-1.5 rounded-lg bg-slate-950 border border-slate-800 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500"
          />
        </div>
      </div>

      {/* Artifacts Table */}
      <div className="rounded-xl border border-slate-800 bg-slate-900/30 overflow-hidden">
        {filteredArtifacts.length === 0 ? (
          <div className="p-16 text-center text-xs text-slate-500 space-y-2">
            <FolderGit2 className="w-10 h-10 mx-auto text-slate-600 mb-2" />
            <p>No S3 artifacts found matching current filters.</p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead className="bg-slate-950/80 text-slate-400 border-b border-slate-800 uppercase tracking-wider text-[10px]">
                <tr>
                  <th className="py-2.5 px-4 font-semibold">Filename</th>
                  <th className="py-2.5 px-4 font-semibold">Associated Task</th>
                  <th className="py-2.5 px-4 font-semibold">S3 Object Key</th>
                  <th className="py-2.5 px-4 font-semibold">Size</th>
                  <th className="py-2.5 px-4 font-semibold">Checksum</th>
                  <th className="py-2.5 px-4 font-semibold">Uploaded</th>
                  <th className="py-2.5 px-4 font-semibold text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/60 font-mono text-[11px]">
                {filteredArtifacts.map((art) => (
                  <tr key={art.id} className="hover:bg-slate-800/20 transition">
                    <td className="py-3 px-4 font-sans font-semibold text-white">
                      <div className="flex items-center gap-2">
                        <FileText className="w-4 h-4 text-blue-400 shrink-0" />
                        <span className="truncate max-w-[180px]" title={art.filename}>
                          {art.filename}
                        </span>
                      </div>
                    </td>
                    <td className="py-3 px-4">
                      <Link
                        href={`/tasks/${art.task_id}`}
                        className="text-blue-400 hover:text-blue-300"
                      >
                        {art.task_id.slice(0, 8)}...
                      </Link>
                    </td>
                    <td className="py-3 px-4 text-slate-400 truncate max-w-xs" title={art.object_key}>
                      {art.object_key}
                    </td>
                    <td className="py-3 px-4 text-slate-300 whitespace-nowrap">
                      {(art.size_bytes / 1024).toFixed(1)} KB
                    </td>
                    <td className="py-3 px-4 text-slate-500 truncate max-w-[140px]" title={art.checksum_sha256}>
                      {art.checksum_sha256.slice(0, 12)}...
                    </td>
                    <td className="py-3 px-4 text-slate-400 whitespace-nowrap">
                      {new Date(art.created_at).toLocaleDateString()}
                    </td>
                    <td className="py-3 px-4 text-right">
                      <div className="flex items-center justify-end gap-1.5">
                        <button
                          onClick={() => handleDownload(art)}
                          className="p-1.5 rounded-md text-slate-300 hover:text-white hover:bg-slate-800 border border-slate-700 transition"
                          title="Generate Presigned Download URL"
                        >
                          <Download className="w-3.5 h-3.5 text-blue-400" />
                        </button>
                        <button
                          onClick={() => handleDelete(art.id, art.filename)}
                          className="p-1.5 rounded-md text-slate-400 hover:text-rose-400 hover:bg-slate-800 border border-transparent hover:border-slate-800 transition"
                          title="Delete Artifact"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Upload Modal */}
      {uploadModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/75 backdrop-blur-sm">
          <div className="w-full max-w-md rounded-2xl bg-slate-900 border border-slate-800 p-6 shadow-2xl space-y-4">
            <div className="flex items-center justify-between border-b border-slate-800 pb-3">
              <h2 className="text-base font-bold text-white flex items-center gap-2">
                <Upload className="w-5 h-5 text-blue-400" />
                <span>Upload to AWS S3</span>
              </h2>
              <button
                onClick={() => setUploadModalOpen(false)}
                className="p-1 rounded-md text-slate-400 hover:text-white hover:bg-slate-800"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            <form onSubmit={handleUpload} className="space-y-4 text-xs">
              <div>
                <label className="block text-slate-300 font-medium mb-1">
                  Target Task UUID
                </label>
                <input
                  type="text"
                  required
                  placeholder="e.g. 00000000-0000-0000-0000-000000000000"
                  value={uploadTaskId}
                  onChange={(e) => setUploadTaskId(e.target.value)}
                  className="w-full p-2.5 rounded-lg bg-slate-950 border border-slate-800 text-white placeholder-slate-500 focus:outline-none focus:border-blue-500 font-mono text-[11px]"
                />
              </div>

              <div>
                <label className="block text-slate-300 font-medium mb-1">
                  Select File
                </label>
                <input
                  type="file"
                  required
                  onChange={(e) => setSelectedFile(e.target.files?.[0] || null)}
                  className="w-full p-2 rounded-lg bg-slate-950 border border-slate-800 text-slate-300 file:mr-3 file:py-1 file:px-2.5 file:rounded-md file:border-0 file:text-xs file:font-semibold file:bg-blue-600 file:text-white hover:file:bg-blue-500"
                />
              </div>

              <div className="pt-2 flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setUploadModalOpen(false)}
                  className="px-4 py-2 rounded-lg text-slate-300 hover:bg-slate-800 transition"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={uploading || !selectedFile || !uploadTaskId.trim()}
                  className="flex items-center gap-2 px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white font-semibold transition"
                >
                  {uploading && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
                  <span>Upload</span>
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
