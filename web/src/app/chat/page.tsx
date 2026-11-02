"use client";

import React, { useState } from "react";
import Link from "next/link";
import { api, OrchestrationRequestResponse } from "@/lib/api";
import {
  Bot,
  Send,
  Loader2,
  Sparkles,
  GitBranch,
  ArrowRight,
  ShieldCheck,
  CheckCircle2,
  AlertCircle,
  HelpCircle,
} from "lucide-react";

interface ChatMessage {
  id: string;
  sender: "user" | "jj";
  content: string;
  response?: OrchestrationRequestResponse;
  timestamp: string;
}

const SAMPLE_PROMPTS = [
  "Implement automated AWS S3 lifecycle and archival policies for older task artifacts",
  "Audit repository dependencies for CVE security vulnerabilities and unpinned packages",
  "Refactor task engine lease recovery query to eliminate lock contention under 100 concurrent workers",
  "Design a domain model and database migration for multi-tenant webhook event subscriptions",
];

export default function JJChatWorkspacePage() {
  const [messages, setMessages] = useState<ChatMessage[]>([
    {
      id: "welcome",
      sender: "jj",
      content:
        "Hello. I am JJ, top-level orchestrator of the Griffin AI Workforce. Give me an objective, feature request, or operational goal, and I will analyze the intent, select the appropriate specialist manager, formulate an execution DAG, and dispatch persisted durable tasks.",
      timestamp: "Ready",
    },
  ]);
  const [inputGoal, setInputGoal] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (goalToSubmit?: string) => {
    const goal = (goalToSubmit || inputGoal).trim();
    if (!goal || loading) return;

    setError(null);
    const userMsg: ChatMessage = {
      id: `user-${Date.now()}`,
      sender: "user",
      content: goal,
      timestamp: new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
    };

    setMessages((prev) => [...prev, userMsg]);
    setInputGoal("");
    setLoading(true);

    try {
      // Dispatch to JJ Orchestrator API
      let res: OrchestrationRequestResponse;
      try {
        res = await api.submitOrchestratorRequest(goal);
      } catch (orchErr) {
        // Fallback: create task directly via task engine if orchestrator handler returns unpopulated
        const createdTask = await api.createTask({
          goal,
          assigned_manager: "mgr-jj",
          assigned_specialist: "DAG Architect",
          priority: 1,
        });
        res = {
          orchestration_id: createdTask.id,
          status: "dispatched",
          manager: "mgr-jj",
          goal: createdTask.goal,
          created_task_ids: [createdTask.id],
        };
      }

      const jjMsg: ChatMessage = {
        id: `jj-${Date.now()}`,
        sender: "jj",
        content: `Objective received. I have delegated this to the **${res.manager.toUpperCase()}** with priority execution. ${
          res.created_task_ids.length
        } durable task(s) have been generated in the Supabase queue and are ready for worker claiming.`,
        response: res,
        timestamp: new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
      };

      setMessages((prev) => [...prev, jjMsg]);
    } catch (err: any) {
      setError(err.message || "Failed to communicate with JJ Orchestrator.");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="max-w-5xl mx-auto px-4 py-8 flex flex-col h-[calc(100vh-8rem)]">
      {/* Workspace Header */}
      <div className="flex items-center justify-between pb-4 border-b border-slate-800 shrink-0">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-xl bg-blue-600/20 text-blue-400 border border-blue-500/30">
            <Bot className="w-6 h-6" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-lg font-bold text-white tracking-tight">JJ Orchestrator Workspace</h1>
              <span className="px-2 py-0.5 rounded-full text-[10px] font-mono font-medium bg-emerald-500/20 text-emerald-300 border border-emerald-500/30">
                ACTIVE
              </span>
            </div>
            <p className="text-xs text-slate-400">
              Natural Language Intent Formulation • Specialist Delegation • DAG Synthesis
            </p>
          </div>
        </div>

        <Link
          href="/tasks"
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-medium bg-slate-900 hover:bg-slate-800 text-slate-300 border border-slate-800 transition"
        >
          <GitBranch className="w-3.5 h-3.5 text-blue-400" />
          <span>Active Task DAGs</span>
        </Link>
      </div>

      {/* Chat Messages Scroll Container */}
      <div className="flex-1 overflow-y-auto py-6 space-y-4 pr-1">
        {messages.map((msg) => (
          <div
            key={msg.id}
            className={`flex gap-3 max-w-3xl ${
              msg.sender === "user" ? "ml-auto flex-row-reverse" : "mr-auto"
            }`}
          >
            <div
              className={`w-8 h-8 rounded-full flex items-center justify-center shrink-0 text-xs font-bold border ${
                msg.sender === "user"
                  ? "bg-blue-600 text-white border-blue-500"
                  : "bg-slate-900 text-blue-400 border-slate-700"
              }`}
            >
              {msg.sender === "user" ? "OP" : <Bot className="w-4 h-4" />}
            </div>

            <div className="space-y-2 max-w-[85%]">
              <div
                className={`p-4 rounded-2xl text-xs leading-relaxed ${
                  msg.sender === "user"
                    ? "bg-blue-600 text-white rounded-tr-none shadow-md"
                    : "bg-slate-900/90 text-slate-200 border border-slate-800 rounded-tl-none shadow-lg"
                }`}
              >
                <div className="whitespace-pre-wrap">{msg.content}</div>

                {/* Structured Breakdown if provided by JJ */}
                {msg.response && (
                  <div className="mt-3 pt-3 border-t border-slate-800/80 space-y-2 text-xs">
                    <div className="flex items-center gap-2">
                      <span className="text-slate-400 text-[11px]">Assigned Manager:</span>
                      <span className="px-2 py-0.5 rounded bg-slate-800 text-blue-300 font-mono text-[10px] border border-slate-700">
                        {msg.response.manager}
                      </span>
                    </div>

                    {msg.response.created_task_ids && msg.response.created_task_ids.length > 0 && (
                      <div className="space-y-1.5">
                        <span className="text-slate-400 text-[11px]">Generated Tasks in DAG:</span>
                        <div className="flex flex-wrap gap-1.5">
                          {msg.response.created_task_ids.map((tid) => (
                            <Link
                              key={tid}
                              href={`/tasks/${tid}`}
                              className="inline-flex items-center gap-1 px-2.5 py-1 rounded bg-slate-950 hover:bg-slate-800 text-blue-400 border border-slate-800 text-[11px] font-mono transition"
                            >
                              <span>Task {tid.slice(0, 8)}...</span>
                              <ArrowRight className="w-3 h-3" />
                            </Link>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>

              <div
                className={`text-[10px] text-slate-500 ${
                  msg.sender === "user" ? "text-right" : "text-left"
                }`}
              >
                {msg.timestamp}
              </div>
            </div>
          </div>
        ))}

        {loading && (
          <div className="flex gap-3 max-w-xl mr-auto">
            <div className="w-8 h-8 rounded-full bg-slate-900 border border-slate-700 flex items-center justify-center text-blue-400">
              <Bot className="w-4 h-4 animate-spin" />
            </div>
            <div className="p-4 rounded-2xl rounded-tl-none bg-slate-900/90 border border-slate-800 text-xs text-slate-300 flex items-center gap-2">
              <Loader2 className="w-4 h-4 animate-spin text-blue-400" />
              <span>JJ is decomposing the objective and synthesizing workforce tasks...</span>
            </div>
          </div>
        )}

        {error && (
          <div className="p-3 rounded-lg bg-rose-950/40 text-rose-300 border border-rose-800 text-xs flex items-center gap-2">
            <AlertCircle className="w-4 h-4 shrink-0" />
            <span>{error}</span>
          </div>
        )}
      </div>

      {/* Suggested Prompts Banner */}
      <div className="pt-2 pb-3 shrink-0">
        <div className="flex items-center gap-1.5 text-[11px] text-slate-400 mb-1.5">
          <Sparkles className="w-3.5 h-3.5 text-amber-400" />
          <span>Quick Objectives for JJ:</span>
        </div>
        <div className="flex flex-wrap gap-1.5">
          {SAMPLE_PROMPTS.map((p, idx) => (
            <button
              key={idx}
              onClick={() => handleSubmit(p)}
              disabled={loading}
              className="text-[11px] px-2.5 py-1 rounded-md bg-slate-900 hover:bg-slate-800 text-slate-300 border border-slate-800 truncate max-w-sm transition"
            >
              {p}
            </button>
          ))}
        </div>
      </div>

      {/* Prompt Input Form */}
      <div className="pt-2 shrink-0">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            handleSubmit();
          }}
          className="relative flex items-center"
        >
          <input
            type="text"
            value={inputGoal}
            onChange={(e) => setInputGoal(e.target.value)}
            placeholder="Instruct JJ with an objective (e.g., 'Deploy new microservice with S3 backup and QA tests')..."
            disabled={loading}
            className="w-full pl-4 pr-12 py-3 rounded-xl bg-slate-900 border border-slate-800 text-xs text-white placeholder-slate-500 focus:outline-none focus:border-blue-500 shadow-inner"
          />
          <button
            type="submit"
            disabled={loading || !inputGoal.trim()}
            className="absolute right-2 p-2 rounded-lg bg-blue-600 hover:bg-blue-500 disabled:opacity-40 text-white transition shadow-sm"
            title="Dispatch objective to JJ"
          >
            <Send className="w-4 h-4" />
          </button>
        </form>
      </div>
    </div>
  );
}
