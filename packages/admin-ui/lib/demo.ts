// Helpers for the dark-aesthetic demo view at `/`. Keeps fetch plumbing
// and small pure utilities out of app/page.tsx so the component file
// stays readable.

export type AuditEvent = {
  id: string;
  timestamp: string;
  actor: string;
  on_behalf_of?: string;
  action: string;
  resource?: string;
  decision: "allow" | "deny" | "error";
};

export type Draft = {
  id: string;
  schema_id: string;
  body: string; // JSON string of the draft.v1 document
  created_at: string;
};

export type DraftBody = {
  subject: string;
  body: string;
  to: string[];
  generatedByAgent: string;
  generatedAt: string;
  status: "pending" | "sending" | "sent" | "rejected" | "error";
  sentMessageId?: string;
  inReplyToDocumentId?: string;
};

export type TriageStats = {
  pairs_checked: number;
  emails_processed: number;
  drafts_created: number;
  emails_skipped: number;
  errors: number;
};

// ---------------------------------------------------------------------------
// Low-level fetch helpers. We intentionally bypass lib/api.ts here so
// the demo can take its config as function args rather than pulling
// from localStorage on every call.
// ---------------------------------------------------------------------------

type FetchOpts = {
  gateway: string;
  admin?: string;
  principal?: string;
  method?: string;
  body?: unknown;
};

export async function gfetch<T = unknown>(path: string, opts: FetchOpts): Promise<T> {
  const headers: Record<string, string> = { "content-type": "application/json" };
  if (opts.admin) headers["Authorization"] = `Bearer ${opts.admin}`;
  if (opts.principal) headers["X-Dominion-Dev-Principal"] = opts.principal;
  const res = await fetch(`${opts.gateway}${path}`, {
    method: opts.method || (opts.body ? "POST" : "GET"),
    headers,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  const text = await res.text();
  let parsed: any = null;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      parsed = text;
    }
  }
  if (!res.ok) {
    const msg =
      (parsed && typeof parsed === "object" && (parsed.message || parsed.error)) ||
      res.statusText;
    throw new Error(`[${res.status}] ${msg}`);
  }
  return parsed as T;
}

// ---------------------------------------------------------------------------
// Pure helpers — exported so they're unit-testable without a DOM.
// ---------------------------------------------------------------------------

export function relTime(iso: string): string {
  const t = Date.parse(iso);
  if (isNaN(t)) return iso;
  const diff = Math.max(0, Date.now() - t);
  if (diff < 2_000) return "now";
  if (diff < 60_000) return `${Math.round(diff / 1000)}s ago`;
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.round(diff / 3_600_000)}h ago`;
  return new Date(t).toLocaleString();
}

export function actorKind(actor: string): "agent" | "user" | "admin" | "system" | "other" {
  if (actor.startsWith("agent:")) return "agent";
  if (actor.startsWith("user:")) return "user";
  if (actor.startsWith("admin:")) return "admin";
  if (actor.startsWith("system:")) return "system";
  return "other";
}

export function actorChipClass(actor: string): string {
  switch (actorKind(actor)) {
    case "agent":
      return "bg-violet-500/10 text-violet-300 border-violet-500/30";
    case "user":
      return "bg-sky-500/10 text-sky-300 border-sky-500/30";
    case "admin":
      return "bg-amber-500/10 text-amber-300 border-amber-500/30";
    case "system":
      return "bg-neutral-800 text-neutral-400 border-neutral-700";
    default:
      return "bg-neutral-800 text-neutral-400 border-neutral-700";
  }
}

export function shortId(s: string, n = 8): string {
  if (!s) return s;
  const colon = s.indexOf(":");
  if (colon >= 0) return s.slice(0, colon + 1) + s.slice(colon + 1, colon + 1 + n) + "…";
  return s.length > n ? s.slice(0, n) + "…" : s;
}

export function decisionDot(decision: string): string {
  if (decision === "allow") return "bg-emerald-400";
  if (decision === "deny") return "bg-red-400";
  return "bg-amber-400";
}
