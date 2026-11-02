"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import {
  api,
  Task,
  RunnerStatus,
  getCurrentUser,
  UserProfile,
} from "@/lib/api";
import { StatusBadge, PriorityBadge } from "@/components/StatusBadge";
import {
  Activity,
  CheckCircle,
  AlertTriangle,
  Clock,
  ShieldAlert,
  ArrowRight,
  Database,
  Cloud,
  Cpu,
  PlusCircle,
  MessageSquare,
  RefreshCw,
  FolderGit2,
} from "lucide-react";

export default function DashboardPage() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [runner, setRunner] = useState<RunnerStatus | null>(null);
  const [dbReady, setDbReady] = useState<boolean | null>(null);
  const [loading, setLoading] = useState(true);
  const [user, setUser] = useState<UserProfile | null>(null);

  const loadData = async () => {
    setLoading(true);
    setUser(getCurrentUser());

    try {
      const [fetchedTasks, fetchedRunner, readyz] = await Promise.allSettled([
        api.listTasks(),
        api.getRunnerStatus(),
        api.getReadiness(),
      ]);

      if (fetchedTasks.status === "fulfilled") {
        setTasks(fetchedTasks.value);
      }
      if (fetchedRunner.status === "fulfilled") {
        setRunner(fetchedRunner.value);
      }
      if (readyz.status === "fulfilled") {
        setDbReady(readyz.value.database === "connected");
      }
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
    const interval = setInterval(loadData, 10000);
    return () => clearInterval(interval);
  }, []);

  const queuedCount = tasks.filter((t) => t.status === "queued").length;
  const runningCount = tasks.filter((t) => t.status === "running").length;
  const approvalCount = tasks.filter((t) => t.status === "waiting_approval").length;
  const succeededCount = tasks.filter((t) => t.status === "succeeded").length;
  const failedCount = tasks.filter((t) => t.status === "failed" || t.status === "blocked").length;

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8 space-y-8">
      {/* Welcome & Overview Header */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 pb-4 border-b border-slate-800">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white">
            Workforce Executive Dashboard
          </h1>
          <p className="text-xs text-slate-400 mt-1">
            Real-time telemetry for Griffin durable task engine and Antigravity AI execution.
          </p>
        </div>

        <div className="flex items-center gap-3">
          <button
            onClick={loadData}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-slate-900 hover:bg-slate-800 text-slate-300 border border-slate-800 transition"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? "animate-spin" : ""}`} />
            <span>Refresh</span>
          </button>
          <Link
            href="/chat"
            className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-lg text-xs font-semibold bg-blue-600 hover:bg-blue-500 text-white transition shadow-sm"
          >
            <MessageSquare className="w-3.5 h-3.5" />
            <span>Engage JJ</span>
          </Link>
          <Link
            href="/tasks"
            className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-lg text-xs font-semibold bg-slate-800 hover:bg-slate-700 text-white border border-slate-700 transition"
          >
            <PlusCircle className="w-3.5 h-3.5 text-blue-400" />
            <span>New Task</span>
          </Link>
        </div>
      </div>

      {/* Metric Cards */}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-4">
        {/* Active & Running */}
        <div className="p-4 rounded-xl bg-slate-900/60 border border-slate-800 relative overflow-hidden">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-slate-400">In Flight</span>
            <Activity className="w-4 h-4 text-blue-400 animate-pulse" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-2xl font-black text-white">{runningCount}</span>
            <span className="text-[11px] text-slate-500">running</span>
          </div>
          <div className="mt-1 text-[10px] text-slate-400">
            {queuedCount} queued in Supabase
          </div>
          <div className="absolute top-0 right-0 h-1 w-full bg-blue-500" />
        </div>

        {/* Approvals */}
        <Link
          href="/approvals"
          className="p-4 rounded-xl bg-slate-900/60 border border-purple-900/40 hover:border-purple-600/60 transition group relative overflow-hidden"
        >
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-purple-300">Approvals</span>
            <ShieldAlert className="w-4 h-4 text-purple-400 group-hover:scale-110 transition" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-2xl font-black text-purple-200">{approvalCount}</span>
            <span className="text-[11px] text-purple-400">pending</span>
          </div>
          <div className="mt-1 text-[10px] text-purple-400 flex items-center gap-1">
            <span>Human-in-the-loop</span>
            <ArrowRight className="w-2.5 h-2.5" />
          </div>
          <div className="absolute top-0 right-0 h-1 w-full bg-purple-500" />
        </Link>

        {/* Queued */}
        <div className="p-4 rounded-xl bg-slate-900/60 border border-slate-800 relative overflow-hidden">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-slate-400">Backlog</span>
            <Clock className="w-4 h-4 text-amber-400" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-2xl font-black text-amber-200">{queuedCount}</span>
            <span className="text-[11px] text-amber-400/80">awaiting claim</span>
          </div>
          <div className="mt-1 text-[10px] text-slate-400">Lease: 300s FIFO</div>
          <div className="absolute top-0 right-0 h-1 w-full bg-amber-500" />
        </div>

        {/* Succeeded */}
        <div className="p-4 rounded-xl bg-slate-900/60 border border-slate-800 relative overflow-hidden">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-slate-400">Succeeded</span>
            <CheckCircle className="w-4 h-4 text-emerald-400" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-2xl font-black text-emerald-300">{succeededCount}</span>
            <span className="text-[11px] text-slate-500">completed</span>
          </div>
          <div className="mt-1 text-[10px] text-slate-400">Outputs persisted</div>
          <div className="absolute top-0 right-0 h-1 w-full bg-emerald-500" />
        </div>

        {/* Failed / Blocked */}
        <div className="p-4 rounded-xl bg-slate-900/60 border border-slate-800 relative overflow-hidden">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-slate-400">Failed / Blocked</span>
            <AlertTriangle className="w-4 h-4 text-rose-400" />
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-2xl font-black text-rose-300">{failedCount}</span>
            <span className="text-[11px] text-slate-500">issues</span>
          </div>
          <div className="mt-1 text-[10px] text-slate-400">Exponential backoff</div>
          <div className="absolute top-0 right-0 h-1 w-full bg-rose-500" />
        </div>
      </div>

      {/* Cloud Subsystem Topology & Health */}
      <div className="p-5 rounded-xl border border-slate-800 bg-slate-900/40 space-y-4">
        <h2 className="text-xs font-semibold text-slate-300 tracking-wider uppercase">
          Cloud Subsystem Topology & Providers
        </h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 text-xs">
          {/* Supabase DB */}
          <div className="p-3 rounded-lg bg-slate-950 border border-slate-800 flex items-start gap-3">
            <Database className="w-5 h-5 text-emerald-400 shrink-0 mt-0.5" />
            <div className="space-y-0.5 min-w-0">
              <div className="flex items-center gap-1.5">
                <span className="font-semibold text-white">Supabase PostgreSQL</span>
                <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
              </div>
              <p className="text-[11px] text-slate-400 truncate">Source of truth & lease queue</p>
              <div className="text-[10px] font-mono text-emerald-400">
                {dbReady ? "CONNECTED (Port 54322)" : "ONLINE"}
              </div>
            </div>
          </div>

          {/* AWS S3 */}
          <div className="p-3 rounded-lg bg-slate-950 border border-slate-800 flex items-start gap-3">
            <Cloud className="w-5 h-5 text-blue-400 shrink-0 mt-0.5" />
            <div className="space-y-0.5 min-w-0">
              <div className="flex items-center gap-1.5">
                <span className="font-semibold text-white">AWS S3 Artifacts</span>
                <span className="h-1.5 w-1.5 rounded-full bg-blue-400" />
              </div>
              <p className="text-[11px] text-slate-400 truncate">
                griffin-artifacts-220115213279-ap-south-1
              </p>
              <div className="text-[10px] font-mono text-blue-400">ap-south-1 (S3 SDK v2)</div>
            </div>
          </div>

          {/* Scale to Zero Runner */}
          <div className="p-3 rounded-lg bg-slate-950 border border-slate-800 flex items-start gap-3">
            <Cpu className="w-5 h-5 text-indigo-400 shrink-0 mt-0.5" />
            <div className="space-y-0.5 min-w-0">
              <div className="flex items-center gap-1.5">
                <span className="font-semibold text-white">EC2 Scale-to-Zero</span>
                <span
                  className={`h-1.5 w-1.5 rounded-full ${
                    runner?.state === "running" ? "bg-emerald-400 animate-ping" : "bg-slate-500"
                  }`}
                />
              </div>
              <p className="text-[11px] text-slate-400 truncate">
                State: {runner?.state?.toUpperCase() || "STOPPED"}
              </p>
              <div className="text-[10px] font-mono text-indigo-300">
                Idle Shutdown: 600s
              </div>
            </div>
          </div>

          {/* Antigravity CLI */}
          <div className="p-3 rounded-lg bg-slate-950 border border-slate-800 flex items-start gap-3">
            <Activity className="w-5 h-5 text-amber-400 shrink-0 mt-0.5" />
            <div className="space-y-0.5 min-w-0">
              <div className="flex items-center gap-1.5">
                <span className="font-semibold text-white">Antigravity AI Engine</span>
                <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />
              </div>
              <p className="text-[11px] text-slate-400 truncate">agy headless provider</p>
              <div className="text-[10px] font-mono text-amber-400">JSON schema constrained</div>
            </div>
          </div>
        </div>
      </div>

      {/* Recent Tasks Queue Table */}
      <div className="rounded-xl border border-slate-800 bg-slate-900/40 overflow-hidden space-y-0">
        <div className="p-4 border-b border-slate-800 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Clock className="w-4 h-4 text-slate-400" />
            <h2 className="text-sm font-semibold text-white">Recent Task Engine Operations</h2>
          </div>
          <Link
            href="/tasks"
            className="text-xs text-blue-400 hover:text-blue-300 font-medium flex items-center gap-1"
          >
            <span>View All Tasks & DAG Trees</span>
            <ArrowRight className="w-3.5 h-3.5" />
          </Link>
        </div>

        {tasks.length === 0 ? (
          <div className="p-12 text-center space-y-3">
            <div className="w-10 h-10 rounded-full bg-slate-800 text-slate-500 mx-auto flex items-center justify-center">
              <Clock className="w-5 h-5" />
            </div>
            <p className="text-xs text-slate-400">No tasks currently queued or processed.</p>
            <Link
              href="/chat"
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold bg-blue-600 hover:bg-blue-500 text-white transition"
            >
              <MessageSquare className="w-3.5 h-3.5" />
              <span>Prompt JJ to Dispatch First Task</span>
            </Link>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead className="bg-slate-950/80 text-slate-400 border-b border-slate-800 uppercase tracking-wider text-[10px]">
                <tr>
                  <th className="py-2.5 px-4 font-semibold">Status</th>
                  <th className="py-2.5 px-4 font-semibold">Priority</th>
                  <th className="py-2.5 px-4 font-semibold">Task Goal</th>
                  <th className="py-2.5 px-4 font-semibold">Assigned Manager</th>
                  <th className="py-2.5 px-4 font-semibold">Created</th>
                  <th className="py-2.5 px-4 font-semibold text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/60 font-normal">
                {tasks.slice(0, 8).map((task) => (
                  <tr key={task.id} className="hover:bg-slate-800/30 transition">
                    <td className="py-3 px-4">
                      <StatusBadge status={task.status} />
                    </td>
                    <td className="py-3 px-4">
                      <PriorityBadge priority={task.priority} />
                    </td>
                    <td className="py-3 px-4 max-w-md">
                      <Link
                        href={`/tasks/${task.id}`}
                        className="font-medium text-slate-200 hover:text-blue-400 line-clamp-1"
                        title={task.goal}
                      >
                        {task.goal}
                      </Link>
                      <span className="text-[10px] font-mono text-slate-500">
                        {task.id.slice(0, 8)}...
                      </span>
                    </td>
                    <td className="py-3 px-4">
                      <span className="px-2 py-0.5 rounded bg-slate-800 text-slate-300 font-mono text-[11px] border border-slate-700">
                        {task.assigned_manager || "jj-orchestrator"}
                      </span>
                    </td>
                    <td className="py-3 px-4 text-slate-400 text-[11px] whitespace-nowrap">
                      {new Date(task.created_at).toLocaleTimeString([], {
                        hour: "2-digit",
                        minute: "2-digit",
                        second: "2-digit",
                      })}
                    </td>
                    <td className="py-3 px-4 text-right">
                      <Link
                        href={`/tasks/${task.id}`}
                        className="inline-flex items-center gap-1 text-xs text-blue-400 hover:text-blue-300 font-medium"
                      >
                        <span>Details</span>
                        <ArrowRight className="w-3 h-3" />
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
