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
  { value: "BACKGROUND_P3", label: "Background" },
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

export interface Artifact {
  id: string;
  task_id?: string | null;
  title: string;
  artifact_type: string;
  storage_provider: string;
  filename?: string | null;
  file_size_bytes?: number | null;
  mime_type?: string | null;
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

export interface OrchestrationPlan {
  id: string;
  objective: string;
  manager_id: string;
  status: string;
  plan_payload: {
    child_tasks: { key: string; title: string; manager_domain: string; approval_required: boolean }[];
  };
}

export interface SubmitResult {
  orchestration: Orchestration;
  plan: OrchestrationPlan;
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

async function fetchWithAuth<T>(path: string, options: RequestInit = {}): Promise<T> {
  const token = await resolveAuthToken();
  const headers = new Headers(options.headers || {});
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (!headers.has("Content-Type") && !(options.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
  }
  const res = await fetch(`${API_BASE_URL}${path}`, { ...options, headers });
  if (!res.ok) throw new Error(await errorMessage(res));
  return unwrap<T>(res);
}

/** Lists come back as `null` when empty on some endpoints; always hand pages an array. */
async function fetchList<T>(path: string): Promise<T[]> {
  return (await fetchWithAuth<T[] | null>(path)) ?? [];
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

  // Runner
  getRunnerStatus: () => fetchWithAuth<RunnerStatus>("/api/v1/runner/status"),
  wakeRunner: () => fetchWithAuth<{ message?: string; status?: string }>("/api/v1/runner/wake", { method: "POST" }),

  // Tasks
  listTasks(params?: { status?: TaskStatus; limit?: number }) {
    const q = new URLSearchParams();
    if (params?.status) q.set("status", params.status);
    if (params?.limit) q.set("limit", String(params.limit));
    const qs = q.toString();
    return fetchList<Task>(`/api/v1/tasks${qs ? `?${qs}` : ""}`);
  },
  getTask: (id: string) => fetchWithAuth<Task>(`/api/v1/tasks/${id}`),
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
  getTaskEvents: (id: string) => fetchList<TaskEvent>(`/api/v1/tasks/${id}/events`),
  getTaskAttempts: (id: string) => fetchList<TaskAttempt>(`/api/v1/tasks/${id}/attempts`),

  // Orchestrator
  submitOrchestratorRequest: (requestText: string, priority?: TaskPriority) =>
    fetchWithAuth<SubmitResult>("/api/v1/orchestrator/requests", {
      method: "POST",
      body: JSON.stringify({ request_text: requestText, priority }),
    }),
  listManagers: () => fetchList<Manager>("/api/v1/managers"),

  // Approvals
  listApprovals: (status?: ApprovalStatus) =>
    fetchList<Approval>(`/api/v1/approvals${status ? `?status=${status}` : ""}`),
  respondApproval: (id: string, approve: boolean, reason?: string) =>
    fetchWithAuth<unknown>(`/api/v1/approvals/${id}/respond`, {
      method: "POST",
      body: JSON.stringify({ decision: approve ? "APPROVE" : "REJECT", reason: reason ?? "" }),
    }),

  // Artifacts
  listArtifacts: (taskId?: string) => fetchList<Artifact>(`/api/v1/artifacts${taskId ? `?task_id=${taskId}` : ""}`),
  getPresignedUrl: (id: string) => fetchWithAuth<{ url: string; expires_at: string }>(`/api/v1/artifacts/${id}/presign`),
  deleteArtifact: (id: string) => fetchWithAuth<unknown>(`/api/v1/artifacts/${id}`, { method: "DELETE" }),
  async uploadArtifact(file: File, taskId?: string): Promise<Artifact> {
    const form = new FormData();
    if (taskId) form.append("task_id", taskId);
    form.append("title", file.name);
    form.append("file", file);
    return fetchWithAuth<Artifact>("/api/v1/artifacts/upload", { method: "POST", body: form });
  },
};
