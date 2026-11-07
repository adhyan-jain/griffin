import type { HostCapability, HostKind, HostMode, HostView, Task } from "@/lib/api";

export const CAPABILITY_INFO: Record<HostCapability, { label: string; help: string }> = {
  agent: { label: "Run the assistant", help: "Runs plain steps with the device's own agent login." },
  browser: { label: "Use a browser", help: "Open sites, sign in, fill forms." },
  files: { label: "Read and write files", help: "Only in folders you allow." },
  shell: { label: "Run commands", help: "Terminal commands, inside a sandbox." },
  phone_apps: { label: "Use phone apps", help: "WhatsApp, Instagram and others you enable." },
  sms: { label: "Send texts", help: "Text messages from your number." },
  calls: { label: "Make calls", help: "Always with your approval." },
  messaging: { label: "Send messages", help: "Messages from your accounts." },
};

/** Capabilities the current laptop build can actually back. Offering more would hand it steps it cannot do. */
export const BACKED_BY_BUILD: Record<HostKind, HostCapability[]> = {
  laptop: ["agent"],
  phone: [],
};

export function capabilityLabel(c: string): string {
  return CAPABILITY_INFO[c as HostCapability]?.label ?? c;
}

export function describeMode(mode: HostMode): { title: string; detail: string; tone: "ok" | "warn" | "idle" } {
  switch (mode) {
    case "laptop":
      return { title: "Your laptop is primary", detail: "Work runs on your laptop with its own setup. The cloud runner stays asleep for it.", tone: "ok" };
    case "phone":
      return { title: "Your phone is primary", detail: "Your laptop is off, so the cloud thinks and your phone does the hands-on work.", tone: "ok" };
    default:
      return { title: "No device online", detail: "Plain text steps still run in the cloud. Steps that need a device wait here until one comes online.", tone: "idle" };
  }
}

/** "just now", "3 min ago", "2 h ago", "4 d ago"; never a raw timestamp. */
export function lastSeen(iso: string | null | undefined, now: number = Date.now()): string {
  if (!iso) return "never";
  const secs = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  if (secs < 60) return "just now";
  if (secs < 3600) return `${Math.floor(secs / 60)} min ago`;
  if (secs < 86400) return `${Math.floor(secs / 3600)} h ago`;
  return `${Math.floor(secs / 86400)} d ago`;
}

/** One line describing a device's state for the list. */
export function hostStatus(h: Pick<HostView, "live" | "paused" | "last_seen_at">, now: number = Date.now()): { label: string; tone: "ok" | "warn" | "idle" } {
  if (h.paused) return { label: "Paused", tone: "warn" };
  if (h.live) return { label: "Online", tone: "ok" };
  return { label: `Offline, last seen ${lastSeen(h.last_seen_at, now)}`, tone: "idle" };
}

/** What a device may do once it connects, in words, or a note that it has nothing to do yet. */
export function effectiveSummary(h: Pick<HostView, "capabilities" | "effective_capabilities" | "live">): string {
  if (h.capabilities.length === 0) return "No permissions granted yet";
  const caps = (h.live ? h.effective_capabilities : h.capabilities).map(capabilityLabel);
  return caps.length ? caps.join(", ") : "Connected, but nothing it reports matches what you granted";
}

export function deviceName(kind: HostKind): string {
  return kind === "phone" ? "phone" : "laptop";
}

/** Capabilities a step declares it needs (input_payload.requires), cleaned. */
export function requiredCapabilities(task: Pick<Task, "input_payload">): HostCapability[] {
  const p = task.input_payload as { requires?: unknown } | null | undefined;
  if (!p || !Array.isArray(p.requires)) return [];
  return p.requires.filter((c): c is HostCapability => typeof c === "string" && c in CAPABILITY_INFO);
}

/** The device a waiting step is waiting for, in plain words; null when it is not waiting on one. */
export function waitingForDevice(task: Pick<Task, "status" | "input_payload">): string | null {
  if (task.status !== "QUEUED" && task.status !== "RETRYING") return null;
  const req = requiredCapabilities(task);
  if (req.length === 0) return null;
  const phoneOnly = req.every((c) => c === "phone_apps" || c === "sms" || c === "calls");
  return phoneOnly ? "your phone" : "your laptop";
}

/** The command that starts a laptop host once registered. The token is passed by file, never inline. */
export function laptopSetup(apiUrl: string): string {
  return [
    `mkdir -p ~/.griffin-host && chmod 700 ~/.griffin-host`,
    `# paste the token into this file (it is shown only once), then:`,
    `chmod 600 ~/.griffin-host/token`,
    `GRIFFIN_API_URL=${apiUrl || "https://YOUR-API"} GRIFFIN_HOST_TOKEN_FILE=~/.griffin-host/token ./griffin-host --check`,
  ].join("\n");
}

/** A worker id as a person reads it: a device the owner registered is "your device", never a raw id. */
export function displayWorker(id: string | null | undefined): string | null {
  if (!id) return null;
  return id.startsWith("host:") ? "Your device" : id;
}
