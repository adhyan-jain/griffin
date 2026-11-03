"use client";

import { useCallback, useEffect, useState } from "react";
import { RefreshCw } from "lucide-react";
import { api, SYSTEM_COMPONENTS, type ComponentState, type SystemStatus } from "@/lib/api";
import { overallState, STATE_LABEL, type OverallState } from "@/lib/system-status";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { timeAgo } from "@/components/page";

const DOT: Record<OverallState, string> = {
  ok: "bg-success",
  degraded: "bg-warning",
  down: "bg-destructive",
  unknown: "bg-muted-foreground",
  unavailable: "bg-muted-foreground/50",
};
const NAME: Record<(typeof SYSTEM_COMPONENTS)[number], string> = {
  api: "API",
  database: "Database",
  worker: "Worker",
  provider: "AI provider",
  scheduler: "Scheduler",
  storage: "Storage",
  runner: "Runner",
};

/** Loads GET /system/status. Any failure (including 404 while the endpoint is not deployed) is "unavailable". */
export function useSystemStatus(enabled = true, intervalMs = 30000) {
  const [status, setStatus] = useState<SystemStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const refresh = useCallback(async () => {
    try {
      setStatus(await api.getSystemStatus());
    } catch {
      setStatus(null);
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => {
    if (!enabled) return;
    void refresh();
    const t = setInterval(refresh, intervalMs);
    return () => clearInterval(t);
  }, [enabled, intervalMs, refresh]);
  return { status, loading, refresh };
}

export function StatusDot({ state, className }: { state: OverallState | ComponentState; className?: string }) {
  return <span className={cn("inline-block size-2.5 shrink-0 rounded-full", DOT[state], className)} aria-hidden />;
}

export function SystemStatusList({ status, loading, onRefresh }: { status: SystemStatus | null; loading: boolean; onRefresh: () => void }) {
  if (loading) return <p className="text-sm text-muted-foreground" role="status">Checking...</p>;
  if (!status) {
    return (
      <div className="space-y-3">
        <p className="text-sm text-muted-foreground" role="status">Status unavailable. The server did not return component health.</p>
        <Button variant="outline" className="h-11 w-full" onClick={onRefresh}>
          <RefreshCw /> Try again
        </Button>
      </div>
    );
  }
  return (
    <div className="space-y-3">
      <ul className="divide-y rounded-lg border">
        {SYSTEM_COMPONENTS.map((n) => {
          const c = status.components[n];
          const state = c?.state ?? "unknown";
          return (
            <li key={n} className="flex items-start gap-3 px-3 py-2.5 text-sm">
              <StatusDot state={state} className="mt-1.5" />
              <div className="min-w-0 flex-1">
                <p className="font-medium">
                  {NAME[n]} <span className="font-normal text-muted-foreground">{STATE_LABEL[state]}</span>
                </p>
                <p className="break-words text-xs text-muted-foreground">
                  {c?.evidence || "No evidence reported"}
                  {c?.observed_at ? ` / seen ${timeAgo(c.observed_at)}` : " / never observed"}
                </p>
              </div>
            </li>
          );
        })}
      </ul>
      <p className="text-xs text-muted-foreground">Checked {timeAgo(status.generated_at)}</p>
    </div>
  );
}

/** Small header dot that opens the status sheet. */
export function SystemStatusDot() {
  const [open, setOpen] = useState(false);
  const { status, loading, refresh } = useSystemStatus(true);
  const overall = overallState(status);
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label={`System status: ${loading ? "checking" : STATE_LABEL[overall]}`}
        className="relative inline-flex size-11 items-center justify-center rounded-full"
      >
        <StatusDot state={overall} className="size-3 ring-4 ring-background" />
      </button>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent side="bottom" className="max-h-[85dvh] overflow-y-auto rounded-t-2xl pb-[max(1rem,env(safe-area-inset-bottom))]">
          <SheetHeader>
            <SheetTitle>System status</SheetTitle>
            <SheetDescription>{loading ? "Checking" : STATE_LABEL[overall]}</SheetDescription>
          </SheetHeader>
          <div className="px-4 pb-4">
            <SystemStatusList status={status} loading={loading} onRefresh={refresh} />
          </div>
        </SheetContent>
      </Sheet>
    </>
  );
}
