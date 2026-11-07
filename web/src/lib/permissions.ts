import type { ActionClass, Policy, PolicyMode, PolicyRule } from "@/lib/api";

export const CLASS_INFO: Record<ActionClass, { label: string; help: string }> = {
  read: { label: "Look at things", help: "Read pages, files and chats." },
  draft: { label: "Prepare", help: "Write drafts and fill forms without sending." },
  send: { label: "Send messages", help: "Messages, emails and posts from your accounts." },
  submit: { label: "Submit forms", help: "Press submit, sign up, confirm." },
  buy: { label: "Buy things", help: "Always needs your approval." },
  delete: { label: "Delete", help: "Remove files, messages or accounts." },
  call: { label: "Make calls", help: "Always needs your approval." },
};

export const CLASS_ORDER: ActionClass[] = ["read", "draft", "send", "submit", "buy", "delete", "call"];

export const MODE_LABEL: Record<PolicyMode, string> = { off: "Never", ask: "Ask me", auto: "Go ahead" };

/** The server never lets these run on their own, so the page does not offer it. */
export const NEVER_AUTO: ActionClass[] = ["buy", "call"];

export const FULL_CHOICES: { hours: number | null; label: string }[] = [
  { hours: null, label: "Off" },
  { hours: 1, label: "1 hour" },
  { hours: 2, label: "2 hours" },
  { hours: 4, label: "4 hours" },
  { hours: 8, label: "8 hours" },
];

const isClassOnly = (r: PolicyRule, c: ActionClass) =>
  r.class === c && !r.device && !r.capability && !r.app && !r.folder && !r.contact;

/** What happens for an action class everywhere: the class-only rule if there is one, else the default. */
export function classMode(p: Policy, c: ActionClass): PolicyMode {
  const rule = p.rules.find((r) => isClassOnly(r, c));
  const mode = rule ? rule.mode : p.default;
  return NEVER_AUTO.includes(c) && mode === "auto" ? "ask" : mode;
}

export function setClassMode(p: Policy, c: ActionClass, mode: PolicyMode): Policy {
  const m = NEVER_AUTO.includes(c) && mode === "auto" ? "ask" : mode;
  const rest = p.rules.filter((r) => !isClassOnly(r, c));
  return { ...p, rules: [...rest, { class: c, mode: m }] };
}

const norm = (s: string) => s.trim().toLowerCase();

// ---- apps: "send from <app>" ----
const isAppSend = (r: PolicyRule) => !!r.app && r.class === "send" && !r.folder && !r.contact && !r.device && !r.capability;
export const appRules = (p: Policy) => p.rules.filter(isAppSend);
export function setAppMode(p: Policy, app: string, mode: PolicyMode): Policy {
  const a = norm(app);
  if (!a) return p;
  return { ...p, rules: [...p.rules.filter((r) => !(isAppSend(r) && r.app === a)), { app: a, class: "send", mode }] };
}

// ---- contacts: "message <contact> without asking" ----
const isContact = (r: PolicyRule) => !!r.contact && r.class === "send" && !r.app && !r.folder && !r.device && !r.capability;
export const contactRules = (p: Policy) => p.rules.filter(isContact);
export function setContactMode(p: Policy, contact: string, mode: PolicyMode): Policy {
  const c = norm(contact);
  if (!c) return p;
  return { ...p, rules: [...p.rules.filter((r) => !(isContact(r) && r.contact === c)), { contact: c, class: "send", mode }] };
}

// ---- folders: read without asking / never touch ----
export type FolderKind = "read" | "never";
const isFolderRule = (r: PolicyRule) => !!r.folder && !r.app && !r.contact && !r.device && !r.capability;
export const folderRules = (p: Policy) => p.rules.filter(isFolderRule);
export const folderKind = (r: PolicyRule): FolderKind => (r.mode === "off" && !r.class ? "never" : "read");

/** Returns an error message for a path the server would refuse, or null. */
export function folderError(raw: string): string | null {
  const f = raw.trim();
  if (!f.startsWith("/")) return "Use a full path starting with /";
  if (f.split("/").includes("..")) return "A path cannot contain ..";
  if (f.replace(/\/+/g, "/") === "/") return "Pick a specific folder, not everything";
  return null;
}

const cleanFolder = (f: string) => f.trim().replace(/\/+/g, "/").replace(/\/$/, "");

export function setFolder(p: Policy, folder: string, kind: FolderKind): Policy {
  const f = cleanFolder(folder);
  const rest = p.rules.filter((r) => !(isFolderRule(r) && r.folder === f));
  const rule: PolicyRule = kind === "never" ? { folder: f, mode: "off" } : { folder: f, class: "read", mode: "auto" };
  return { ...p, rules: [...rest, rule] };
}

export function removeRule(p: Policy, rule: PolicyRule): Policy {
  return { ...p, rules: p.rules.filter((r) => r !== rule && JSON.stringify(r) !== JSON.stringify(rule)) };
}

// ---- Full mode ----
export function fullRemainingMs(p: Policy, now: number = Date.now()): number {
  if (!p.full_until) return 0;
  return Math.max(0, new Date(p.full_until).getTime() - now);
}

export function setFull(p: Policy, hours: number | null, now: number = Date.now()): Policy {
  if (hours == null) {
    const rest = { ...p };
    delete rest.full_until;
    return rest;
  }
  return { ...p, full_until: new Date(now + hours * 3600_000).toISOString() };
}

export function fullLabel(ms: number): string {
  if (ms <= 0) return "Off";
  const mins = Math.ceil(ms / 60000);
  return mins >= 60 ? `On, ${Math.floor(mins / 60)} h ${mins % 60} min left` : `On, ${mins} min left`;
}
