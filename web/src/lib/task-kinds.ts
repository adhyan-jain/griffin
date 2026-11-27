import type { Task } from "@/lib/api";

/** What a task row is, so the list can show the owner's own requests first. */
export type TaskKind = "request" | "routine" | "reminder" | "system";

const ROUTINE_WORDS = [
  "TRACKER",
  "ALUMNI DM BATCH",
  "ALUMNI EVIDENCE CHECK",
  "LISTINGS SYNC",
  "OPPORTUNITY ASSESS",
  "OPPORTUNITY SCAN",
  "LISTING DETAILS",
  "APPLICATION PREP",
  "COMMUNITY REPLIES",
  "COMMUNITY SCAN",
  "OPEN-SOURCE WATCH",
  "REPO HYGIENE",
  "DSA COACH",
];
const SYSTEM_PREFIXES = ["ALUMNI DM - ", "ALUMNI OUTREACH DRAFT - ", "APPLICATION PACKAGE - ", "APPLY - ", "SUBMIT - "];

type Payload = { kind?: unknown; cadence?: unknown } | null | undefined;

/** Mirrors Go domain.IsRoutine (internal/domain/task_archive.go) plus the reminder and system rows. */
export function taskKind(t: Pick<Task, "title" | "goal" | "input_payload">): TaskKind {
  const p = t.input_payload as Payload;
  const title = t.title ?? "";
  const upper = title.toUpperCase();
  if (upper.startsWith("REMINDER - ") || p?.kind === "reminder") return "reminder";
  if (typeof p?.cadence === "string" && p.cadence !== "") return "routine";
  // Code writes routine names in capitals ("HACKATHON TRACKER (daily 08:30)"); an owner's own words ("remove it
  // from the internship tracker") are not a routine even when they name one.
  if (p?.kind !== "chat" && ROUTINE_WORDS.some((w) => title.includes(w))) return "routine";
  if (
    SYSTEM_PREFIXES.some((x) => upper.startsWith(x)) ||
    (t.goal ?? "").startsWith("Merged tracker snapshot") ||
    p?.kind === "tracker_snapshot"
  )
    return "system";
  return "request";
}
