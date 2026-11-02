"use client";

import React, { useEffect, useState } from "react";
import { api, RunnerStatus } from "@/lib/api";
import { Power, Play, Square, Loader2 } from "lucide-react";

export function RunnerPill() {
  const [status, setStatus] = useState<RunnerStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [actionMessage, setActionMessage] = useState<string | null>(null);

  const refreshStatus = async () => {
    try {
      const data = await api.getRunnerStatus();
      setStatus(data);
    } catch {
      // Runner might be stopped or endpoint unavailable
      setStatus({
        scale_to_zero_enabled: true,
        state: "stopped",
        timestamp: new Date().toISOString(),
      });
    }
  };

  useEffect(() => {
    refreshStatus();
    const interval = setInterval(refreshStatus, 15000);
    return () => clearInterval(interval);
  }, []);

  const handleWake = async () => {
    setLoading(true);
    setActionMessage(null);
    try {
      const res = await api.wakeRunner();
      setActionMessage(res.message);
      await refreshStatus();
    } catch (err: any) {
      setActionMessage(`Wake failed: ${err.message}`);
    } finally {
      setLoading(false);
    }
  };

  const handleSleep = async () => {
    setLoading(true);
    setActionMessage(null);
    try {
      const res = await api.sleepRunner();
      setActionMessage(res.message);
      await refreshStatus();
    } catch (err: any) {
      setActionMessage(`Sleep failed: ${err.message}`);
    } finally {
      setLoading(false);
    }
  };

  const state = status?.state || "stopped";
  const isRunning = state === "running";

  return (
    <div className="flex items-center gap-2 text-xs">
      <div
        className={`flex items-center gap-1.5 px-2.5 py-1 rounded-full border transition-colors ${
          isRunning
            ? "bg-emerald-950/60 text-emerald-300 border-emerald-800"
            : "bg-slate-900 text-slate-400 border-slate-800"
        }`}
        title={`Scale-to-Zero Cloud Runner: ${state}`}
      >
        <span
          className={`h-2 w-2 rounded-full ${
            isRunning ? "bg-emerald-400 animate-pulse" : "bg-slate-500"
          }`}
        />
        <span className="font-medium tracking-wide">
          RUNNER: {state.toUpperCase()}
        </span>
      </div>

      <div className="flex items-center gap-1">
        {isRunning ? (
          <button
            onClick={handleSleep}
            disabled={loading}
            className="flex items-center gap-1 px-2 py-1 rounded bg-slate-800 hover:bg-slate-700 text-slate-300 border border-slate-700 transition"
            title="Trigger EC2 scale-to-zero sleep"
          >
            {loading ? <Loader2 className="w-3 h-3 animate-spin" /> : <Square className="w-3 h-3 text-amber-400" />}
            <span>Sleep</span>
          </button>
        ) : (
          <button
            onClick={handleWake}
            disabled={loading}
            className="flex items-center gap-1 px-2 py-1 rounded bg-blue-900/60 hover:bg-blue-800 text-blue-200 border border-blue-700 transition"
            title="Wake EC2 execution runner"
          >
            {loading ? <Loader2 className="w-3 h-3 animate-spin" /> : <Play className="w-3 h-3 text-emerald-400" />}
            <span>Wake</span>
          </button>
        )}
      </div>

      {actionMessage && (
        <span className="text-[10px] text-slate-400 italic truncate max-w-[150px]">
          {actionMessage}
        </span>
      )}
    </div>
  );
}
