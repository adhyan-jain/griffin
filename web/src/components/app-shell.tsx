"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import dynamic from "next/dynamic";
import { useEffect, useState, type ReactNode } from "react";
import { useTheme } from "next-themes";
import { toast } from "sonner";
import {
  FolderOpen,
  LayoutDashboard,
  ListChecks,
  LogOut,
  MessageSquare,
  Moon,
  MoreHorizontal,
  Notebook,
  Settings,
  ShieldCheck,
  Sun,
  Users,
  type LucideIcon,
} from "lucide-react";
import { api, clearAuthToken, getCurrentUser, resolveAuthToken, type RunnerStatus, type UserProfile } from "@/lib/api";
import { cn } from "@/lib/utils";
import { usePolling } from "@/lib/use-polling";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { SystemStatusDot } from "@/components/system-status";

const DeepField = dynamic(() => import("@/components/fx/deep-field"), { ssr: false });
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet";

type NavItem = { href: string; label: string; icon: LucideIcon };

const PRIMARY: NavItem[] = [
  { href: "/", label: "Home", icon: LayoutDashboard },
  { href: "/chat", label: "Ask JJ", icon: MessageSquare },
  { href: "/tasks", label: "Tasks", icon: ListChecks },
  { href: "/approvals", label: "Approvals", icon: ShieldCheck },
];
const SECONDARY: NavItem[] = [
  { href: "/lists", label: "Lists", icon: Notebook },
  { href: "/artifacts", label: "Files", icon: FolderOpen },
  { href: "/workforce", label: "Workforce", icon: Users },
  { href: "/settings", label: "Settings", icon: Settings },
];

const isActive = (pathname: string, href: string) => (href === "/" ? pathname === "/" : pathname.startsWith(href));

function useShellData(enabled: boolean) {
  const [runner, setRunner] = useState<RunnerStatus | null>(null);
  const [pending, setPending] = useState(0);
  usePolling(
    async (signal) => {
      const [r, a] = await Promise.allSettled([api.getRunnerStatus({ signal }), api.listApprovals("PENDING", { signal })]);
      if (signal.aborted) return;
      if (r.status === "fulfilled") setRunner(r.value);
      if (a.status === "fulfilled") setPending(a.value.length);
      // Both failed: let the poller back off instead of hammering a struggling API.
      if (r.status === "rejected" && a.status === "rejected") throw r.reason;
    },
    { intervalMs: 30000, enabled },
  );
  return { runner, pending };
}

function Brand() {
  return (
    <Link href="/" className="flex items-center gap-2 font-semibold tracking-tight">
      <svg viewBox="0 0 24 24" className="size-6 text-primary" aria-hidden>
        <path d="M12 2 21 7v10l-9 5-9-5V7z" fill="none" stroke="currentColor" strokeWidth="1.6" />
        <path d="M12 7l5 2.8v4.4L12 17l-5-2.8V9.8z" fill="currentColor" />
      </svg>
      Griffin
    </Link>
  );
}

function RunnerPill({ runner }: { runner: RunnerStatus | null }) {
  const [busy, setBusy] = useState(false);
  const state = runner?.state ?? "unknown";
  const dot =
    state === "running" ? "bg-success" : state === "pending" || state === "stopping" ? "bg-warning animate-pulse" : "bg-muted-foreground";
  const label = !runner ? "Runner" : state === "running" ? "On" : state === "stopped" ? "Asleep" : state;
  const canWake = state === "stopped";

  const wake = async () => {
    setBusy(true);
    try {
      await api.wakeRunner();
      toast.success("Waking the runner. It takes about a minute.");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not wake the runner.");
    } finally {
      setBusy(false);
    }
  };

  const body = (
    <>
      <span className={cn("size-2 shrink-0 rounded-full", dot)} aria-hidden />
      <span className="capitalize">
        <span className="hidden sm:inline">Runner </span>
        {label.toLowerCase()}
      </span>
      {canWake && <span className="border-l pl-2 text-primary">Wake</span>}
    </>
  );
  const cls = "inline-flex h-9 shrink-0 items-center gap-2 whitespace-nowrap rounded-full border bg-card px-3 text-xs font-medium";
  return canWake ? (
    <button type="button" onClick={wake} disabled={busy} className={cn(cls, "active:scale-[0.97] transition-transform")}>
      {body}
    </button>
  ) : (
    <div className={cls} role="status">
      {body}
    </div>
  );
}

function ThemeToggle({ className }: { className?: string }) {
  const { resolvedTheme, setTheme } = useTheme();
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  const dark = mounted && resolvedTheme === "dark";
  return (
    <Button
      variant="ghost"
      size="icon"
      className={className}
      aria-label={dark ? "Switch to light theme" : "Switch to dark theme"}
      onClick={() => setTheme(dark ? "light" : "dark")}
    >
      {dark ? <Sun /> : <Moon />}
    </Button>
  );
}

export function AppShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const isLogin = pathname === "/login";
  const [authed, setAuthed] = useState(false);
  const [user, setUser] = useState<UserProfile | null>(null);
  const [moreOpen, setMoreOpen] = useState(false);
  const { runner, pending } = useShellData(authed);

  useEffect(() => {
    if (isLogin) {
      setAuthed(false);
      return;
    }
    let alive = true;
    resolveAuthToken().then((t) => {
      if (!alive) return;
      if (t) {
        setAuthed(true);
        setUser(getCurrentUser());
      } else {
        router.replace("/login");
      }
    });
    return () => {
      alive = false;
    };
  }, [isLogin, pathname, router]);

  const signOut = () => {
    clearAuthToken();
    setAuthed(false);
    setMoreOpen(false);
    router.replace("/login");
  };

  if (isLogin)
    return (
      <div className="relative isolate min-h-dvh">
        <DeepField />
        {children}
      </div>
    );

  if (!authed) {
    return (
      <div className="flex min-h-dvh items-center justify-center" aria-busy="true">
        <Skeleton className="h-8 w-32" />
      </div>
    );
  }

  const badge = (href: string) => (href === "/approvals" && pending > 0 ? pending : 0);

  return (
    <div className="relative isolate min-h-dvh md:flex">
      <DeepField />
      {/* Desktop sidebar */}
      <aside className="sticky top-0 hidden h-dvh w-60 shrink-0 flex-col border-r bg-card/40 p-4 md:flex">
        <div className="mb-6 px-2 pt-1">
          <Brand />
        </div>
        <nav className="flex flex-1 flex-col gap-1" aria-label="Main">
          {[...PRIMARY, ...SECONDARY].map(({ href, label, icon: Icon }) => {
            const active = isActive(pathname, href);
            const n = badge(href);
            return (
              <Link
                key={href}
                href={href}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "flex min-h-10 items-center gap-3 rounded-lg px-3 text-sm font-medium transition-colors",
                  active ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground",
                )}
              >
                <Icon className="size-4" />
                <span className="flex-1">{label}</span>
                {n > 0 && (
                  <span className="rounded-full bg-primary px-1.5 text-xs font-semibold tabular-nums text-primary-foreground">{n}</span>
                )}
              </Link>
            );
          })}
        </nav>
        <div className="space-y-3 border-t pt-4">
          <div className="flex items-center justify-between gap-2">
            <RunnerPill runner={runner} />
            <SystemStatusDot />
          </div>
          <div className="flex items-center justify-between gap-2">
            <span className="truncate text-xs text-muted-foreground" title={user?.email}>
              {user?.email ?? "Signed in"}
            </span>
            <div className="flex shrink-0">
              <ThemeToggle />
              <Button variant="ghost" size="icon" aria-label="Sign out" onClick={signOut}>
                <LogOut />
              </Button>
            </div>
          </div>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        {/* Mobile top bar */}
        <header className="sticky top-0 z-30 flex items-center justify-between border-b bg-background/85 px-4 pb-2 pt-[max(0.5rem,env(safe-area-inset-top))] backdrop-blur md:hidden">
          <Brand />
          <div className="flex items-center gap-1">
            <RunnerPill runner={runner} />
            <SystemStatusDot />
          </div>
        </header>

        <main className="flex-1 pb-[calc(4.5rem+env(safe-area-inset-bottom))] md:pb-0">{children}</main>
      </div>

      {/* Mobile bottom tabs: four primary destinations + More */}
      <nav
        aria-label="Main"
        className="fixed inset-x-0 bottom-0 z-40 grid grid-cols-5 border-t bg-background/92 pb-[env(safe-area-inset-bottom)] backdrop-blur md:hidden"
      >
        {PRIMARY.map(({ href, label, icon: Icon }) => {
          const active = isActive(pathname, href);
          const n = badge(href);
          return (
            <Link
              key={href}
              href={href}
              aria-current={active ? "page" : undefined}
              className={cn(
                "relative flex min-h-14 flex-col items-center justify-center gap-0.5 text-[11px] font-medium transition-colors",
                active ? "text-primary" : "text-muted-foreground",
              )}
            >
              <Icon className="size-5" />
              {label}
              {n > 0 && (
                <span className="absolute right-[calc(50%-1.4rem)] top-1.5 min-w-4 rounded-full bg-primary px-1 text-center text-[10px] font-bold leading-4 tabular-nums text-primary-foreground">
                  {n}
                </span>
              )}
            </Link>
          );
        })}
        <button
          type="button"
          onClick={() => setMoreOpen(true)}
          className={cn(
            "flex min-h-14 flex-col items-center justify-center gap-0.5 text-[11px] font-medium",
            SECONDARY.some((s) => isActive(pathname, s.href)) ? "text-primary" : "text-muted-foreground",
          )}
        >
          <MoreHorizontal className="size-5" />
          More
        </button>
      </nav>

      <Sheet open={moreOpen} onOpenChange={setMoreOpen}>
        <SheetContent side="bottom" className="rounded-t-2xl px-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
          <SheetHeader className="px-0">
            <SheetTitle>More</SheetTitle>
            <SheetDescription className="truncate">{user?.email ?? "Signed in"}</SheetDescription>
          </SheetHeader>
          <div className="flex flex-col gap-1">
            {SECONDARY.map(({ href, label, icon: Icon }) => (
              <Link
                key={href}
                href={href}
                onClick={() => setMoreOpen(false)}
                className="flex min-h-12 items-center gap-3 rounded-lg px-3 text-base font-medium active:bg-muted"
              >
                <Icon className="size-5 text-muted-foreground" />
                {label}
              </Link>
            ))}
            <button
              type="button"
              onClick={signOut}
              className="flex min-h-12 items-center gap-3 rounded-lg px-3 text-base font-medium text-destructive active:bg-muted"
            >
              <LogOut className="size-5" />
              Sign out
            </button>
          </div>
        </SheetContent>
      </Sheet>
    </div>
  );
}
