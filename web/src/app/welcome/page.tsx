"use client";

import Link from "next/link";
import { useState } from "react";
import { ArrowRight, CircleCheck, Circle } from "lucide-react";
import { api, type HostView } from "@/lib/api";
import { usePolling } from "@/lib/use-polling";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorNote, PageContainer, PageHeader } from "@/components/page";

type Facts = { hosts: HostView[]; policySaved: boolean; campaigns: number };

type Step = { id: string; title: string; why: string; done: (f: Facts) => boolean; href: string; action: string; hint?: string };

const STEPS: Step[] = [
  {
    id: "laptop",
    title: "Connect your laptop",
    why: "The laptop is where Griffin works your browser and desktop, in the background.",
    done: (f) => f.hosts.some((h) => h.kind === "laptop" && h.live),
    href: "/devices",
    action: "Add the laptop",
    hint: "Create the device, then start the host with the command shown.",
  },
  {
    id: "phone",
    title: "Connect your phone",
    why: "The Android app listens for “hey JJ”, replies to chats, and can use apps for you.",
    done: (f) => f.hosts.some((h) => h.kind === "phone" && h.live),
    href: "/devices",
    action: "See devices",
    hint: "Install the Android app and sign in: the phone registers itself.",
  },
  {
    id: "browser",
    title: "Sign in to your sites once",
    why: "So Griffin can use Gmail, LinkedIn and others as you. It never sees your passwords.",
    done: (f) => f.hosts.some((h) => h.kind === "laptop" && h.effective_capabilities?.includes("browser")),
    href: "/devices",
    action: "How to do it",
    hint: "On the laptop run: griffin-host browser-login. Then set HOST_BROWSER=on and restart the host.",
  },
  {
    id: "permissions",
    title: "Decide what it may do",
    why: "Set, per app and per person, what Griffin may do alone and what it must ask about.",
    done: (f) => f.policySaved,
    href: "/permissions",
    action: "Review permissions",
  },
  {
    id: "campaign",
    title: "Start your first outreach campaign",
    why: "Griffin finds people, drafts the messages, and waits for your yes before anything is sent.",
    done: (f) => f.campaigns > 0,
    href: "/outreach",
    action: "Create a campaign",
  },
];

/** A checklist that ticks itself from real state, so there is never a step to “mark as done”. */
export default function WelcomePage() {
  const [facts, setFacts] = useState<Facts | null>(null);
  const [error, setError] = useState<string | null>(null);

  usePolling(
    async (signal) => {
      const [overview, policy, campaigns] = await Promise.all([
        api.getHosts({ signal }),
        api.getPermissions({ signal }).catch(() => null),
        api.listCampaigns({ signal }).catch(() => []),
      ]);
      setFacts({ hosts: overview.hosts, policySaved: (policy?.version ?? 0) > 0, campaigns: campaigns.length });
      setError(null);
    },
    { intervalMs: 8000, onError: (e) => setError(e instanceof Error ? e.message : "Could not check your setup.") },
  );

  const doneCount = facts ? STEPS.filter((s) => s.done(facts)).length : 0;
  const nextId = facts ? STEPS.find((s) => !s.done(facts))?.id : undefined;

  return (
    <PageContainer className="max-w-2xl">
      <PageHeader title="Get started" description="Five steps to a Griffin that works for you in the background. Each one ticks itself when it is done." />
      {error && <ErrorNote message={error} />}
      {!facts ? (
        <div className="space-y-3">
          <Skeleton className="h-24 w-full rounded-xl" />
          <Skeleton className="h-24 w-full rounded-xl" />
        </div>
      ) : (
        <>
          <div className="mb-5">
            <div className="mb-2 flex justify-between text-sm text-muted-foreground">
              <span>{doneCount === STEPS.length ? "All set" : `${doneCount} of ${STEPS.length} done`}</span>
            </div>
            <Progress value={(doneCount / STEPS.length) * 100} aria-label="Setup progress" />
          </div>
          <ol className="space-y-3">
            {STEPS.map((s) => {
              const done = s.done(facts);
              return (
                <li key={s.id} className={`flex gap-3 rounded-xl border p-4 ${s.id === nextId ? "border-primary bg-primary/5" : "bg-card"}`}>
                  <span className={`mt-0.5 ${done ? "text-primary" : "text-muted-foreground"}`} aria-hidden>
                    {done ? <CircleCheck className="size-5" /> : <Circle className="size-5" />}
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="font-medium">{s.title}</p>
                    <p className="mt-0.5 text-sm text-muted-foreground">{s.why}</p>
                    {!done && s.hint && <p className="mt-1 text-xs text-muted-foreground">{s.hint}</p>}
                    {!done && (
                      <Button asChild size="sm" variant={s.id === nextId ? "default" : "outline"} className="mt-3">
                        <Link href={s.href}>
                          {s.action} <ArrowRight />
                        </Link>
                      </Button>
                    )}
                  </div>
                </li>
              );
            })}
          </ol>
        </>
      )}
    </PageContainer>
  );
}
