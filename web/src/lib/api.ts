/**
 * Griffin API client. Types mirror the Go backend (internal/domain) exactly, so pages consume
 * server payloads directly with no adapter layer.
 */

import { getSupabase, getSupabaseAccessToken } from "@/lib/supabase";

// Production builds must set NEXT_PUBLIC_API_URL; the localhost fallback exists for `next dev` only.
const API_BASE_URL =
  process.env.NEXT_PUBLIC_API_URL ||
  (process.env.NODE_ENV === "production" ? "" : "http://localhost:8080");

/**
 * Development-only login (POST /api/v1/auth/login). The flag is inlined at build time and is
 * absent from production builds, so the dev login UI and call path are dead code there. The
 * backend additionally refuses to register that route in production.
 */
export const DEV_LOGIN_ENABLED = process.env.NEXT_PUBLIC_ENABLE_DEV_LOGIN === "true";

// ---------------------------------------------------------------------------
// Domain types (backend wire format)
// ---------------------------------------------------------------------------

export type TaskStatus =
  | "CREATED"
  | "ROUTED"
  | "QUEUED"
  | "RUNNING"
  | "WAITING_FOR_DEPENDENCY"
  | "VERIFYING"
  | "AWAITING_APPROVAL"
  | "RETRYING"
  | "COMPLETED"
  | "FAILED"
  | "CANCELLED";

export type TaskPriority = "CRITICAL_P0" | "HIGH_P1" | "NORMAL_P2" | "BACKGROUND_P3";

export const PRIORITIES: { value: TaskPriority; label: string }[] = [
  { value: "CRITICAL_P0", label: "Critical" },
  { value: "HIGH_P1", label: "High" },
  { value: "NORMAL_P2", label: "Normal" },
  { value: "BACKGROUND_P3", label: "Low" },
];

export interface Task {
  id: string;
  user_id: string;
  workspace_id?: string | null;
  parent_id?: string | null;
  root_id?: string | null;
  manager_domain: string;
  title: string;
  goal: string;
  input_payload?: unknown;
  output_payload?: unknown;
  status: TaskStatus;
  priority: TaskPriority;
  /** The worker (or "JJ_ORCHESTRATOR" for a coordinating parent) holding the task's lease. */
  leased_by?: string | null;
  retry_count: number;
  max_retries: number;
  failure_reason?: string | null;
  scheduled_for?: string | null;
  started_at?: string | null;
  completed_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface TaskAttempt {
  id: string;
  task_id: string;
  worker_id: string;
  attempt_number: number;
  status: string;
  prompt_tokens: number;
  completion_tokens: number;
  duration_ms: number;
  error_message?: string | null;
  started_at: string;
  completed_at?: string | null;
}

export interface TaskEvent {
  id: number;
  task_id: string;
  event_type: string;
  message: string;
  metadata?: unknown;
  created_at: string;
}

export type ApprovalStatus = "PENDING" | "APPROVED" | "REJECTED" | "EXPIRED";

export interface Approval {
  id: string;
  task_id: string;
  action_type: string;
  risk_level: "LOW" | "MEDIUM" | "HIGH" | "CRITICAL";
  summary: string;
  action_payload?: unknown;
  status: ApprovalStatus;
  decision_reason?: string | null;
  decided_at?: string | null;
  expires_at?: string | null;
  created_at: string;
}

export type OutreachStatus = "found" | "drafted" | "approved" | "sent" | "replied" | "followup_due" | "closed";

export interface OutreachLimits {
  daily_cap: number;
  min_gap_seconds: number;
  jitter_seconds: number;
  quiet_start_hour: number;
  quiet_end_hour: number;
  followup_after_day: number;
  max_followups: number;
}

export interface OutreachCampaign {
  id: string;
  name: string;
  goal: string;
  channel: "email" | "linkedin" | "whatsapp" | "instagram" | "other";
  status: "active" | "paused" | "done";
  limits: OutreachLimits;
  counts: Partial<Record<OutreachStatus, number>>;
  created_at: string;
}

export interface OutreachTarget {
  id: string;
  campaign_id: string;
  name: string;
  handle: string;
  info?: string;
  status: OutreachStatus;
  draft?: string;
  sent_at?: string | null;
  replied_at?: string | null;
  reply_text?: string;
  next_followup_at?: string | null;
  followups_sent: number;
  notes?: string;
  updated_at: string;
}

export interface Artifact {
  id: string;
  task_id?: string | null;
  title: string;
  artifact_type: string;
  storage_provider: string;
  filename?: string | null;
  file_size_bytes?: number | null;
  mime_type?: string | null;
  /** Free-form tags; `{ kind: "list" }` marks a living list. */
  metadata?: { kind?: string } | null;
  updated_at?: string;
  created_at: string;
}

export interface Manager {
  id: string;
  name: string;
  description: string;
  domain: string;
  responsibilities: string[];
  supported_task_types: string[];
  allowed_capabilities: string[];
  available_workers: string[];
}

export interface RunnerStatus {
  scale_to_zero_enabled: boolean;
  state: "running" | "stopped" | "stopping" | "pending" | "unknown";
  timestamp: string;
}

export interface Orchestration {
  id: string;
  root_task_id: string;
  request_text: string;
  manager_id: string;
  status: string;
  final_response?: string | null;
  created_at: string;
}

/** internal/domain.ChildTaskPlan */
export interface ChildTaskPlan {
  key: string;
  title: string;
  goal?: string;
  manager_domain: string;
  worker_key?: string;
  priority?: TaskPriority;
  capabilities_required?: string[] | null;
  approval_required: boolean;
  action_type?: string;
  risk_level?: Approval["risk_level"];
}

/** internal/domain.PlanDependency: task_key waits for depends_on_key. */
export interface PlanDependency {
  task_key: string;
  depends_on_key: string;
}

/** internal/domain.PlanPayload */
export interface PlanPayload {
  objective?: string;
  interpreted_request?: string;
  manager_id?: string;
  task_type?: string;
  priority?: TaskPriority;
  child_tasks: ChildTaskPlan[] | null;
  dependencies?: PlanDependency[] | null;
  expected_outputs?: string[] | null;
  verification_criteria?: string[] | null;
}

export interface OrchestrationPlan {
  id: string;
  orchestration_id?: string;
  objective: string;
  manager_id: string;
  status: string;
  plan_payload: PlanPayload;
}

export type ComponentState = "ok" | "degraded" | "down" | "unknown";
export const SYSTEM_COMPONENTS = ["api", "database", "worker", "provider", "scheduler", "storage", "runner"] as const;
export type SystemComponentName = (typeof SYSTEM_COMPONENTS)[number];

export interface ComponentStatus {
  state: ComponentState;
  evidence: string;
  observed_at: string | null;
}

/** GET /api/v1/system/status */
export interface SystemStatus {
  generated_at: string;
  components: Partial<Record<SystemComponentName, ComponentStatus>>;
}

export interface SubmitResult {
  orchestration: Orchestration;
  plan: OrchestrationPlan;
}

/** runner.wake from POST /api/v1/jj/messages. */
export interface RunnerWake {
  wake: "signaled" | "skipped" | "failed" | "disabled";
  detail?: string;
}

export type VaultCategory = "id" | "finance" | "resume" | "certificate" | "other";

export interface VaultEntry {
  id: string;
  label: string;
  category: VaultCategory;
  sensitive: boolean;
  encrypted: boolean;
  filename?: string;
  size_bytes: number;
  source?: string;
  fingerprint?: string;
  created_at: string;
}

export interface VaultMiss {
  id: string;
  query: string;
  task_id?: string;
  count: number;
  at: string;
}

export interface ManagerNameSetting {
  manager_name: string;
  default: string;
  is_default: boolean;
}

export interface VoiceTurnResult {
  kind: "wake" | "conversation" | "objective" | "command";
  reply: string;
  handoff: boolean;
  manager_name: string;
  fallback?: string;
  confirm_token?: string;
}

export interface JJHistoryTurn {
  role: "user" | "assistant";
  text: string;
}

export interface JJMessageRequest {
  message: string;
  conversation_id?: string;
  history?: JJHistoryTurn[];
  priority?: TaskPriority;
  idempotency_key?: string;
  /** Force a fresh task even if the message looks like a continuation. */
  new_task?: boolean;
  /** Continue that finished task (the owner's choice, overriding the guess). */
  continue_task_id?: string;
}

/** Set when the message was added to an earlier task's thread instead of becoming a new task. */
export interface JJContinued {
  task_id: string;
  root_id: string;
  title: string;
  /** Why it was judged a continuation, in words the owner can read. */
  reason: string;
}

/** Set when the message became a new task but might continue an earlier one. */
export interface JJSuggestion {
  task_id: string;
  title: string;
  reason: string;
}

export interface DeviceNote {
  capabilities: string[];
  message: string;
}

/** 201 body of POST /api/v1/jj/messages. */
export type JJMessageResult = (
  | { kind: "conversation"; conversation_id: string; task_id: string; runner?: RunnerWake }
  | { kind: "objective"; conversation_id: string; orchestration: Orchestration; plan: OrchestrationPlan; runner?: RunnerWake; needs_device?: DeviceNote }
) & { continued?: JJContinued; suggestion?: JJSuggestion };

// ---------------------------------------------------------------------------
// Devices (hosts): a laptop or phone that connects out and does work (ADR-074, ADR-077)
// ---------------------------------------------------------------------------

export type HostKind = "laptop" | "phone";
export type HostMode = "laptop" | "phone" | "none";
export type HostCapability = "agent" | "browser" | "files" | "shell" | "phone_apps" | "sms" | "calls" | "messaging" | "web";

export interface HostView {
  id: string;
  name: string;
  kind: HostKind;
  /** Granted by you. */
  capabilities: HostCapability[];
  /** What the device reported it can do at its last contact. */
  advertised: HostCapability[];
  /** Granted AND reported: what it may actually do. */
  effective_capabilities: HostCapability[];
  version: string;
  paused: boolean;
  live: boolean;
  last_seen_at?: string | null;
  created_at: string;
}

export interface WaitingTask {
  id: string;
  title: string;
  requires: HostCapability[];
  /** True when a live device can take it now. */
  covered: boolean;
}

export interface HostOverview {
  mode: HostMode;
  hosts: HostView[];
  waiting: WaitingTask[];
}

export interface RegisteredHost {
  host: HostView;
  /** Shown once. Only its hash is kept. */
  token: string;
}

// ---------------------------------------------------------------------------
// Permissions: what your devices may do (PRD 7.16, ADR-079)
// ---------------------------------------------------------------------------

export type PolicyMode = "off" | "ask" | "auto";
export type ActionClass = "read" | "draft" | "send" | "submit" | "buy" | "delete" | "call";

export interface PolicyRule {
  device?: "laptop" | "phone";
  capability?: HostCapability;
  app?: string;
  folder?: string;
  contact?: string;
  class?: ActionClass;
  mode: PolicyMode;
}

export interface Policy {
  default: PolicyMode;
  rules: PolicyRule[];
  /** While in the future, Full mode is on (laptop folder limits lifted). */
  full_until?: string | null;
}

export interface PolicyView {
  policy: Policy;
  /** 0 means nothing saved yet: the safe default applies. */
  version: number;
}

export interface UserProfile {
  id: string;
  email: string;
  role: string;
}

// ---------------------------------------------------------------------------
// Auth/token plumbing
// ---------------------------------------------------------------------------

const TOKEN_KEY = "griffin_auth_token";
const USER_KEY = "griffin_user_profile";

export function getAuthToken(): string | null {
  // Only the dev-login token lives in localStorage, and only when dev login is enabled.
  if (!DEV_LOGIN_ENABLED || typeof window === "undefined") return null;
  return localStorage.getItem(TOKEN_KEY);
}

/** Resolves the bearer token: Supabase session first, then (dev builds only) the dev token. */
export async function resolveAuthToken(): Promise<string | null> {
  return (await getSupabaseAccessToken()) ?? getAuthToken();
}

export function setAuthToken(token: string, user: UserProfile): void {
  if (!DEV_LOGIN_ENABLED || typeof window === "undefined") return;
  localStorage.setItem(TOKEN_KEY, token);
  localStorage.setItem(USER_KEY, JSON.stringify(user));
}

/** Caches the signed-in profile for display (never a credential). */
export function setCurrentUser(user: UserProfile): void {
  if (typeof window === "undefined") return;
  localStorage.setItem(USER_KEY, JSON.stringify(user));
}

export function clearAuthToken(): void {
  if (typeof window === "undefined") return;
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(USER_KEY);
  void getSupabase()?.auth.signOut();
}

export function getCurrentUser(): UserProfile | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = localStorage.getItem(USER_KEY);
    return raw ? (JSON.parse(raw) as UserProfile) : null;
  } catch {
    return null;
  }
}

/** An HTTP error from the API; `status` lets callers tell "not found" from "broken". */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

/** POST /api/v1/jj/messages answered 404: the backend that serves it is not deployed yet. */
export class JJEndpointUnavailable extends Error {
  constructor() {
    super("The JJ messages endpoint is not available on this backend.");
    this.name = "JJEndpointUnavailable";
  }
}

async function errorMessage(res: Response): Promise<string> {
  let message = `HTTP ${res.status}: ${res.statusText}`;
  try {
    const body = await res.json();
    const detail = body?.error;
    if (typeof detail === "string") message = detail;
    else if (detail?.message) message = detail.message;
    else if (body?.message) message = body.message;
  } catch {
    // keep status text
  }
  return message;
}

async function unwrap<T>(res: Response): Promise<T> {
  if (res.status === 204) return {} as T;
  // The API wraps payloads as { success, data }; unwrap so callers receive the data itself.
  const body = await res.json();
  if (body && typeof body === "object" && "success" in body && "data" in body) {
    return body.data as T;
  }
  return body as T;
}

/** Optional per-call options; pass a signal so an obsolete request can be cancelled. */
export interface ReqOpts {
  signal?: AbortSignal;
}

async function doFetch<T>(path: string, options: RequestInit): Promise<T> {
  const token = await resolveAuthToken();
  const headers = new Headers(options.headers || {});
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (!headers.has("Content-Type") && !(options.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
  }
  const res = await fetch(`${API_BASE_URL}${path}`, { ...options, headers });
  if (!res.ok) throw new ApiError(await errorMessage(res), res.status);
  return unwrap<T>(res);
}

interface SharedGet {
  promise: Promise<unknown>;
  controller: AbortController;
  refs: number;
}
const inflightGets = new Map<string, SharedGet>();

const abortError = () => new DOMException("The request was aborted", "AbortError");

/**
 * GETs for the same URL that overlap in time share ONE network request. It is reference-counted:
 * the underlying request is aborted only once every caller has aborted. Nothing is cached after
 * the request settles, and writes are never shared.
 */
async function fetchWithAuth<T>(path: string, options: RequestInit = {}): Promise<T> {
  const method = (options.method ?? "GET").toUpperCase();
  if (method !== "GET") return doFetch<T>(path, options);

  const { signal, ...rest } = options;
  if (signal?.aborted) throw abortError();

  let shared = inflightGets.get(path);
  if (!shared) {
    const controller = new AbortController();
    const created: SharedGet = { controller, refs: 0, promise: Promise.resolve() };
    created.promise = doFetch<unknown>(path, { ...rest, signal: controller.signal }).finally(() => {
      if (inflightGets.get(path) === created) inflightGets.delete(path);
    });
    inflightGets.set(path, created);
    shared = created;
  }
  const entry = shared;
  entry.refs += 1;

  return new Promise<T>((resolve, reject) => {
    let settled = false;
    const onAbort = () => {
      if (settled) return;
      settled = true;
      entry.refs -= 1;
      if (entry.refs <= 0) {
        entry.controller.abort();
        // Never let a later caller join a request we just aborted: React runs a stopped poller's
        // cleanup and the next effect in one synchronous commit, so the entry must leave the map now.
        if (inflightGets.get(path) === entry) inflightGets.delete(path);
      }
      reject(abortError());
    };
    signal?.addEventListener("abort", onAbort, { once: true });
    entry.promise.then(
      (value) => {
        if (settled) return;
        settled = true;
        signal?.removeEventListener("abort", onAbort);
        resolve(value as T);
      },
      (err) => {
        if (settled) return;
        settled = true;
        signal?.removeEventListener("abort", onAbort);
        reject(err);
      },
    );
  });
}

/** Lists come back as `null` when empty on some endpoints; always hand pages an array. */
async function fetchList<T>(path: string, opts?: ReqOpts): Promise<T[]> {
  return (await fetchWithAuth<T[] | null>(path, opts)) ?? [];
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

export const api = {
  /** Supabase email/password sign-in (the production path). */
  async signIn(email: string, password: string): Promise<UserProfile> {
    const sb = getSupabase();
    if (!sb) throw new Error("Supabase is not configured (NEXT_PUBLIC_SUPABASE_URL / _ANON_KEY).");
    const { data, error } = await sb.auth.signInWithPassword({ email, password });
    if (error || !data.user) throw new Error(error?.message || "Sign-in failed.");
    const user: UserProfile = { id: data.user.id, email: data.user.email ?? email, role: "OPERATOR" };
    setCurrentUser(user);
    return user;
  },

  /** DEVELOPMENT ONLY: refuses to run unless NEXT_PUBLIC_ENABLE_DEV_LOGIN=true at build time. */
  async login(email?: string): Promise<{ token: string; user: UserProfile }> {
    if (!DEV_LOGIN_ENABLED) throw new Error("Dev login is disabled.");
    const res = await fetch(`${API_BASE_URL}/api/v1/auth/login`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: email || "admin@griffin.internal" }),
    });
    if (!res.ok) throw new Error(await errorMessage(res));
    const data = await unwrap<{ token: string; user: UserProfile }>(res);
    setAuthToken(data.token, data.user);
    return data;
  },

  // Health (unauthenticated)
  async getHealth(): Promise<{ status: string; time: string }> {
    return unwrap(await fetch(`${API_BASE_URL}/healthz`));
  },
  async getReadiness(): Promise<{ status: string; database: string; time: string }> {
    return unwrap(await fetch(`${API_BASE_URL}/readyz`));
  },

  /** Component health; the endpoint may not exist yet (404), callers must render "unavailable". */
  getSystemStatus: (opts?: ReqOpts) => fetchWithAuth<SystemStatus>("/api/v1/system/status", opts),

  // Devices (404 means the feature is switched off on the server)
  getHosts: (opts?: ReqOpts) => fetchWithAuth<HostOverview>("/api/v1/hosts", opts),
  registerHost: (body: { name: string; kind: HostKind; capabilities: HostCapability[] }) =>
    fetchWithAuth<RegisteredHost>("/api/v1/hosts", { method: "POST", body: JSON.stringify(body) }),
  updateHost: (id: string, body: { paused?: boolean; capabilities?: HostCapability[]; set_capabilities?: boolean }) =>
    fetchWithAuth<{ message?: string }>(`/api/v1/hosts/${id}`, { method: "PATCH", body: JSON.stringify(body) }),
  revokeHost: (id: string) => fetchWithAuth<{ message?: string }>(`/api/v1/hosts/${id}`, { method: "DELETE" }),

  // Permissions (404 means the feature is switched off on the server; 409 means another device saved first)
  getPermissions: (opts?: ReqOpts) => fetchWithAuth<PolicyView>("/api/v1/permissions", opts),
  savePermissions: (policy: Policy, version: number) =>
    fetchWithAuth<PolicyView>("/api/v1/permissions", { method: "PUT", body: JSON.stringify({ policy, version }) }),

  // Runner
  getRunnerStatus: (opts?: ReqOpts) => fetchWithAuth<RunnerStatus>("/api/v1/runner/status", opts),
  wakeRunner: () => fetchWithAuth<{ message?: string; status?: string }>("/api/v1/runner/wake", { method: "POST" }),

  // Tasks
  listTasks(params?: { status?: TaskStatus; limit?: number; parent_id?: string }, opts?: ReqOpts) {
    const q = new URLSearchParams();
    if (params?.status) q.set("status", params.status);
    if (params?.parent_id) q.set("parent_id", params.parent_id);
    if (params?.limit) q.set("limit", String(params.limit));
    const qs = q.toString();
    return fetchList<Task>(`/api/v1/tasks${qs ? `?${qs}` : ""}`, opts);
  },
  getTask: (id: string, opts?: ReqOpts) => fetchWithAuth<Task>(`/api/v1/tasks/${id}`, opts),
  createTask(payload: {
    goal: string;
    title?: string;
    manager_domain?: string;
    priority?: TaskPriority;
    parent_id?: string;
  }) {
    return fetchWithAuth<Task>("/api/v1/tasks", {
      method: "POST",
      body: JSON.stringify({
        title: (payload.title ?? payload.goal).trim().slice(0, 80),
        goal: payload.goal,
        manager_domain: payload.manager_domain ?? "JJ",
        priority: payload.priority ?? "NORMAL_P2",
        parent_id: payload.parent_id,
        input_payload: {},
      }),
    });
  },
  cancelTask: (id: string) => fetchWithAuth<unknown>(`/api/v1/tasks/${id}/cancel`, { method: "POST" }),
  /** Re-queues a failed or cancelled standalone task (409 for plans, subtasks and active tasks). */
  resumeTask: (id: string) => fetchWithAuth<Task>(`/api/v1/tasks/${id}/resume`, { method: "POST" }),
  /** Permanently deletes a finished task and its subtasks (409 while anything in it is still active). */
  deleteTask: (id: string) => fetchWithAuth<unknown>(`/api/v1/tasks/${id}`, { method: "DELETE" }),
  getTaskEvents: (id: string, opts?: ReqOpts) => fetchList<TaskEvent>(`/api/v1/tasks/${id}/events`, opts),
  getTaskAttempts: (id: string, opts?: ReqOpts) => fetchList<TaskAttempt>(`/api/v1/tasks/${id}/attempts`, opts),

  // Orchestrator
  submitOrchestratorRequest: (requestText: string, priority?: TaskPriority) =>
    fetchWithAuth<SubmitResult>("/api/v1/orchestrator/requests", {
      method: "POST",
      body: JSON.stringify({ request_text: requestText, priority }),
    }),
  /**
   * Sends a chat message to JJ. Every call carries an idempotency key (fresh unless given) so a
   * double tap cannot create two replies. Throws JJEndpointUnavailable on 404 (old backend).
   */
  async sendJJMessage(req: JJMessageRequest): Promise<JJMessageResult> {
    try {
      return await fetchWithAuth<JJMessageResult>("/api/v1/jj/messages", {
        method: "POST",
        body: JSON.stringify({ ...req, idempotency_key: req.idempotency_key ?? crypto.randomUUID() }),
      });
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) throw new JJEndpointUnavailable();
      throw e;
    }
  },
  listManagers: () => fetchList<Manager>("/api/v1/managers"),
  /**
   * One spoken turn: a quick answer, a task command (list, cancel, delete, approvals) or a hand-off of real work.
   * A change comes back with a confirm_token; send it back with {confirm: {token, yes}} when the owner answers.
   */
  voiceTurn: (body: { text?: string; history?: JJHistoryTurn[]; confirm?: { token: string; yes: boolean } }) =>
    fetchWithAuth<VoiceTurnResult>("/api/v1/voice/turn", { method: "POST", body: JSON.stringify({ ...body, unlocked: true }) }),
  /** A few seconds of speech about a task, for the Listen page (what a notification tap plays). */
  getSpokenSummary: (taskId: string) =>
    fetchWithAuth<{ text: string; case: "answer" | "summary" | "empty" | "failed" | "approval" | "running"; task_id: string; title: string; status: TaskStatus }>(
      `/api/v1/tasks/${taskId}/spoken-summary`,
    ),
  /** Continues a finished task: the message joins that task's thread and starts from what was said. */
  followUp: (taskId: string, message: string) =>
    fetchWithAuth<{ kind: "conversation" | "objective"; conversation_id: string; task_id?: string; orchestration?: { id: string; root_task_id?: string } }>(
      `/api/v1/tasks/${taskId}/follow-ups`,
      { method: "POST", body: JSON.stringify({ message, idempotency_key: crypto.randomUUID() }) },
    ),

  // The top manager's name (JJ by default)
  getManagerName: () => fetchWithAuth<ManagerNameSetting>("/api/v1/settings/manager-name"),
  putManagerName: (name: string) =>
    fetchWithAuth<ManagerNameSetting>("/api/v1/settings/manager-name", {
      method: "PUT",
      body: JSON.stringify({ manager_name: name }),
    }),

  // Approvals
  listApprovals: (status?: ApprovalStatus, opts?: ReqOpts) =>
    fetchList<Approval>(`/api/v1/approvals${status ? `?status=${status}` : ""}`, opts),
  respondApproval: (id: string, approve: boolean, reason?: string) =>
    fetchWithAuth<unknown>(`/api/v1/approvals/${id}/respond`, {
      method: "POST",
      body: JSON.stringify({ decision: approve ? "APPROVE" : "REJECT", reason: reason ?? "" }),
    }),
  /** Changes the text of a message waiting for approval; the approval then covers the edited text. */
  editApproval: (id: string, text: string) =>
    fetchWithAuth<Approval>(`/api/v1/approvals/${id}`, { method: "PATCH", body: JSON.stringify({ text }) }),
  batchRespondApprovals: (approve: boolean, ids?: string[], reason?: string) =>
    fetchWithAuth<{ approved: number; rejected: number; failed: number; total: number }>(`/api/v1/approvals/batch`, {
      method: "POST",
      body: JSON.stringify({ decision: approve ? "APPROVE" : "REJECT", reason: reason ?? "", ids }),
    }),

  // Outreach
  listCampaigns: (opts?: ReqOpts) => fetchList<OutreachCampaign>(`/api/v1/outreach/campaigns`, opts),
  createCampaign: (body: { name: string; goal?: string; channel: OutreachCampaign["channel"] }) =>
    fetchWithAuth<OutreachCampaign>(`/api/v1/outreach/campaigns`, { method: "POST", body: JSON.stringify(body) }),
  updateCampaign: (id: string, body: { status?: OutreachCampaign["status"]; limits?: OutreachLimits }) =>
    fetchWithAuth<OutreachCampaign>(`/api/v1/outreach/campaigns/${id}`, { method: "PATCH", body: JSON.stringify(body) }),
  listTargets: (campaignId: string, opts?: ReqOpts) =>
    fetchList<OutreachTarget>(`/api/v1/outreach/campaigns/${campaignId}/targets`, opts),
  /** Writes first drafts for up to ten people who are still only "found". */
  draftCampaign: (id: string) =>
    fetchWithAuth<{ drafted: number; failed: number; remaining: number }>(`/api/v1/outreach/campaigns/${id}/draft`, { method: "POST" }),
  addTargets: (campaignId: string, targets: { name: string; handle: string; info?: string }[]) =>
    fetchWithAuth<{ added: number; received: number }>(`/api/v1/outreach/campaigns/${campaignId}/targets`, {
      method: "POST",
      body: JSON.stringify({ targets }),
    }),
  updateTarget: (id: string, body: { status?: OutreachStatus; draft?: string; notes?: string }) =>
    fetchWithAuth<OutreachTarget>(`/api/v1/outreach/targets/${id}`, { method: "PATCH", body: JSON.stringify(body) }),
  /** The campaign as a CSV file's text, for a spreadsheet. */
  exportCampaign: async (id: string): Promise<string> => {
    const token = await resolveAuthToken();
    const res = await fetch(`${API_BASE_URL}/api/v1/outreach/campaigns/${id}/export.csv`, { headers: token ? { Authorization: `Bearer ${token}` } : {} });
    if (!res.ok) throw new Error(`Could not export (HTTP ${res.status}).`);
    return res.text();
  },

  // Artifacts
  listArtifacts: (taskId?: string, opts?: ReqOpts) =>
    fetchList<Artifact>(`/api/v1/artifacts${taskId ? `?task_id=${taskId}` : ""}`, opts),
  getPresignedUrl: (id: string) => fetchWithAuth<{ url: string; expires_at: string }>(`/api/v1/artifacts/${id}/presign`),
  getArtifact: (id: string, opts?: ReqOpts) => fetchWithAuth<Artifact>(`/api/v1/artifacts/${id}`, opts),
  /** Living lists: Markdown files a task or you can keep adding to. */
  createList: (title: string, content?: string) => fetchWithAuth<Artifact>("/api/v1/lists", { method: "POST", body: JSON.stringify({ title, content }) }),
  updateArtifactText: (id: string, text: string, mode: "append" | "replace") =>
    fetchWithAuth<Artifact>(`/api/v1/artifacts/${id}/text`, { method: "PUT", body: JSON.stringify({ text, mode }) }),
  async getArtifactText(id: string, opts?: ReqOpts): Promise<string> {
    const token = await resolveAuthToken();
    const res = await fetch(`${API_BASE_URL}/api/v1/artifacts/${id}/download`, { headers: token ? { Authorization: `Bearer ${token}` } : {}, signal: opts?.signal });
    if (!res.ok) throw new ApiError(await errorMessage(res), res.status);
    return res.text();
  },
  /** A file's bytes, through the API (the storage bucket needs no browser access rules this way). */
  async getArtifactBytes(id: string, opts?: ReqOpts): Promise<{ bytes: Uint8Array; contentType: string }> {
    const token = await resolveAuthToken();
    const res = await fetch(`${API_BASE_URL}/api/v1/artifacts/${id}/download`, { headers: token ? { Authorization: `Bearer ${token}` } : {}, signal: opts?.signal });
    if (!res.ok) throw new ApiError(await errorMessage(res), res.status);
    return { bytes: new Uint8Array(await res.arrayBuffer()), contentType: res.headers.get("Content-Type") ?? "application/octet-stream" };
  },

  // The Vault: documents you may need on your phone (sensitive ones are encrypted on your device first)
  listVault: (opts?: ReqOpts) => fetchList<VaultEntry>("/api/v1/vault", opts),
  addVault: (form: FormData) => fetchWithAuth<{ duplicate: boolean; entry: VaultEntry }>("/api/v1/vault", { method: "POST", body: form }),
  listVaultMisses: (opts?: ReqOpts) => fetchList<VaultMiss>("/api/v1/vault/misses", opts),
  resolveVaultMiss: (id: string) => fetchWithAuth<unknown>(`/api/v1/vault/misses/${id}`, { method: "DELETE" }),
  deleteArtifact: (id: string) => fetchWithAuth<unknown>(`/api/v1/artifacts/${id}`, { method: "DELETE" }),
  async uploadArtifact(file: File, taskId?: string): Promise<Artifact> {
    const form = new FormData();
    if (taskId) form.append("task_id", taskId);
    form.append("title", file.name);
    form.append("file", file);
    return fetchWithAuth<Artifact>("/api/v1/artifacts/upload", { method: "POST", body: form });
  },
};
