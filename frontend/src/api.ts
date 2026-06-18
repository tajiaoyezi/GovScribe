import { RoleCode } from "./auth";

const apiBase = "/api";

export type LoginResponse = {
  token: string;
};

export type ManagedUser = {
  id: string;
  username: string;
  department: string;
  isActive: boolean;
  mustChangePassword: boolean;
  roles: RoleCode[];
};

export async function login(username: string, password: string): Promise<LoginResponse> {
  return request<LoginResponse>("/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password })
  });
}

export async function changePassword(token: string, newPassword: string): Promise<void> {
  await request<void>("/auth/change-password", {
    method: "POST",
    token,
    body: JSON.stringify({ newPassword })
  });
}

export async function listUsers(token: string): Promise<ManagedUser[]> {
  return request<ManagedUser[]>("/admin/users", { token });
}

export async function createUser(token: string, input: {
  username: string;
  password: string;
  department: string;
  role: RoleCode;
}): Promise<ManagedUser> {
  return request<ManagedUser>("/admin/users", {
    method: "POST",
    token,
    body: JSON.stringify(input)
  });
}

export async function disableUser(token: string, userId: string): Promise<void> {
  await request<void>(`/admin/users/${encodeURIComponent(userId)}/disable`, {
    method: "POST",
    token
  });
}

export async function assignRole(token: string, userId: string, role: RoleCode): Promise<void> {
  await request<void>(`/admin/users/${encodeURIComponent(userId)}/role`, {
    method: "PUT",
    token,
    body: JSON.stringify({ role })
  });
}

export async function resetPassword(token: string, userId: string, password: string): Promise<void> {
  await request<void>(`/admin/users/${encodeURIComponent(userId)}/password`, {
    method: "POST",
    token,
    body: JSON.stringify({ password })
  });
}

// ===== c06 文种判别 / 候选确认 / 要素澄清（请求-响应，不流式）=====

export type SecurityLevel = "" | "unclassified" | "sensitive" | "classified";

export type CandidateView = {
  doctype: string;
  subtype: string;
  direction: string;
  confidence: number;
  tier: string;
  isStarredRare: boolean;
  targetCapability: string;
};

export type ClassifyResponse = {
  needsConfirmation: boolean;
  result?: CandidateView;
  candidates?: CandidateView[];
};

export type ScenarioContextView = {
  targetCapability: string;
  doctype: string;
  subtype: string;
  direction: string;
  confidence: number;
  sceneDescription: string;
  filledSlots: Record<string, string>;
  missingSlots: string[];
  contentSecurityLevel: string;
};

export type ClarifyResponse = {
  done: boolean;
  askingSlot?: string;
  question?: string;
  filled: Record<string, string>;
  round: number;
  context?: ScenarioContextView;
};

export type ClarifyInput = {
  doctype: string;
  subtype: string;
  scene: string;
  securityLevel: SecurityLevel;
  filled: Record<string, string>;
  round: number;
  skipped: boolean;
};

export async function classifyDoctype(
  token: string,
  scene: string,
  securityLevel: SecurityLevel
): Promise<ClassifyResponse> {
  return request<ClassifyResponse>("/doctype/classify", {
    method: "POST",
    token,
    body: JSON.stringify({ scene, securityLevel })
  });
}

export async function clarifyDoctype(token: string, input: ClarifyInput): Promise<ClarifyResponse> {
  return request<ClarifyResponse>("/doctype/clarify", {
    method: "POST",
    token,
    body: JSON.stringify(input)
  });
}

async function request<T>(path: string, init: RequestInit & { token?: string } = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");
  if (init.token) {
    headers.set("Authorization", `Bearer ${init.token}`);
  }
  const response = await fetch(apiBase + path, { ...init, headers });
  if (response.status === 401) {
    throw new ApiError("unauthenticated", response.status);
  }
  if (response.status === 403) {
    throw new ApiError("forbidden", response.status);
  }
  if (!response.ok) {
    throw new ApiError("request_failed", response.status);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

export class ApiError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
  }
}
