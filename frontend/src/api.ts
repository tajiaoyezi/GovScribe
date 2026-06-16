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
