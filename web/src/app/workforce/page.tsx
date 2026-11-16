"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Plus, Users } from "lucide-react";
import { api, type Manager } from "@/lib/api";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState, ErrorNote, PageContainer, PageHeader } from "@/components/page";
import { NewTaskDialog } from "@/components/new-task-dialog";

export default function WorkforcePage() {
  const router = useRouter();
  const [managers, setManagers] = useState<Manager[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [assignTo, setAssignTo] = useState<string | null>(null);

  useEffect(() => {
    api
      .listManagers()
      .then(setManagers)
      .catch((e) => {
        setError(e instanceof Error ? e.message : "Could not load your team.");
        setManagers([]);
      });
  }, []);

  return (
    <PageContainer>
      <PageHeader title="Workforce" description="The managers JJ hands work to, and the specialists under each." />
      {error && <ErrorNote message={error} />}

      {managers === null ? (
        <div className="grid gap-3 md:grid-cols-2">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-40 rounded-xl" />
          ))}
        </div>
      ) : managers.length === 0 ? (
        <EmptyState icon={<Users />} title="No managers yet" description="Managers appear here once they are registered." />
      ) : (
        <ul className="grid gap-3 md:grid-cols-2">
          {managers.map((m) => (
            <li key={m.id} className="flex flex-col gap-3 rounded-xl border bg-card p-4">
              <div>
                <h2 className="font-medium">{m.name}</h2>
                <p className="mt-1 line-clamp-3 text-sm text-muted-foreground">{m.description}</p>
              </div>
              <div className="flex flex-wrap gap-1.5">
                {m.available_workers.slice(0, 3).map((w) => (
                  <Badge key={w} variant="secondary" className="font-normal">
                    {w.replaceAll("_", " ")}
                  </Badge>
                ))}
                {m.available_workers.length > 3 && (
                  <Badge variant="outline" className="font-normal">
                    +{m.available_workers.length - 3}
                  </Badge>
                )}
              </div>
              <Button variant="outline" className="mt-auto h-10 self-start" onClick={() => setAssignTo(m.domain)}>
                <Plus /> Give a task
              </Button>
            </li>
          ))}
        </ul>
      )}

      <NewTaskDialog
        open={assignTo !== null}
        onOpenChange={(o) => !o && setAssignTo(null)}
        defaultManager={assignTo ?? "JJ"}
        onCreated={(t) => router.push(`/tasks/${t.id}`)}
      />
    </PageContainer>
  );
}
