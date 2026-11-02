"use client";

import React, { useEffect, useState, use } from "react";
import Link from "next/link";
import {
  api,
  Task,
  TaskAttempt,
  TaskEvent,
  Artifact,
} from "@/lib/api";
import { StatusBadge, PriorityBadge } from "@/components/StatusBadge";
import {
  ArrowLeft,
  RefreshCw,
  GitBranch,
  Terminal,
  Activity,
  FolderGit2,
  CheckCircle2,
  XCircle,
  Clock,
  Download,
  AlertCircle,
  FileCode,
} from "lucide-react";

export default function TaskDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);

  const [task, setTask] = useState<Task | null>(null);
  const [attempts, setAttempts] = useState<TaskAttempt[]>([]);
  const [events, setEvents] = useState<TaskEvent[]>([]);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<"payloads" | "attempts" | "events" | "artifacts">("payloads");

  const loadTaskData = async () => {
    setLoading(true);
    setError(null);
    try {
      const [t, atts, evts, arts] = await Promise.all([
        api.getTask(id),
        api.getTaskAttempts(id).catch(() => []),
        api.getTaskEvents(id).catch(() => []),
        api.listArtifacts(id).catch(() => []),
      ]);
      setTask(t);
      setAttempts(atts);
      setEvents(evts);
      setArtifacts(arts);
    } catch (err: any) {
      setError(err.message || "Failed to load task details.");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadTaskData();
    const interval = setInterval(loadTaskData, 10000);
    return () => clearInterval(interval);
  }, [id]);

  const handleDownloadArtifact = async (artifactId: string, filename: string) => {
    try {
      const presign = await api.getPresignedUrl(artifactId);
      window.open(presign.download_url, "_blank");
    } catch (err: any) {
      alert(`Download failed: ${err.message}`);
    }
  };

  if (loading && !task) {
    return (
      <div className="max-w-6xl mx-auto px-4 py-16 text-center text-xs text-slate-400">
        <RefreshCw className="w-6 h-6 animate-spin mx-auto text-blue-500 mb-2" />
        <span>Loading task telemetry and execution logs...</span>
      </div>
    );
  }

  if (error || !task) {
    return (
      <div className="max-w-4xl mx-auto px-4 py-12">
        <div className="p-4 rounded-xl bg-rose-950/40 border border-rose-800 text-xs text-rose-300 space-y-2">
          <div className="flex items-center gap-2 font-semibold">
            <AlertCircle className="w-4 h-4" />
            <span>Task not found or access denied</span>
          </div>
          <p>{error}</p>
          <Link href="/tasks" className="text-blue-400 underline block pt-2">
            Return to Task List
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-8 space-y-6">
      {/* Back button & Action Bar */}
      <div className="flex items-center justify-between pb-2">
        <Link
          href="/tasks"
          className="flex items-center gap-1.5 text-xs text-slate-400 hover:text-white transition"
        >
          <ArrowLeft className="w-3.5 h-3.5" />
          <span>Back to Tasks</span>
        </Link>

        <button
          onClick={loadTaskData}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-slate-900 hover:bg-slate-800 text-slate-300 border border-slate-800 transition"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${loading ? "animate-spin" : ""}`} />
          <span>Refresh</span>
        </button>
      </div>

      {/* Task Summary Header */}
      <div className="p-6 rounded-2xl bg-slate-900/60 border border-slate-800 space-y-4">
        <div className="flex flex-wrap items-center gap-2.5">
          <StatusBadge status={task.status} />
          <PriorityBadge priority={task.priority} />
          <span className="px-2 py-0.5 rounded bg-slate-800 text-blue-300 font-mono text-xs border border-slate-700">
            Manager: {task.assigned_manager}
          </span>
          {task.assigned_specialist && (
            <span className="px-2 py-0.5 rounded bg-slate-800/60 text-slate-400 text-xs font-mono">
              Role: {task.assigned_specialist}
            </span>
          )}
          <span className="text-xs text-slate-500 font-mono">Step #{task.step_index}</span>
        </div>

        <div>
          <h1 className="text-xl font-bold text-white tracking-tight leading-snug">
            {task.goal}
          </h1>
          <div className="mt-2 flex flex-wrap items-center gap-x-6 gap-y-1 text-xs text-slate-400 font-mono">
            <span>ID: {task.id}</span>
            <span>Created: {new Date(task.created_at).toLocaleString()}</span>
            <span>Retries: {task.retry_count} / {task.max_retries}</span>
            {task.worker_id && <span>Claimed By: {task.worker_id}</span>}
          </div>
        </div>
      </div>

      {/* Tab Navigation */}
      <div className="flex border-b border-slate-800 space-x-4 text-xs font-medium">
        {[
          { id: "payloads", label: "Payloads & Results", icon: FileCode },
          { id: "attempts", label: `Attempts (${attempts.length})`, icon: Terminal },
          { id: "events", label: `Lifecycle Events (${events.length})`, icon: Activity },
          { id: "artifacts", label: `Artifacts (${artifacts.length})`, icon: FolderGit2 },
        ].map((tab) => {
          const Icon = tab.icon;
          const isActive = activeTab === tab.id;
          return (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id as any)}
              className={`flex items-center gap-2 pb-3 pt-1 border-b-2 transition ${
                isActive
                  ? "border-blue-500 text-white font-semibold"
                  : "border-transparent text-slate-400 hover:text-slate-200"
              }`}
            >
              <Icon className="w-4 h-4" />
              <span>{tab.label}</span>
            </button>
          );
        })}
      </div>

      {/* Tab Content */}
      <div className="min-h-[300px]">
        {/* Payloads Tab */}
        {activeTab === "payloads" && (
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 text-xs">
            <div className="p-4 rounded-xl bg-slate-900/40 border border-slate-800 space-y-2">
              <div className="flex items-center justify-between text-slate-300 font-semibold">
                <span>Input Payload</span>
                <span className="text-[10px] text-slate-500 font-mono">JSON</span>
              </div>
              <pre className="p-3 rounded-lg bg-slate-950 border border-slate-800 font-mono text-[11px] text-slate-300 overflow-x-auto max-h-96">
                {JSON.stringify(task.input_payload || {}, null, 2)}
              </pre>
            </div>

            <div className="p-4 rounded-xl bg-slate-900/40 border border-slate-800 space-y-2">
              <div className="flex items-center justify-between text-slate-300 font-semibold">
                <span>Output Payload / Result</span>
                <span className="text-[10px] text-slate-500 font-mono">JSON</span>
              </div>
              <pre className="p-3 rounded-lg bg-slate-950 border border-slate-800 font-mono text-[11px] text-emerald-300 overflow-x-auto max-h-96">
                {JSON.stringify(task.output_payload || {}, null, 2)}
              </pre>
            </div>
          </div>
        )}

        {/* Attempts Tab */}
        {activeTab === "attempts" && (
          <div className="space-y-4 text-xs">
            {attempts.length === 0 ? (
              <div className="p-8 text-center text-slate-500 rounded-xl bg-slate-900/30 border border-slate-800">
                No worker execution attempts recorded yet.
              </div>
            ) : (
              attempts.map((att) => (
                <div
                  key={att.id}
                  className="p-4 rounded-xl bg-slate-900/40 border border-slate-800 space-y-3 font-mono"
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <span className="font-bold text-white">Attempt #{att.attempt_number}</span>
                      <span className="px-2 py-0.5 rounded bg-slate-800 text-blue-300 text-[10px]">
                        Worker: {att.worker_id}
                      </span>
                    </div>
                    <span
                      className={`px-2 py-0.5 rounded text-[10px] uppercase font-bold ${
                        att.status === "succeeded"
                          ? "bg-emerald-500/20 text-emerald-300"
                          : att.status === "failed"
                          ? "bg-rose-500/20 text-rose-300"
                          : "bg-amber-500/20 text-amber-300"
                      }`}
                    >
                      {att.status}
                    </span>
                  </div>

                  <div className="text-[11px] text-slate-400 space-y-1">
                    <div>Started: {new Date(att.started_at).toLocaleString()}</div>
                    {att.completed_at && <div>Completed: {new Date(att.completed_at).toLocaleString()}</div>}
                    {att.exit_code !== null && att.exit_code !== undefined && (
                      <div>Exit Code: {att.exit_code}</div>
                    )}
                  </div>

                  {att.error_trace && (
                    <div className="p-3 rounded-lg bg-rose-950/40 border border-rose-900/50 text-rose-300 text-[11px] whitespace-pre-wrap overflow-x-auto">
                      {att.error_trace}
                    </div>
                  )}
                </div>
              ))
            )}
          </div>
        )}

        {/* Events Tab */}
        {activeTab === "events" && (
          <div className="space-y-2 text-xs">
            {events.length === 0 ? (
              <div className="p-8 text-center text-slate-500 rounded-xl bg-slate-900/30 border border-slate-800">
                No lifecycle events recorded for this task.
              </div>
            ) : (
              events.map((evt) => (
                <div
                  key={evt.id}
                  className="p-3 rounded-lg bg-slate-900/40 border border-slate-800 flex items-center justify-between gap-4 font-mono text-[11px]"
                >
                  <div className="flex items-center gap-3">
                    <Activity className="w-4 h-4 text-blue-400 shrink-0" />
                    <div>
                      <span className="font-semibold text-white">{evt.event_type}</span>
                      {evt.old_status && evt.new_status && (
                        <span className="text-slate-400 ml-2">
                          ({evt.old_status} → {evt.new_status})
                        </span>
                      )}
                    </div>
                  </div>
                  <span className="text-slate-500 whitespace-nowrap">
                    {new Date(evt.created_at).toLocaleString()}
                  </span>
                </div>
              ))
            )}
          </div>
        )}

        {/* Artifacts Tab */}
        {activeTab === "artifacts" && (
          <div className="space-y-3 text-xs">
            {artifacts.length === 0 ? (
              <div className="p-8 text-center text-slate-500 rounded-xl bg-slate-900/30 border border-slate-800">
                No artifacts generated by this task.
              </div>
            ) : (
              artifacts.map((art) => (
                <div
                  key={art.id}
                  className="p-4 rounded-xl bg-slate-900/40 border border-slate-800 flex flex-col sm:flex-row sm:items-center justify-between gap-4"
                >
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <FolderGit2 className="w-4 h-4 text-blue-400" />
                      <span className="font-bold text-white">{art.filename}</span>
                      <span className="px-2 py-0.5 rounded bg-slate-800 text-slate-300 text-[10px] font-mono">
                        {(art.size_bytes / 1024).toFixed(1)} KB
                      </span>
                    </div>
                    <div className="text-[11px] font-mono text-slate-400">
                      S3 Key: s3://{art.bucket}/{art.object_key}
                    </div>
                    <div className="text-[10px] font-mono text-slate-500 truncate max-w-lg">
                      SHA256: {art.checksum_sha256}
                    </div>
                  </div>

                  <button
                    onClick={() => handleDownloadArtifact(art.id, art.filename)}
                    className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-blue-600 hover:bg-blue-500 text-white font-medium text-xs transition self-start sm:self-center"
                  >
                    <Download className="w-3.5 h-3.5" />
                    <span>Download via S3</span>
                  </button>
                </div>
              ))
            )}
          </div>
        )}
      </div>
    </div>
  );
}
