import React from "react";

export function StatusBadge({ status }: { status: string }) {
  const normalized = status.toLowerCase();

  const getStyle = () => {
    switch (normalized) {
      case "succeeded":
      case "completed":
        return "bg-emerald-500/15 text-emerald-400 border-emerald-500/30";
      case "running":
        return "bg-blue-500/15 text-blue-400 border-blue-500/30 animate-pulse";
      case "queued":
        return "bg-amber-500/15 text-amber-400 border-amber-500/30";
      case "waiting_approval":
        return "bg-purple-500/15 text-purple-400 border-purple-500/30 font-semibold";
      case "failed":
        return "bg-rose-500/15 text-rose-400 border-rose-500/30";
      case "blocked":
        return "bg-slate-700/40 text-slate-400 border-slate-600/30";
      case "cancelled":
        return "bg-neutral-800 text-neutral-400 border-neutral-700";
      default:
        return "bg-slate-800 text-slate-300 border-slate-700";
    }
  };

  const getLabel = () => {
    switch (normalized) {
      case "waiting_approval":
        return "Needs Approval";
      default:
        return normalized.toUpperCase();
    }
  };

  return (
    <span
      className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-mono font-medium border ${getStyle()}`}
    >
      {getLabel()}
    </span>
  );
}

export function PriorityBadge({ priority }: { priority: number }) {
  switch (priority) {
    case 0:
      return (
        <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-bold bg-red-950 text-red-400 border border-red-800">
          P0 CRITICAL
        </span>
      );
    case 1:
      return (
        <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-semibold bg-amber-950 text-amber-300 border border-amber-800">
          P1 HIGH
        </span>
      );
    case 2:
      return (
        <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-slate-800 text-slate-300 border border-slate-700">
          P2 NORMAL
        </span>
      );
    default:
      return (
        <span className="inline-flex items-center px-2 py-0.5 rounded text-xs text-slate-400 bg-slate-900 border border-slate-800">
          P3 LOW
        </span>
      );
  }
}
