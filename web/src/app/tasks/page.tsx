"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { api, Task, ManagerDef } from "@/lib/api";
import { StatusBadge, PriorityBadge } from "@/components/StatusBadge";
import {
  GitBranch,
  PlusCircle,
  RefreshCw,
  Search,
  Filter,
  CornerDownRight,
  ArrowRight,
  XCircle,
  X,
  Loader2,
  CheckCircle2,
  Layers,
} from "lucide-react";

export default function TasksPage() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [managers, setManagers] = useState<ManagerDef[]>([]);
  const [loading, setLoading] = useState(true);
  const [statusFilter, setStatusFilter] = useState<string>("all");
  const [searchQuery, setSearchQuery] = useState("");

  // Create Task Modal State
  const [modalOpen, setModalOpen] = useState(false);
  const [newGoal, setNewGoal] = useState("");
  const [newManager, setNewManager] = useState("mgr-code");
  const [newSpecialist, setNewSpecialist] = useState("Go Backend Specialist");
  const [newPriority, setNewPriority] = useState(2);
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);

  const loadData = async () => {
    setLoading(true);
    try {
      const [fetchedTasks, fetchedManagers] = await Promise.all([
        api.listTasks(),
        api.listManagers(),
      ]);
      setTasks(fetchedTasks);
      setManagers(fetchedManagers);
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
    const interval = setInterval(loadData, 12000);
    return () => clearInterval(interval);
  }, []);

  const handleCreateTask = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newGoal.trim()) return;

    setCreating(true);
    setCreateError(null);
    try {
      await api.createTask({
        goal: newGoal.trim(),
        assigned_manager: newManager,
        assigned_specialist: newSpecialist,
        priority: newPriority,
      });
      setModalOpen(false);
      setNewGoal("");
      await loadData();
    } catch (err: any) {
      setCreateError(err.message || "Failed to create task.");
    } finally {
      setCreating(false);
    }
  };

  const handleCancelTask = async (id: string) => {
    if (!confirm("Are you sure you want to cancel this task?")) return;
    try {
      await api.cancelTask(id);
      await loadData();
    } catch (err: any) {
      alert(`Cancel failed: ${err.message}`);
    }
  };

  // Filter tasks
  const filteredTasks = tasks.filter((t) => {
    if (statusFilter !== "all" && t.status !== statusFilter) return false;
    if (searchQuery.trim()) {
      const query = searchQuery.toLowerCase();
      return (
        t.goal.toLowerCase().includes(query) ||
        t.id.toLowerCase().includes(query) ||
        t.assigned_manager.toLowerCase().includes(query)
      );
    }
    return true;
  });

  // Group into roots and children for hierarchical tree view
  const rootTasks = filteredTasks.filter((t) => !t.parent_task_id);
  const childTasksByParent = tasks.reduce<Record<string, Task[]>>((acc, t) => {
    if (t.parent_task_id) {
      acc[t.parent_task_id] = acc[t.parent_task_id] || [];
      acc[t.parent_task_id].push(t);
    }
    return acc;
  }, {});

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8 space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-slate-800">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-white flex items-center gap-2">
            <GitBranch className="w-6 h-6 text-blue-400" />
            <span>Task Engine & DAG Hierarchy</span>
          </h1>
          <p className="text-xs text-slate-400 mt-1">
            Durable task execution tree with dependency resolution and child delegation tracking.
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
          <button
            onClick={() => setModalOpen(true)}
            className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-lg text-xs font-semibold bg-blue-600 hover:bg-blue-500 text-white transition shadow-sm"
          >
            <PlusCircle className="w-3.5 h-3.5" />
            <span>Create Task</span>
          </button>
        </div>
      </div>

      {/* Filter and Search Bar */}
      <div className="flex flex-col sm:flex-row items-center justify-between gap-4 p-4 rounded-xl bg-slate-900/40 border border-slate-800">
        {/* Status Filters */}
        <div className="flex flex-wrap items-center gap-1.5 w-full sm:w-auto">
          {[
            { id: "all", label: "All" },
            { id: "running", label: "Running" },
            { id: "queued", label: "Queued" },
            { id: "waiting_approval", label: "Waiting Approval" },
            { id: "succeeded", label: "Succeeded" },
            { id: "failed", label: "Failed" },
          ].map((tab) => (
            <button
              key={tab.id}
              onClick={() => setStatusFilter(tab.id)}
              className={`px-3 py-1.5 rounded-lg text-xs font-medium transition ${
                statusFilter === tab.id
                  ? "bg-blue-600 text-white font-semibold shadow-sm"
                  : "text-slate-400 hover:text-white hover:bg-slate-800"
              }`}
            >
              {tab.label}
            </button>
          ))}
        </div>

        {/* Search */}
        <div className="relative w-full sm:w-64">
          <Search className="w-3.5 h-3.5 text-slate-500 absolute left-3 top-2.5" />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Search tasks, IDs, managers..."
            className="w-full pl-9 pr-3 py-1.5 rounded-lg bg-slate-950 border border-slate-800 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500"
          />
        </div>
      </div>

      {/* Task List / Hierarchy Tree */}
      <div className="rounded-xl border border-slate-800 bg-slate-900/30 overflow-hidden">
        {filteredTasks.length === 0 ? (
          <div className="p-12 text-center text-xs text-slate-400 space-y-2">
            <Layers className="w-8 h-8 mx-auto text-slate-600 mb-2" />
            <p>No tasks matching the selected filters.</p>
          </div>
        ) : (
          <div className="divide-y divide-slate-800/60 text-xs">
            {rootTasks.map((root) => {
              const children = childTasksByParent[root.id] || [];
              return (
                <div key={root.id} className="p-4 hover:bg-slate-800/20 transition space-y-3">
                  {/* Root Task Header */}
                  <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                    <div className="space-y-1">
                      <div className="flex items-center gap-2">
                        <StatusBadge status={root.status} />
                        <PriorityBadge priority={root.priority} />
                        <span className="text-[10px] font-mono text-slate-500">
                          {root.id.slice(0, 8)}...
                        </span>
                        <span className="px-2 py-0.5 rounded bg-slate-800 text-blue-300 font-mono text-[10px] border border-slate-700">
                          {root.assigned_manager}
                        </span>
                      </div>
                      <Link
                        href={`/tasks/${root.id}`}
                        className="font-medium text-slate-100 hover:text-blue-400 text-sm block"
                      >
                        {root.goal}
                      </Link>
                    </div>

                    <div className="flex items-center gap-2 self-start sm:self-center">
                      {(root.status === "running" || root.status === "queued") && (
                        <button
                          onClick={() => handleCancelTask(root.id)}
                          className="px-2 py-1 rounded bg-rose-950/40 hover:bg-rose-900/60 text-rose-300 border border-rose-800 text-[11px] transition"
                        >
                          Cancel
                        </button>
                      )}
                      <Link
                        href={`/tasks/${root.id}`}
                        className="flex items-center gap-1 px-3 py-1 rounded bg-slate-800 hover:bg-slate-700 text-slate-200 border border-slate-700 text-xs transition"
                      >
                        <span>Details & Logs</span>
                        <ArrowRight className="w-3 h-3" />
                      </Link>
                    </div>
                  </div>

                  {/* Child Tasks Tree Indentation */}
                  {children.length > 0 && (
                    <div className="pl-6 border-l-2 border-slate-800 ml-2 space-y-2 pt-1">
                      <div className="text-[10px] font-medium text-slate-500 uppercase tracking-wider flex items-center gap-1">
                        <CornerDownRight className="w-3 h-3" />
                        <span>Delegated Child Tasks ({children.length})</span>
                      </div>
                      {children.map((child) => (
                        <div
                          key={child.id}
                          className="p-2.5 rounded-lg bg-slate-950/60 border border-slate-800 flex items-center justify-between gap-3 hover:border-slate-700 transition"
                        >
                          <div className="flex items-center gap-2 min-w-0">
                            <CornerDownRight className="w-3.5 h-3.5 text-blue-500 shrink-0" />
                            <StatusBadge status={child.status} />
                            <Link
                              href={`/tasks/${child.id}`}
                              className="font-mono text-slate-200 hover:text-blue-400 truncate max-w-md"
                              title={child.goal}
                            >
                              {child.goal}
                            </Link>
                          </div>
                          <div className="flex items-center gap-2 shrink-0">
                            <span className="text-[10px] font-mono text-slate-400">
                              {child.assigned_manager}
                            </span>
                            <Link
                              href={`/tasks/${child.id}`}
                              className="text-blue-400 hover:text-blue-300 text-xs font-medium"
                            >
                              Inspect
                            </Link>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Create Task Modal */}
      {modalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/75 backdrop-blur-sm">
          <div className="w-full max-w-lg rounded-2xl bg-slate-900 border border-slate-800 p-6 shadow-2xl space-y-4">
            <div className="flex items-center justify-between border-b border-slate-800 pb-3">
              <h2 className="text-base font-bold text-white flex items-center gap-2">
                <PlusCircle className="w-5 h-5 text-blue-400" />
                <span>Create Durable Task</span>
              </h2>
              <button
                onClick={() => setModalOpen(false)}
                className="p-1 rounded-md text-slate-400 hover:text-white hover:bg-slate-800"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            {createError && (
              <div className="p-3 rounded-lg bg-rose-950/40 text-rose-300 border border-rose-800 text-xs">
                {createError}
              </div>
            )}

            <form onSubmit={handleCreateTask} className="space-y-4 text-xs">
              <div>
                <label className="block text-slate-300 font-medium mb-1">Task Goal / Prompt</label>
                <textarea
                  rows={3}
                  value={newGoal}
                  onChange={(e) => setNewGoal(e.target.value)}
                  placeholder="Describe the precise task for the worker..."
                  required
                  className="w-full p-2.5 rounded-lg bg-slate-950 border border-slate-800 text-white placeholder-slate-500 focus:outline-none focus:border-blue-500"
                />
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-slate-300 font-medium mb-1">Assigned Manager</label>
                  <select
                    value={newManager}
                    onChange={(e) => setNewManager(e.target.value)}
                    className="w-full p-2 rounded-lg bg-slate-950 border border-slate-800 text-white focus:outline-none focus:border-blue-500"
                  >
                    <option value="mgr-jj">JJ Orchestrator</option>
                    <option value="mgr-code">Code Manager</option>
                    <option value="mgr-arch">Architecture Manager</option>
                    <option value="mgr-qa">Quality & QA Manager</option>
                    <option value="mgr-sec">Security Manager</option>
                    <option value="mgr-devops">DevOps & Infra Manager</option>
                    <option value="mgr-data">Data & DB Manager</option>
                    <option value="mgr-research">Research Manager</option>
                  </select>
                </div>

                <div>
                  <label className="block text-slate-300 font-medium mb-1">Priority</label>
                  <select
                    value={newPriority}
                    onChange={(e) => setNewPriority(Number(e.target.value))}
                    className="w-full p-2 rounded-lg bg-slate-950 border border-slate-800 text-white focus:outline-none focus:border-blue-500"
                  >
                    <option value={0}>P0 - Critical</option>
                    <option value={1}>P1 - High</option>
                    <option value={2}>P2 - Normal</option>
                    <option value={3}>P3 - Low</option>
                  </select>
                </div>
              </div>

              <div>
                <label className="block text-slate-300 font-medium mb-1">Specialist Role</label>
                <input
                  type="text"
                  value={newSpecialist}
                  onChange={(e) => setNewSpecialist(e.target.value)}
                  placeholder="e.g. Go Backend Specialist"
                  className="w-full p-2 rounded-lg bg-slate-950 border border-slate-800 text-white focus:outline-none focus:border-blue-500"
                />
              </div>

              <div className="pt-2 flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setModalOpen(false)}
                  className="px-4 py-2 rounded-lg text-slate-300 hover:bg-slate-800 transition"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={creating || !newGoal.trim()}
                  className="flex items-center gap-2 px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-500 disabled:opacity-50 text-white font-semibold transition"
                >
                  {creating && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
                  <span>Dispatch Task</span>
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
