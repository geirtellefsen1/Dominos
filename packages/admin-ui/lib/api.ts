// Minimal fetch helpers. The admin UI speaks directly to the gateway;
// admin routes use a bearer token pasted by the operator, user-view
// routes use the dev-principal header shim (phase 9's UI is not wired
// to real OIDC sessions yet — phase 7's /auth/login still works
// separately for that).

export const STORAGE_KEYS = {
  gatewayUrl: "dominion:gateway_url",
  adminToken: "dominion:admin_token",
  devPrincipal: "dominion:dev_principal",
} as const;

export function getGatewayUrl(): string {
  if (typeof window === "undefined") return "";
  return (
    localStorage.getItem(STORAGE_KEYS.gatewayUrl) || "http://localhost:3000"
  );
}
export function getAdminToken(): string {
  if (typeof window === "undefined") return "";
  return localStorage.getItem(STORAGE_KEYS.adminToken) || "";
}
export function getDevPrincipal(): string {
  if (typeof window === "undefined") return "";
  return localStorage.getItem(STORAGE_KEYS.devPrincipal) || "";
}

type Opts = {
  method?: string;
  body?: unknown;
  admin?: boolean; // Authorization: Bearer <admin>
  asPrincipal?: boolean; // X-Dominion-Dev-Principal
  expectStatus?: number[]; // default [200..299]
};

export async function call<T = unknown>(path: string, opts: Opts = {}): Promise<T> {
  const base = getGatewayUrl();
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (opts.admin) {
    const t = getAdminToken();
    if (!t) throw new Error("admin token not set");
    headers["Authorization"] = `Bearer ${t}`;
  }
  if (opts.asPrincipal) {
    const p = getDevPrincipal();
    if (!p) throw new Error("dev principal not set");
    headers["X-Dominion-Dev-Principal"] = p;
  }
  const res = await fetch(`${base}${path}`, {
    method: opts.method || (opts.body ? "POST" : "GET"),
    headers,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  const text = await res.text();
  let data: any = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = text;
    }
  }
  const ok = (opts.expectStatus ?? [-1]).includes(res.status) || res.ok;
  if (!ok) {
    const msg = typeof data === "object" && data?.message ? data.message : res.statusText;
    const code = typeof data === "object" && data?.error ? data.error : "http_error";
    throw new Error(`[${res.status} ${code}] ${msg}`);
  }
  return data as T;
}
