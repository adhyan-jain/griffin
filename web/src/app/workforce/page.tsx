"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { api, ManagerDef } from "@/lib/api";
import {
  Users2,
  Bot,
  Terminal,
  Shield,
  Layers,
  Code2,
  Database,
  Cloud,
  FileCheck2,
  Search,
  Wrench,
  ArrowRight,
  MessageSquare,
  Sparkles,
} from "lucide-react";

export default function WorkforcePage() {
  const [managers, setManagers] = useState<ManagerDef[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    async function fetchManagers() {
      setLoading(false);
      const data = await api.listManagers();
      setManagers(data);
    }
    fetchManagers();
  }, []);

  const getManagerIcon = (id: string) => {
    switch (id) {
      case "mgr-jj":
        return Bot;
      case "mgr-arch":
        return Layers;
      case "mgr-code":
        return Code2;
      case "mgr-qa":
        return FileCheck2;
      case "mgr-sec":
        return Shield;
      case "mgr-devops":
        return Cloud;
      case "mgr-data":
        return Database;
      case "mgr-research":
        return Search;
      default:
        return Wrench;
    }
  };

  return (
    <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8 space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 pb-4 border-b border-slate-800">
        <div>
          <div className="flex items-center gap-2.5">
            <div className="p-2 rounded-xl bg-blue-600/20 text-blue-400 border border-blue-500/30">
              <Users2 className="w-6 h-6" />
            </div>
            <div>
              <h1 className="text-2xl font-bold tracking-tight text-white">
                Workforce Management & Specialists
              </h1>
              <p className="text-xs text-slate-400">
                11 autonomous specialist managers orchestrated by JJ with bounded toolsets and role constraints.
              </p>
            </div>
          </div>
        </div>

        <Link
          href="/chat"
          className="flex items-center gap-1.5 px-3.5 py-1.5 rounded-lg text-xs font-semibold bg-blue-600 hover:bg-blue-500 text-white transition shadow-sm self-start sm:self-center"
        >
          <MessageSquare className="w-3.5 h-3.5" />
          <span>Delegate via JJ</span>
        </Link>
      </div>

      {/* Managers Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {managers.map((mgr) => {
          const Icon = getManagerIcon(mgr.id);
          const isOrchestrator = mgr.id === "mgr-jj";

          return (
            <div
              key={mgr.id}
              className={`p-6 rounded-2xl border transition flex flex-col justify-between space-y-4 ${
                isOrchestrator
                  ? "bg-slate-900/90 border-blue-500/50 shadow-xl shadow-blue-950/20"
                  : "bg-slate-900/40 border-slate-800 hover:border-slate-700"
              }`}
            >
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <div
                    className={`p-2.5 rounded-xl border ${
                      isOrchestrator
                        ? "bg-blue-600 text-white border-blue-400"
                        : "bg-slate-800 text-blue-400 border-slate-700"
                    }`}
                  >
                    <Icon className="w-5 h-5" />
                  </div>
                  <span className="font-mono text-[10px] text-slate-500 uppercase px-2 py-0.5 rounded bg-slate-950 border border-slate-800">
                    {mgr.id}
                  </span>
                </div>

                <div>
                  <h3 className="text-base font-bold text-white tracking-tight">
                    {mgr.name}
                  </h3>
                  <p className="text-xs text-slate-400 mt-1 line-clamp-3 leading-relaxed">
                    {mgr.description}
                  </p>
                </div>

                {/* Specialists */}
                <div className="space-y-1.5 pt-2 border-t border-slate-800/80">
                  <div className="text-[11px] font-semibold text-slate-300">
                    Specialist Personas:
                  </div>
                  <div className="flex flex-wrap gap-1">
                    {mgr.specialists.map((spec, idx) => (
                      <span
                        key={idx}
                        className="px-2 py-0.5 rounded bg-slate-950 text-slate-300 font-mono text-[10px] border border-slate-800"
                      >
                        {spec}
                      </span>
                    ))}
                  </div>
                </div>

                {/* Tools */}
                <div className="space-y-1.5 pt-1">
                  <div className="text-[11px] font-semibold text-slate-300">
                    Allowed Toolsets:
                  </div>
                  <div className="flex flex-wrap gap-1">
                    {mgr.tools.map((t, idx) => (
                      <span
                        key={idx}
                        className="px-2 py-0.5 rounded bg-blue-950/40 text-blue-300 font-mono text-[10px] border border-blue-900/40"
                      >
                        {t}
                      </span>
                    ))}
                  </div>
                </div>
              </div>

              <div className="pt-3 border-t border-slate-800/80">
                <Link
                  href={`/tasks?manager=${mgr.id}`}
                  className="flex items-center justify-between text-xs text-blue-400 hover:text-blue-300 font-medium group"
                >
                  <span>View Manager Tasks</span>
                  <ArrowRight className="w-3.5 h-3.5 group-hover:translate-x-1 transition" />
                </Link>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
