"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AuditEvent,
  actorChipClass,
  decisionDot,
  gfetch,
  relTime,
  shortId,
} from "../lib/demo";

// ---------------------------------------------------------------------------
// Keys + types
// ---------------------------------------------------------------------------

const K = {
  gateway:   "dominion:gateway_url",
  admin:     "dominion:admin_token",
  aliceID:   "dominion:demo_alice_id",
  astridID:  "dominion:demo_astrid_id",
  aliceMail: "dominion:demo_alice_email",
} as const;

type DemoConfig = {
  gateway: string;
  admin: string;
  aliceID: string;
  astridID: string;
  aliceMail: string;
};

function loadConfig(): DemoConfig | null {
  if (typeof window === "undefined") return null;
  const c: DemoConfig = {
    gateway:   localStorage.getItem(K.gateway)   || "",
    admin:     localStorage.getItem(K.admin)     || "",
    aliceID:   localStorage.getItem(K.aliceID)   || "",
    astridID:  localStorage.getItem(K.astridID)  || "",
    aliceMail: localStorage.getItem(K.aliceMail) || "",
  };
  if (!c.gateway || !c.admin || !c.aliceID || !c.astridID) return null;
  return c;
}

function saveConfig(c: DemoConfig) {
  localStorage.setItem(K.gateway,   c.gateway);
  localStorage.setItem(K.admin,     c.admin);
  localStorage.setItem(K.aliceID,   c.aliceID);
  localStorage.setItem(K.astridID,  c.astridID);
  localStorage.setItem(K.aliceMail, c.aliceMail);
  // Also mirror into the old lib/api.ts keys so the /admin console
  // stays usable alongside the demo view.
  localStorage.setItem("dominion:gateway_url", c.gateway);
  localStorage.setItem("dominion:admin_token", c.admin);
  localStorage.setItem("dominion:dev_principal", `user:${c.aliceID}`);
}

// ---------------------------------------------------------------------------
// Root
// ---------------------------------------------------------------------------

export default function Demo() {
  const [cfg, setCfg] = useState<DemoConfig | null>(null);
  const [showSetup, setShowSetup] = useState(false);
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    const c = loadConfig();
    setCfg(c);
    setShowSetup(c === null);
    setMounted(true);
  }, []);

  if (!mounted) return null;

  return (
    <div className="min-h-screen bg-neutral-950 text-neutral-100">
      <TopBar
        cfg={cfg}
        onOpenSetup={() => setShowSetup(true)}
        onOpenAdmin={() => (window.location.href = "/admin")}
      />
      {cfg ? (
        <Stage cfg={cfg} />
      ) : (
        <WelcomeEmpty onOpenSetup={() => setShowSetup(true)} />
      )}
      {showSetup && (
        <SetupSheet
          initial={cfg}
          onClose={() => setShowSetup(false)}
          onSave={(c) => {
            saveConfig(c);
            setCfg(c);
            setShowSetup(false);
          }}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Top bar — brand + governance words + settings cog
// ---------------------------------------------------------------------------

function TopBar({
  cfg,
  onOpenSetup,
  onOpenAdmin,
}: {
  cfg: DemoConfig | null;
  onOpenSetup: () => void;
  onOpenAdmin: () => void;
}) {
  return (
    <header className="sticky top-0 z-20 border-b border-neutral-800 bg-neutral-950/80 backdrop-blur">
      <div className="mx-auto flex max-w-[1600px] items-center gap-6 px-6 py-4">
        <div className="flex items-baseline gap-3">
          <span className="text-lg font-semibold tracking-tight">Dominion</span>
          <span className="text-xs uppercase tracking-[0.18em] text-neutral-500">
            governance spine
          </span>
        </div>
        <div className="ml-auto flex items-center gap-4 text-xs">
          {cfg && (
            <span className="hidden text-neutral-500 md:inline">
              <span className="text-neutral-400">{cfg.gateway}</span>
              <span className="mx-2">·</span>
              <span className="text-neutral-400">alice@…</span>
            </span>
          )}
          <button
            onClick={onOpenAdmin}
            className="rounded border border-neutral-800 px-3 py-1 text-neutral-400 hover:border-neutral-600 hover:text-neutral-100 transition"
          >
            admin console
          </button>
          <button
            onClick={onOpenSetup}
            className="rounded border border-neutral-800 px-3 py-1 text-neutral-400 hover:border-neutral-600 hover:text-neutral-100 transition"
          >
            settings
          </button>
        </div>
      </div>
    </header>
  );
}

// ---------------------------------------------------------------------------
// Welcome empty — shown before setup is complete
// ---------------------------------------------------------------------------

function WelcomeEmpty({ onOpenSetup }: { onOpenSetup: () => void }) {
  return (
    <div className="mx-auto max-w-xl px-6 py-24 text-center">
      <h1 className="text-3xl font-semibold tracking-tight">
        Every AI action,{" "}
        <span className="bg-gradient-to-r from-violet-400 to-emerald-400 bg-clip-text text-transparent">
          accountable.
        </span>
      </h1>
      <p className="mt-4 text-sm text-neutral-400">
        Four-word thesis:{" "}
        <span className="text-neutral-200">
          attributable · scoped · auditable · revocable
        </span>
        . Point this demo at a running Dominion gateway and watch them hold up
        under a real hero flow.
      </p>
      <button
        onClick={onOpenSetup}
        className="mt-8 rounded-lg bg-neutral-100 px-5 py-2.5 text-sm font-medium text-neutral-900 hover:bg-white transition"
      >
        Configure demo →
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Setup sheet — five fields, saved to localStorage
// ---------------------------------------------------------------------------

function SetupSheet({
  initial,
  onClose,
  onSave,
}: {
  initial: DemoConfig | null;
  onClose: () => void;
  onSave: (c: DemoConfig) => void;
}) {
  const [form, setForm] = useState<DemoConfig>(
    initial || {
      gateway: "http://localhost:3000",
      admin: "",
      aliceID: "",
      astridID: "",
      aliceMail: "alice@example.com",
    }
  );
  const ready =
    form.gateway.trim() &&
    form.admin.trim() &&
    form.aliceID.trim() &&
    form.astridID.trim();

  return (
    <div className="fixed inset-0 z-30 flex items-center justify-center bg-neutral-950/80 px-4 backdrop-blur-sm">
      <div className="w-full max-w-md rounded-xl border border-neutral-800 bg-neutral-900 p-6 shadow-2xl">
        <div className="mb-5">
          <h2 className="text-base font-semibold">Configure demo</h2>
          <p className="mt-1 text-xs text-neutral-500">
            Saved in <code>localStorage</code>. Grab the IDs from
            <code className="mx-1">scripts/smoke-test.sh</code>'s output on the
            droplet.
          </p>
        </div>
        <div className="space-y-3 text-sm">
          <Field
            label="Gateway URL"
            hint="http://<droplet-ip>:3000"
            value={form.gateway}
            onChange={(v) => setForm({ ...form, gateway: v })}
          />
          <Field
            label="Admin bearer token"
            hint="DOMINION_ADMIN_TOKEN from infra/.env"
            type="password"
            value={form.admin}
            onChange={(v) => setForm({ ...form, admin: v })}
          />
          <Field
            label="Alice — user id"
            hint="uuid"
            mono
            value={form.aliceID}
            onChange={(v) => setForm({ ...form, aliceID: v })}
          />
          <Field
            label="Alice — email"
            hint="used in the UI copy only"
            value={form.aliceMail}
            onChange={(v) => setForm({ ...form, aliceMail: v })}
          />
          <Field
            label="Astrid — agent id"
            hint="uuid"
            mono
            value={form.astridID}
            onChange={(v) => setForm({ ...form, astridID: v })}
          />
        </div>
        <div className="mt-6 flex items-center justify-end gap-2">
          <button
            onClick={onClose}
            className="rounded px-3 py-1.5 text-sm text-neutral-400 hover:text-neutral-100"
          >
            cancel
          </button>
          <button
            disabled={!ready}
            onClick={() => onSave(form)}
            className="rounded-lg bg-neutral-100 px-4 py-1.5 text-sm font-medium text-neutral-900 hover:bg-white disabled:opacity-40 transition"
          >
            save
          </button>
        </div>
      </div>
    </div>
  );
}

function Field({
  label,
  hint,
  value,
  onChange,
  type = "text",
  mono,
}: {
  label: string;
  hint?: string;
  value: string;
  onChange: (v: string) => void;
  type?: string;
  mono?: boolean;
}) {
  return (
    <label className="block">
      <span className="text-xs text-neutral-400">{label}</span>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={`mt-1 w-full rounded border border-neutral-800 bg-neutral-950 px-2.5 py-1.5 text-sm outline-none focus:border-neutral-600 ${
          mono ? "font-mono" : ""
        }`}
      />
      {hint && <span className="mt-1 block text-[11px] text-neutral-500">{hint}</span>}
    </label>
  );
}

// ---------------------------------------------------------------------------
// Stage — three panes. Wired pane-by-pane in the next commits.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// useAuditFeed — polls /admin/audit every POLL_MS and returns the most
// recent `limit` events, newest first. `sinceMs` bounds the query window.
// ---------------------------------------------------------------------------

const POLL_MS = 2000;
const AUDIT_WINDOW_MS = 10 * 60 * 1000; // last 10 minutes

function useAuditFeed(cfg: DemoConfig, limit = 80) {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const alive = useRef(true);

  const tick = useCallback(async () => {
    const from = new Date(Date.now() - AUDIT_WINDOW_MS)
      .toISOString()
      .replace(/\.\d+Z$/, "Z");
    try {
      const resp = await gfetch<{ events: AuditEvent[] }>(
        `/admin/audit?from=${encodeURIComponent(from)}`,
        { gateway: cfg.gateway, admin: cfg.admin }
      );
      if (!alive.current) return;
      const fresh = (resp.events || []).slice(-limit).reverse();
      setEvents(fresh);
      setErr(null);
    } catch (e: any) {
      if (alive.current) setErr(String(e?.message || e));
    }
  }, [cfg.gateway, cfg.admin, limit]);

  useEffect(() => {
    alive.current = true;
    tick();
    const h = setInterval(tick, POLL_MS);
    return () => {
      alive.current = false;
      clearInterval(h);
    };
  }, [tick]);

  return { events, err, refresh: tick };
}

function Stage({ cfg }: { cfg: DemoConfig }) {
  const { events, err } = useAuditFeed(cfg);
  const aliceLocal = cfg.aliceMail.split("@")[0] || "alice";

  return (
    <main className="mx-auto max-w-[1600px] px-6 py-6">
      <GovernanceBanner events={events} />
      {err && (
        <div className="mt-4 rounded-lg border border-red-500/40 bg-red-500/10 px-4 py-2 text-xs text-red-300">
          audit poll error · {err}
        </div>
      )}
      <div className="mt-6 grid gap-6 lg:grid-cols-[320px_minmax(0,1fr)_360px]">
        <PaneSkeleton title="Astrid" subtitle="the AI agent" />
        <PaneSkeleton
          title={`${aliceLocal}'s inbox`}
          subtitle="approval queue"
        />
        <AuditPane events={events} />
      </div>
    </main>
  );
}

// ---------------------------------------------------------------------------
// Audit pane — live-streaming, colour-coded by actor kind.
// ---------------------------------------------------------------------------

function AuditPane({ events }: { events: AuditEvent[] }) {
  return (
    <section className="flex max-h-[75vh] flex-col rounded-xl border border-neutral-800 bg-neutral-900/60">
      <header className="flex items-baseline justify-between border-b border-neutral-800 px-5 py-3">
        <h3 className="text-sm font-semibold">Audit stream</h3>
        <span className="flex items-center gap-2 text-[11px] uppercase tracking-wider text-neutral-500">
          <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-emerald-400" />
          {events.length} · Ed25519 signed
        </span>
      </header>
      <ol className="flex-1 overflow-y-auto">
        {events.length === 0 ? (
          <li className="px-5 py-8 text-center text-xs text-neutral-500">
            waiting for the first event…
          </li>
        ) : (
          events.map((e) => (
            <li
              key={e.id}
              className="border-b border-neutral-900 px-5 py-3 text-xs transition hover:bg-neutral-900/60"
            >
              <div className="flex items-center gap-2">
                <span className={"h-1.5 w-1.5 rounded-full " + decisionDot(e.decision)} />
                <span
                  className={
                    "rounded border px-1.5 py-px font-mono text-[10px] " +
                    actorChipClass(e.actor)
                  }
                >
                  {shortId(e.actor, 6)}
                </span>
                <span className="text-neutral-400">{e.action}</span>
                <span className="ml-auto text-[10px] text-neutral-600">
                  {relTime(e.timestamp)}
                </span>
              </div>
              {e.resource && (
                <div className="mt-1 truncate pl-4 font-mono text-[10px] text-neutral-500">
                  {e.resource}
                </div>
              )}
            </li>
          ))
        )}
      </ol>
    </section>
  );
}

function PaneSkeleton({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <section className="rounded-xl border border-neutral-800 bg-neutral-900/60 p-5">
      <header className="mb-3 flex items-baseline justify-between">
        <h3 className="text-sm font-semibold">{title}</h3>
        <span className="text-[11px] uppercase tracking-wider text-neutral-500">
          {subtitle}
        </span>
      </header>
      <p className="text-xs text-neutral-500">
        wired in the next commit.
      </p>
    </section>
  );
}

// ---------------------------------------------------------------------------
// Governance banner — four words, currently inert. Lighting logic comes
// online once the audit feed is polling in the next commit.
// ---------------------------------------------------------------------------

const WORDS = ["attributable", "scoped", "auditable", "revocable"] as const;
type Word = (typeof WORDS)[number];

function GovernanceBanner({ events }: { events: AuditEvent[] }) {
  const lit: Record<Word, boolean> = useMemo(() => {
    const hasAgent = events.some((e) => e.actor?.startsWith("agent:"));
    const hasAudit = events.length > 0;
    const hasScoped = events.some(
      (e) => e.decision === "deny" || (e.resource || "").startsWith("document:")
    );
    const hasRevoke = events.some(
      (e) =>
        e.action?.includes("revoke") ||
        (e.resource || "").startsWith("agent:") ||
        (e.resource || "").startsWith("user:")
    );
    return {
      attributable: hasAgent,
      scoped: hasScoped,
      auditable: hasAudit,
      revocable: hasRevoke,
    };
  }, [events]);

  return (
    <div className="flex items-center gap-2 rounded-xl border border-neutral-800 bg-neutral-900/60 px-5 py-3">
      <span className="text-[11px] uppercase tracking-[0.2em] text-neutral-500">
        every AI action is
      </span>
      <div className="ml-2 flex flex-wrap gap-2">
        {WORDS.map((w) => (
          <GovernanceWord key={w} word={w} lit={lit[w]} />
        ))}
      </div>
    </div>
  );
}

function GovernanceWord({ word, lit }: { word: Word; lit: boolean }) {
  return (
    <span
      className={
        "rounded-full border px-3 py-0.5 text-xs font-medium transition-all duration-500 " +
        (lit
          ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-300 shadow-[0_0_20px_-8px_rgba(16,185,129,0.6)]"
          : "border-neutral-800 bg-neutral-950 text-neutral-600")
      }
    >
      {word}
    </span>
  );
}

