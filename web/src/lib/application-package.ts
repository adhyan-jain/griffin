import type { ApplicationPackage, Listing, PackageBrief, PackageStatus } from "@/lib/api";

/** What the owner reads for each package status: plain words, no jargon. */
export const PACKAGE_STATUS_LABEL: Record<PackageStatus, string> = {
  draft: "Package drafted",
  needs_owner: "Needs your input",
  ready_for_approval: "Waiting for your approval",
  approved: "Package approved",
  rejected: "Package rejected",
};

export type PackageTone = "default" | "secondary" | "outline" | "destructive";

export function packageTone(status: PackageStatus): PackageTone {
  switch (status) {
    case "ready_for_approval":
      return "default";
    case "approved":
      return "secondary";
    case "rejected":
      return "destructive";
    default:
      return "outline";
  }
}

/** The label on a listing's package badge, with what is missing when the package waits on the owner. */
export function packageBadgeLabel(b: PackageBrief): string {
  if (b.status === "needs_owner" && b.missing_count > 0) {
    return `${PACKAGE_STATUS_LABEL.needs_owner}: ${b.missing_count} ${b.missing_count === 1 ? "thing" : "things"}`;
  }
  return PACKAGE_STATUS_LABEL[b.status] ?? b.status;
}

/** The primary action on a listing card: prepare it, prepare it again, or open what exists. null when nothing applies. */
export type PackageAction = { kind: "prepare" | "retry" | "view"; label: string };

export function packageAction(l: Pick<Listing, "stage" | "package">): PackageAction | null {
  if (l.stage === "closed" || l.stage === "confirmed" || l.stage === "submitting") return l.package ? { kind: "view", label: "View package" } : null;
  const p = l.package;
  if (!p) return l.stage === "verified" ? { kind: "prepare", label: "Prepare application" } : null;
  switch (p.status) {
    case "needs_owner":
      return { kind: "retry", label: "Check again" };
    case "rejected":
      return { kind: "retry", label: "Prepare again" };
    default:
      return { kind: "view", label: p.status === "ready_for_approval" ? "Review package" : "View package" };
  }
}

/** The owner can approve or reject only a complete package that is waiting, and only through its approval. */
export function canDecide(p: Pick<ApplicationPackage, "status" | "approval_id">): boolean {
  return p.status === "ready_for_approval" && !!p.approval_id;
}

/** "3 Dec 2026, 5 days left", "closed 2 days ago" or null without a deadline. */
export function deadlineLabel(deadline: string | null | undefined, now: Date = new Date()): string | null {
  if (!deadline) return null;
  const d = new Date(deadline);
  if (Number.isNaN(d.getTime())) return null;
  const day = d.toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric", timeZone: "UTC" });
  const days = Math.round((d.getTime() - now.getTime()) / 86_400_000);
  if (days < 0) return `${day}, passed ${-days} ${-days === 1 ? "day" : "days"} ago`;
  if (days === 0) return `${day}, today`;
  return `${day}, ${days} ${days === 1 ? "day" : "days"} left`;
}

/** The resume's file name, without its folder. */
export function resumeName(path: string): string {
  return path.split("/").pop() || path;
}

/** The package an approval of type application_package is about, from its payload. */
export function packageIdOfApproval(payload: unknown): string | null {
  if (!payload || typeof payload !== "object") return null;
  const id = (payload as { package_id?: unknown }).package_id;
  return typeof id === "string" && id ? id : null;
}

/** What an application_package approval's payload says about itself, for the approvals card. */
export function packageApprovalInfo(payload: unknown): { company: string; role: string; resume: string; projects: string[]; url: string } | null {
  if (!packageIdOfApproval(payload)) return null;
  const p = payload as Record<string, unknown>;
  const str = (v: unknown) => (typeof v === "string" ? v : "");
  return {
    company: str(p.company),
    role: str(p.role),
    resume: str(p.resume_path),
    projects: Array.isArray(p.projects) ? p.projects.filter((x): x is string => typeof x === "string") : [],
    url: str(p.portal_url),
  };
}
