export type RoleCode = "system_admin" | "secretary" | "business_user" | "auditor";

export type SessionPrincipal = {
  userId: string;
  username: string;
  roles: RoleCode[];
  mustChangePassword: boolean;
  expiresAt: number;
};

const tokenKey = "govscribe.session_token";

export function loadToken(): string {
  return window.localStorage.getItem(tokenKey) ?? "";
}

export function saveToken(token: string): void {
  window.localStorage.setItem(tokenKey, token);
}

export function clearToken(): void {
  window.localStorage.removeItem(tokenKey);
}

export function parseSession(token: string): SessionPrincipal | null {
  const parts = token.split(".");
  if (parts.length !== 3) {
    return null;
  }
  try {
    const payload = JSON.parse(base64UrlDecode(parts[1])) as {
      sub?: string;
      username?: string;
      roles?: RoleCode[];
      must_change_password?: boolean;
      exp?: number;
    };
    if (!payload.sub || !payload.exp || payload.exp <= Math.floor(Date.now() / 1000)) {
      return null;
    }
    return {
      userId: payload.sub,
      username: payload.username ?? payload.sub,
      roles: payload.roles ?? [],
      mustChangePassword: Boolean(payload.must_change_password),
      expiresAt: payload.exp
    };
  } catch {
    return null;
  }
}

export function hasRole(principal: SessionPrincipal | null, role: RoleCode): boolean {
  return Boolean(principal?.roles.includes(role));
}

function base64UrlDecode(value: string): string {
  const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
  const padding = "=".repeat((4 - (normalized.length % 4)) % 4);
  return decodeURIComponent(
    atob(normalized + padding)
      .split("")
      .map((char) => `%${char.charCodeAt(0).toString(16).padStart(2, "0")}`)
      .join("")
  );
}
