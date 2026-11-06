"use client";

import { useState, useSyncExternalStore } from "react";
import { RefreshCw } from "lucide-react";
import { createPoller, type Poller } from "@/lib/poll";
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

const STATUS_POLL_MS = 30000;
type StatusSnapshot = { status: SystemStatus | null; loading: boolean };
const INITIAL: StatusSnapshot = { status: null, loading: true };

// ONE shared loop for every consumer (the shell mounts a dot per breakpoint, Settings mounts the
// list): the first subscriber starts it, the last one stops it. Any failure (including a 404 while
// the endpoint is not deployed) is "unavailable", never a fake green.
let snapshot: StatusSnapshot = INITIAL;
let poller: Poller | null = null;
const listeners = new Set<() => void>();
const emit = (next: StatusSnapshot) => {
  snapshot = next;
  listeners.forEach((l) => l());
};

function subscribe(listener: () => void) {
  listeners.add(listener);
  if (!poller) {
    poller = createPoller({
      intervalMs: STATUS_POLL_MS,
      run: async (signal) => {
        try {
          emit({ status: await api.getSystemStatus({ signal }), loading: false });
        } catch (e) {
          if ((e as { name?: string })?.name !== "AbortError") emit({ status: null, loading: false });
          throw e;
        }
      },
    });
    poller.start();
  }
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) {
      poller?.stop();
      poller = null;
    }
  };
}
const noopSubscribe = () => () => {};
const refreshShared = () => poller?.refresh();

/** Loads GET /system/status through the shared loop. */
export function useSystemStatus(enabled = true) {
  const { status, loading } = useSyncExternalStore(enabled ? subscribe : noopSubscribe, () => snapshot, () => INITIAL);
  return { status, loading, refresh: refreshShared };
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
        className="inline-flex size-9 shrink-0 items-center justify-center rounded-full border bg-card transition-transform active:scale-[0.95]"
      >
        <StatusDot state={overall} className="size-2.5" />
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
