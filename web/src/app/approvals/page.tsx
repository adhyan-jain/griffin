"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { api, Approval } from "@/lib/api";
import {
  ShieldAlert,
  CheckCircle,
  XCircle,
  RefreshCw,
  ArrowRight,
  ShieldCheck,
  AlertTriangle,
  Loader2,
  Clock,
} from "lucide-react";

export default function ApprovalsPage() {
  const [approvals, setApprovals] = useState<Approval[]>([]);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState<"pending" | "all">("pending");
  const [comments, setComments] = useState<Record<string, string>>({});
  const [respondingId, setRespondingId] = useState<string | null>(null);
  const [statusMessage, setStatusMessage] = useState<string | null>(null);

  const loadApprovals = async () => {
    setLoading(true);
    try {
      const data = await api.listApprovals(filter === "pending" ? "pending" : undefined);
      setApprovals(data);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadApprovals();
  }, [filter]);

  const handleRespond = async (id: string, approved: boolean) => {
    setRespondingId(id);
    setStatusMessage(null);
    try {
      const comment = comments[id] || (approved ? "Approved by human operator" : "Rejected by human operator");
      await api.respondApproval(id, approved, comment);
      setStatusMessage(`Approval ${id.slice(0, 8)} successfully ${approved ? "APPROVED" : "REJECTED"}.`);
      await loadApprovals();
    } catch (err: any) {
      alert(`Approval response failed: ${err.message}`);
    } finally {
      setRespondingId(null);
    }
  };

  const pendingCount = approvals.filter((a) => a.status === "pending").length;

  return (
    <div className="max-w-5xl mx-auto px-4 sm:px-6 lg:px-8 py-8 space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-slate-800">
        <div>
          <div className="flex items-center gap-2.5">
            <div className="p-2 rounded-xl bg-purple-600/20 text-purple-400 border border-purple-500/30">
              <ShieldAlert className="w-6 h-6" />
            </div>
            <div>
              <h1 className="text-2xl font-bold tracking-tight text-white">
                Human-in-the-Loop Approvals Inbox
              </h1>
              <p className="text-xs text-slate-400">
                Governance gate for high-risk actions, irreversible migrations, and cloud provisioning.
              </p>
            </div>
          </div>
        </div>

        <div className="flex items-center gap-3">
          <div className="flex bg-slate-900 border border-slate-800 rounded-lg p-0.5 text-xs">
            <button
              onClick={() => setFilter("pending")}
              className={`px-3 py-1 rounded-md font-medium transition ${
                filter === "pending" ? "bg-purple-600 text-white shadow-sm" : "text-slate-400 hover:text-white"
              }`}
            >
              Pending ({pendingCount})
            </button>
            <button
              onClick={() => setFilter("all")}
              className={`px-3 py-1 rounded-md font-medium transition ${
                filter === "all" ? "bg-slate-800 text-white" : "text-slate-400 hover:text-white"
              }`}
            >
              All History
            </button>
          </div>

          <button
            onClick={loadApprovals}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-slate-900 hover:bg-slate-800 text-slate-300 border border-slate-800 transition"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? "animate-spin" : ""}`} />
            <span>Refresh</span>
          </button>
        </div>
      </div>

      {statusMessage && (
        <div className="p-3 rounded-lg bg-emerald-950/40 text-emerald-300 border border-emerald-800 text-xs flex items-center gap-2">
          <CheckCircle className="w-4 h-4 shrink-0" />
          <span>{statusMessage}</span>
        </div>
      )}

      {/* Approvals List */}
      <div className="space-y-4">
        {approvals.length === 0 ? (
          <div className="p-16 rounded-2xl bg-slate-900/30 border border-slate-800 text-center space-y-3">
            <ShieldCheck className="w-10 h-10 text-emerald-400 mx-auto" />
            <h3 className="text-sm font-semibold text-white">All Clear</h3>
            <p className="text-xs text-slate-400 max-w-sm mx-auto">
              No tasks are currently waiting for human operator approval. The workforce is operating within its defined autonomy thresholds.
            </p>
          </div>
        ) : (
          approvals.map((app) => {
            const isPending = app.status === "pending";
            const isProcessing = respondingId === app.id;

            return (
              <div
                key={app.id}
                className={`p-6 rounded-2xl border transition space-y-4 ${
                  isPending
                    ? "bg-slate-900/70 border-purple-900/50 shadow-lg"
                    : "bg-slate-950 border-slate-800/80 opacity-80"
                }`}
              >
                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pb-3 border-b border-slate-800">
                  <div className="flex flex-wrap items-center gap-2 text-xs">
                    <span
                      className={`px-2.5 py-0.5 rounded-full font-mono font-bold border ${
                        app.risk_level === "CRITICAL"
                          ? "bg-red-950 text-red-300 border-red-800"
                          : "bg-amber-950 text-amber-300 border-amber-800"
                      }`}
                    >
                      {app.risk_level || "HIGH RISK"}
                    </span>
                    <span className="px-2 py-0.5 rounded bg-slate-800 text-purple-300 font-mono text-[11px] border border-slate-700">
                      Action: {app.action_type || "EXECUTE_IRREVERSIBLE"}
                    </span>
                    <span className="text-slate-400 text-[11px]">
                      Requested by: <strong className="text-white">{app.requested_by || "Worker"}</strong>
                    </span>
                  </div>

                  <span
                    className={`px-2.5 py-0.5 rounded-full text-xs font-mono font-medium border ${
                      isPending
                        ? "bg-purple-500/20 text-purple-300 border-purple-500/30 animate-pulse"
                        : app.status === "approved"
                        ? "bg-emerald-500/20 text-emerald-300 border-emerald-500/30"
                        : "bg-rose-500/20 text-rose-300 border-rose-500/30"
                    }`}
                  >
                    {app.status.toUpperCase()}
                  </span>
                </div>

                <div className="space-y-2 text-xs">
                  <div className="flex items-center gap-2">
                    <span className="text-slate-400">Target Task:</span>
                    <Link
                      href={`/tasks/${app.task_id}`}
                      className="font-mono text-blue-400 hover:text-blue-300 flex items-center gap-1"
                    >
                      <span>Task {app.task_id}</span>
                      <ArrowRight className="w-3 h-3" />
                    </Link>
                  </div>

                  {app.input_payload && (
                    <div className="space-y-1">
                      <span className="text-slate-400 font-medium">Action Payload & Justification:</span>
                      <pre className="p-3 rounded-lg bg-slate-950 border border-slate-800 font-mono text-[11px] text-slate-300 overflow-x-auto max-h-48">
                        {JSON.stringify(app.input_payload, null, 2)}
                      </pre>
                    </div>
                  )}

                  {app.review_comment && (
                    <div className="text-[11px] text-slate-300 p-2.5 rounded bg-slate-800/40 border border-slate-800">
                      <strong>Operator Note:</strong> {app.review_comment}
                    </div>
                  )}
                </div>

                {/* Operator Decision Actions */}
                {isPending && (
                  <div className="pt-2 flex flex-col sm:flex-row items-center gap-3">
                    <input
                      type="text"
                      placeholder="Optional justification or conditions for decision..."
                      value={comments[app.id] || ""}
                      onChange={(e) =>
                        setComments((prev) => ({ ...prev, [app.id]: e.target.value }))
                      }
                      className="w-full sm:flex-1 p-2 rounded-lg bg-slate-950 border border-slate-800 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-purple-500"
                    />

                    <div className="flex items-center gap-2 w-full sm:w-auto">
                      <button
                        onClick={() => handleRespond(app.id, true)}
                        disabled={isProcessing}
                        className="flex-1 sm:flex-initial flex items-center justify-center gap-1.5 px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 disabled:opacity-50 text-white font-semibold text-xs transition shadow-sm"
                      >
                        {isProcessing ? (
                          <Loader2 className="w-3.5 h-3.5 animate-spin" />
                        ) : (
                          <CheckCircle className="w-3.5 h-3.5" />
                        )}
                        <span>Approve</span>
                      </button>

                      <button
                        onClick={() => handleRespond(app.id, false)}
                        disabled={isProcessing}
                        className="flex-1 sm:flex-initial flex items-center justify-center gap-1.5 px-4 py-2 rounded-lg bg-rose-600 hover:bg-rose-500 disabled:opacity-50 text-white font-semibold text-xs transition shadow-sm"
                      >
                        {isProcessing ? (
                          <Loader2 className="w-3.5 h-3.5 animate-spin" />
                        ) : (
                          <XCircle className="w-3.5 h-3.5" />
                        )}
                        <span>Reject</span>
                      </button>
                    </div>
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}
