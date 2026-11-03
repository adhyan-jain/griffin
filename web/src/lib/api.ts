/**
 * Griffin API Client and TypeScript Domain Models
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

// UI priority tiers (0-3) -> backend domain.TaskPriority string enum.
const PRIORITY_WIRE = ["CRITICAL_P0", "HIGH_P1", "NORMAL_P2", "BACKGROUND_P3"] as const;
export function priorityToWire(p?: number): (typeof PRIORITY_WIRE)[number] {
  return PRIORITY_WIRE[p ?? 2] ?? "NORMAL_P2";
}

export interface Task {
  id: string;
  user_id: string;
  parent_task_id?: string | null;
  goal: string;
  assigned_manager: string;
  assigned_specialist: string;
  step_index: number;
  status: "queued" | "running" | "waiting_approval" | "succeeded" | "failed" | "blocked" | "cancelled";
  priority: number | string;
  input_payload: Record<string, any>;
  output_payload?: Record<string, any>;
  retry_count: number;
  max_retries: number;
  worker_id?: string | null;
  created_at: string;
  updated_at: string;
}

export interface TaskAttempt {
  id: string;
  task_id: string;
  attempt_number: number;
  worker_id: string;
  status: string;
  exit_code?: number | null;
  error_trace?: string | null;
  started_at: string;
  completed_at?: string | null;
}

export interface TaskEvent {
  id: string;
  task_id: string;
  event_type: string;
  old_status?: string | null;
  new_status?: string | null;
  details?: Record<string, any>;
  created_at: string;
}

export interface Approval {
  id: string;
  task_id: string;
  status: "pending" | "approved" | "rejected";
  action_type: string;
  risk_level: string;
  requested_by: string;
  input_payload?: Record<string, any>;
  reviewer_id?: string | null;
  review_comment?: string | null;
  created_at: string;
  updated_at: string;
}

export interface Artifact {
  id: string;
  task_id: string;
  user_id: string;
  storage_provider: string;
  bucket: string;
  object_key: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  checksum_sha256: string;
  metadata?: Record<string, any>;
  created_at: string;
}

export interface ManagerDef {
  id: string;
  name: string;
  description: string;
  specialists: string[];
  tools: string[];
}

export interface RunnerStatus {
  scale_to_zero_enabled: boolean;
  state: "running" | "stopped" | "stopping" | "pending" | "unknown";
  timestamp: string;
}

export interface OrchestrationPlan {
  id: string;
  request_id: string;
  summary: string;
  tasks: Array<{
    goal: string;
    manager: string;
    specialist?: string;
    priority?: number;
    dependencies?: number[];
  }>;
  created_at: string;
}

export interface OrchestrationRequestResponse {
  orchestration_id: string;
  status: string;
  manager: string;
  goal: string;
  created_task_ids: string[];
  plan?: OrchestrationPlan;
}

export interface UserProfile {
  id: string;
  email: string;
  role: string;
}

// Token storage helpers
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
  const userStr = localStorage.getItem(USER_KEY);
  if (!userStr) return null;
  try {
    return JSON.parse(userStr);
  } catch {
    return null;
  }
}

// Base fetcher with JWT injection
async function fetchWithAuth<T>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  const token = await resolveAuthToken();
  const headers = new Headers(options.headers || {});

  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  if (!headers.has("Content-Type") && !(options.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
  }

  const res = await fetch(`${API_BASE_URL}${path}`, {
    ...options,
    headers,
  });

  if (!res.ok) {
    let errMessage = `HTTP error ${res.status}: ${res.statusText}`;
    try {
      const errBody = await res.json();
      const detail = errBody?.error;
      if (typeof detail === "string") errMessage = detail;
      else if (detail?.message) errMessage = detail.message;
      else if (errBody?.message) errMessage = errBody.message;
    } catch {
      // ignore json parse error
    }
    throw new Error(errMessage);
  }

  if (res.status === 204) {
    return {} as T;
  }

  // The API wraps payloads as { success, data }; unwrap so callers receive the data itself.
  const body = await res.json();
  if (body && typeof body === "object" && "success" in body && "data" in body) {
    return body.data as T;
  }
  return body as T;
}

export const api = {
  // Auth
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
    if (process.env.NEXT_PUBLIC_ENABLE_DEV_LOGIN !== "true") throw new Error("Dev login is disabled.");
    const res = await fetch(`${API_BASE_URL}/api/v1/auth/login`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ email: email || "admin@griffin.internal" }),
    });
    if (!res.ok) {
      throw new Error(`Login failed with status ${res.status}`);
    }
    const body = await res.json();
    const data = (body.data ?? body) as { token: string; user: UserProfile };
    setAuthToken(data.token, data.user);
    return data;
  },

  // Health
  async getHealth(): Promise<{ status: string; time: string }> {
    const res = await fetch(`${API_BASE_URL}/healthz`);
    return res.json();
  },

  async getReadiness(): Promise<{ status: string; database: string; time: string }> {
    const res = await fetch(`${API_BASE_URL}/readyz`);
    return res.json();
  },

  // Runner
  async getRunnerStatus(): Promise<RunnerStatus> {
    return fetchWithAuth<RunnerStatus>("/api/v1/runner/status");
  },

  async wakeRunner(): Promise<{ message: string; status: string }> {
    return fetchWithAuth("/api/v1/runner/wake", { method: "POST" });
  },

  // Tasks
  async listTasks(params?: { status?: string; manager?: string }): Promise<Task[]> {
    const query = new URLSearchParams();
    if (params?.status) query.set("status", params.status);
    if (params?.manager) query.set("manager", params.manager);
    const qs = query.toString() ? `?${query.toString()}` : "";
    return fetchWithAuth<Task[]>(`/api/v1/tasks${qs}`);
  },

  async getTask(id: string): Promise<Task> {
    return fetchWithAuth<Task>(`/api/v1/tasks/${id}`);
  },

  async createTask(payload: {
    goal: string;
    assigned_manager?: string;
    assigned_specialist?: string;
    priority?: number;
    parent_task_id?: string;
    input_payload?: Record<string, any>;
    max_retries?: number;
  }): Promise<Task> {
    // The backend expects a string priority enum, a title and manager_domain.
    const body = {
      title: payload.goal.trim().slice(0, 80),
      goal: payload.goal,
      manager_domain: payload.assigned_manager ?? "JJ",
      priority: priorityToWire(payload.priority),
      parent_id: payload.parent_task_id,
      input_payload: payload.input_payload ?? {},
    };
    return fetchWithAuth<Task>("/api/v1/tasks", {
      method: "POST",
      body: JSON.stringify(body),
    });
  },

  async cancelTask(id: string): Promise<{ message: string }> {
    return fetchWithAuth(`/api/v1/tasks/${id}/cancel`, { method: "POST" });
  },

  async getTaskEvents(id: string): Promise<TaskEvent[]> {
    return fetchWithAuth<TaskEvent[]>(`/api/v1/tasks/${id}/events`);
  },

  async getTaskAttempts(id: string): Promise<TaskAttempt[]> {
    return fetchWithAuth<TaskAttempt[]>(`/api/v1/tasks/${id}/attempts`);
  },

  // Orchestrator
  async submitOrchestratorRequest(goal: string): Promise<OrchestrationRequestResponse> {
    return fetchWithAuth<OrchestrationRequestResponse>("/api/v1/orchestrator/requests", {
      method: "POST",
      body: JSON.stringify({ goal }),
    });
  },

  async getOrchestratorPlan(id: string): Promise<OrchestrationPlan> {
    return fetchWithAuth<OrchestrationPlan>(`/api/v1/orchestrator/plans/${id}`);
  },

  // Approvals
  async listApprovals(status?: string): Promise<Approval[]> {
    const query = status ? `?status=${status}` : "";
    return fetchWithAuth<Approval[]>(`/api/v1/approvals${query}`);
  },

  async respondApproval(id: string, approved: boolean, reason?: string): Promise<{ message: string }> {
    return fetchWithAuth(`/api/v1/approvals/${id}/respond`, {
      method: "POST",
      body: JSON.stringify({ approved, reason: reason || "" }),
    });
  },

  // Artifacts
  async listArtifacts(taskId?: string): Promise<Artifact[]> {
    const query = taskId ? `?task_id=${taskId}` : "";
    return fetchWithAuth<Artifact[]>(`/api/v1/artifacts${query}`);
  },

  async getArtifact(id: string): Promise<Artifact> {
    return fetchWithAuth<Artifact>(`/api/v1/artifacts/${id}`);
  },

  async getPresignedUrl(id: string): Promise<{ download_url: string; expires_at: string }> {
    return fetchWithAuth(`/api/v1/artifacts/${id}/presign`);
  },

  async deleteArtifact(id: string): Promise<{ message: string }> {
    return fetchWithAuth(`/api/v1/artifacts/${id}`, { method: "DELETE" });
  },

  async uploadArtifact(taskId: string, file: File): Promise<Artifact> {
    const formData = new FormData();
    formData.append("task_id", taskId);
    formData.append("file", file);

    const token = await resolveAuthToken();
    const headers: Record<string, string> = {};
    if (token) {
      headers["Authorization"] = `Bearer ${token}`;
    }

    const res = await fetch(`${API_BASE_URL}/api/v1/artifacts/upload`, {
      method: "POST",
      headers,
      body: formData,
    });

    if (!res.ok) {
      const err = await res.json().catch(() => ({}));
      throw new Error(err.error || `Upload failed: ${res.status}`);
    }

    return res.json();
  },

  // Workforce Managers
  async listManagers(): Promise<ManagerDef[]> {
    try {
      return await fetchWithAuth<ManagerDef[]>("/api/v1/managers");
    } catch {
      // Fallback built-in manager list if backend managers endpoint is unpopulated
      return [
        {
          id: "mgr-jj",
          name: "JJ (Workforce Orchestrator)",
          description: "Top-level orchestrator. Analyzes intent, delegates tasks to specialist managers, validates DAG outputs.",
          specialists: ["General Strategy", "Delegator", "DAG Architect"],
          tools: ["antigravity-cli", "task-engine", "agent-factory"],
        },
        {
          id: "mgr-arch",
          name: "Architecture Manager",
          description: "Responsible for system boundaries, domain models, ADR decisions, and scalability specifications.",
          specialists: ["System Architect", "Domain Modeler", "Interface Designer"],
          tools: ["antigravity-cli", "docs-generator", "repo-analyzer"],
        },
        {
          id: "mgr-code",
          name: "Code Manager",
          description: "Implements production-grade software according to domain design and architectural specifications.",
          specialists: ["Go Backend Specialist", "Frontend Specialist", "Algorithm Specialist"],
          tools: ["antigravity-cli", "go-toolchain", "git"],
        },
        {
          id: "mgr-qa",
          name: "Quality & Testing Manager",
          description: "Drives TDD, mutation testing, integration test suites, and regression guards.",
          specialists: ["Integration Tester", "Performance Tester", "Fuzzing Specialist"],
          tools: ["go-test", "coverage-analyzer", "k6"],
        },
        {
          id: "mgr-sec",
          name: "Security Manager",
          description: "Enforces zero-trust boundaries, IAM least privilege, secret leak prevention, and vulnerability scans.",
          specialists: ["IAM Architect", "Vulnerability Auditor", "Secret Scanner"],
          tools: ["trivy", "gitleaks", "iam-validator"],
        },
        {
          id: "mgr-devops",
          name: "DevOps & Infrastructure Manager",
          description: "Provisions cloud resources, maintains CI/CD pipelines, handles AWS EC2 scale-to-zero lifecycles.",
          specialists: ["AWS Cloud Specialist", "Docker Specialist", "CI/CD Engineer"],
          tools: ["aws-cli", "docker", "systemd"],
        },
        {
          id: "mgr-data",
          name: "Data & DB Manager",
          description: "Designs PostgreSQL schemas, writes zero-downtime migrations, and manages query performance.",
          specialists: ["PostgreSQL DBA", "Migration Specialist", "Indexing Tuner"],
          tools: ["pgx", "golang-migrate", "psql"],
        },
        {
          id: "mgr-research",
          name: "Research Manager",
          description: "Conducts deep technical investigations into primary sources, documentation, and external APIs.",
          specialists: ["API Researcher", "RFC Analyzer", "Literature Synthesizer"],
          tools: ["web-search", "doc-reader", "md-summarizer"],
        },
        {
          id: "mgr-product",
          name: "Product & Spec Manager",
          description: "Translates high-level founder intent into rigorous, verifiable acceptance criteria and user stories.",
          specialists: ["PRD Writer", "Spec Reviewer", "Acceptance Tester"],
          tools: ["spec-validator", "gherkin-editor"],
        },
        {
          id: "mgr-integrations",
          name: "Integrations Manager",
          description: "Manages integrations with AWS S3, Supabase Auth, EventBridge, and external SaaS webhooks.",
          specialists: ["Webhook Handler", "S3 Storage Specialist", "OAuth Manager"],
          tools: ["aws-sdk", "oauth-client", "curl"],
        },
        {
          id: "mgr-ops",
          name: "Operations & Recovery Manager",
          description: "Monitors worker health, dead-letter task recovery, automated rollback, and incident alerting.",
          specialists: ["Heartbeat Monitor", "Saga Compensator", "Incident Responder"],
          tools: ["health-checker", "log-tailer", "saga-orchestrator"],
        },
      ];
    }
  },
};
