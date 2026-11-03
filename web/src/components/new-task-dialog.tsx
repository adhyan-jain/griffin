"use client";

import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Loader2 } from "lucide-react";
import { api, PRIORITIES, type Manager, type Task, type TaskPriority } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ErrorNote } from "@/components/page";
import { ResponsiveDialog } from "@/components/responsive-dialog";

export function NewTaskDialog({
  open,
  onOpenChange,
  defaultManager = "JJ",
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultManager?: string;
  onCreated?: (task: Task) => void;
}) {
  const [goal, setGoal] = useState("");
  const [manager, setManager] = useState(defaultManager);
  const [priority, setPriority] = useState<TaskPriority>("NORMAL_P2");
  const [managers, setManagers] = useState<Manager[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setManager(defaultManager);
    setError(null);
    api.listManagers().then(setManagers).catch(() => setManagers([]));
  }, [open, defaultManager]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!goal.trim()) return;
    setBusy(true);
    setError(null);
    try {
      const task = await api.createTask({ goal: goal.trim(), manager_domain: manager, priority });
      toast.success("Task created");
      setGoal("");
      setPriority("NORMAL_P2");
      onOpenChange(false);
      onCreated?.(task);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not create the task.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <ResponsiveDialog open={open} onOpenChange={onOpenChange} title="New task" description="Describe the outcome you want.">
      <form onSubmit={submit} className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="goal">What should get done?</Label>
          <Textarea
            id="goal"
            value={goal}
            onChange={(e) => setGoal(e.target.value)}
            placeholder="Summarise the README and list open questions"
            rows={4}
            required
            autoFocus
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="manager">Who handles it</Label>
          <Select value={manager} onValueChange={setManager}>
            <SelectTrigger id="manager" className="h-11 w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="JJ">JJ decides</SelectItem>
              {managers.map((m) => (
                <SelectItem key={m.id} value={m.domain}>
                  {m.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-2">
          <Label id="priority-label">Priority</Label>
          <div role="radiogroup" aria-labelledby="priority-label" className="grid grid-cols-4 gap-2">
            {PRIORITIES.map((p) => (
              <button
                key={p.value}
                type="button"
                role="radio"
                aria-checked={priority === p.value}
                onClick={() => setPriority(p.value)}
                className={cn(
                  "min-h-11 rounded-lg border px-1 text-sm font-medium transition-colors",
                  priority === p.value ? "border-primary bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-muted",
                )}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>

        {error && <ErrorNote message={error} />}

        <Button type="submit" className="h-11 w-full" disabled={busy || !goal.trim()}>
          {busy && <Loader2 className="animate-spin" />}
          Create task
        </Button>
      </form>
    </ResponsiveDialog>
  );
}
