"use client";

import { useEffect, useState } from "react";
import {
  STORAGE_KEYS,
  call,
  getAdminToken,
  getDevPrincipal,
  getGatewayUrl,
} from "../lib/api";

type Tab = "agents" | "users" | "acl" | "audit" | "queue" | "triage";

export default function Dashboard() {
  const [tab, setTab] = useState<Tab>("agents");
  const [gateway, setGateway] = useState("");
  const [adminToken, setAdminToken] = useState("");
  const [devPrincipal, setDevPrincipal] = useState("");
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setGateway(getGatewayUrl());
    setAdminToken(getAdminToken());
    setDevPrincipal(getDevPrincipal());
    setMounted(true);
  }, []);

  const saveConfig = () => {
    localStorage.setItem(STORAGE_KEYS.gatewayUrl, gateway);
    localStorage.setItem(STORAGE_KEYS.adminToken, adminToken);
    localStorage.setItem(STORAGE_KEYS.devPrincipal, devPrincipal);
  };

  if (!mounted) return null;

  return (
    <div className="min-h-screen">
      <header className="border-b border-neutral-200 dark:border-neutral-800 px-6 py-4 bg-white dark:bg-neutral-900">
        <div className="flex items-baseline gap-4 flex-wrap">
          <h1 className="text-xl font-semibold">Dominion Admin</h1>
          <span className="text-xs text-neutral-500">governance spine — MVP</span>
        </div>
        <p className="mt-1 text-xs text-neutral-500 max-w-2xl">
          One console for every admin action — create AI agents, grant or
          revoke access, inspect the signed audit log, approve or reject
          drafts, and one-revoke a compromised identity. Fill in the three
          fields below once per browser; they stay in <code>localStorage</code>.
        </p>
        <div className="mt-3 grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">Gateway URL</span>
            <input
              className="border rounded px-2 py-1 bg-transparent"
              value={gateway}
              onChange={(e) => setGateway(e.target.value)}
              placeholder="http://localhost:3000"
            />
            <span className="text-[11px] text-neutral-500 mt-1">
              Where the Dominion API lives. On a droplet it's
              <code className="ml-1">http://&lt;ip&gt;:3000</code>.
            </span>
          </label>
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">Admin bearer token</span>
            <input
              type="password"
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={adminToken}
              onChange={(e) => setAdminToken(e.target.value)}
            />
            <span className="text-[11px] text-neutral-500 mt-1">
              From <code>infra/.env</code> as
              <code className="mx-1">DOMINION_ADMIN_TOKEN</code>.
              Unlocks every <code>/admin/*</code> route below.
            </span>
          </label>
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">
              View-as principal
            </span>
            <div className="flex gap-2">
              <input
                className="border rounded px-2 py-1 bg-transparent font-mono flex-1"
                value={devPrincipal}
                onChange={(e) => setDevPrincipal(e.target.value)}
                placeholder="user:<uuid>  or  agent:<uuid>"
              />
              <button
                onClick={saveConfig}
                className="px-3 py-1 rounded bg-neutral-900 text-white dark:bg-neutral-100 dark:text-neutral-900"
              >
                save
              </button>
            </div>
            <span className="text-[11px] text-neutral-500 mt-1">
              Lets you act as a user or agent on <code>/me/*</code> routes
              (Queue tab). Leave blank in admin tabs. Requires
              <code className="mx-1">DOMINION_DEV_PRINCIPAL_HEADER=true</code>
              on the gateway.
            </span>
          </label>
        </div>
        <nav className="mt-4 flex gap-4 overflow-x-auto text-sm">
          {(["agents", "users", "acl", "audit", "queue", "triage"] as Tab[]).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={
                "py-1 border-b-2 -mb-px " +
                (tab === t
                  ? "border-neutral-900 dark:border-neutral-100"
                  : "border-transparent text-neutral-500")
              }
            >
              {t}
            </button>
          ))}
        </nav>
      </header>

      <main className="p-6">
        {tab === "agents" && <AgentsTab />}
        {tab === "users" && <UsersTab />}
        {tab === "acl" && <ACLTab />}
        {tab === "audit" && <AuditTab />}
        {tab === "queue" && <QueueTab />}
        {tab === "triage" && <TriageTab />}
      </main>
    </div>
  );
}

// ---------- Shared widgets ----------

function Card({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mb-6 rounded-lg border border-neutral-200 dark:border-neutral-800 bg-white dark:bg-neutral-900">
      <h2 className="px-4 py-2 text-sm font-medium border-b border-neutral-200 dark:border-neutral-800">
        {title}
      </h2>
      <div className="p-4">{children}</div>
    </section>
  );
}

// Err renders an error message from the gateway plus a short,
// user-friendly hint for the common failure codes. The raw message
// still shows up so an operator can copy-paste it into a bug report.
function Err({ msg }: { msg: string | null }) {
  if (!msg) return null;
  const hint = hintFor(msg);
  return (
    <div className="mt-2 text-sm">
      <p className="text-red-600 dark:text-red-400 whitespace-pre-wrap font-mono text-xs">
        {msg}
      </p>
      {hint && (
        <p className="mt-1 text-neutral-600 dark:text-neutral-400 text-xs">
          {hint}
        </p>
      )}
    </div>
  );
}

function hintFor(msg: string): string | null {
  if (/\b401\b/.test(msg) && /admin/i.test(msg)) {
    return "The admin bearer token is missing or wrong. Paste a valid DOMINION_ADMIN_TOKEN at the top and click save.";
  }
  if (/\b401\b/.test(msg) || /unauthorized/i.test(msg)) {
    return "Not authenticated. For /me/* routes, set a View-as principal at the top (user:<uuid> or agent:<uuid>) and click save.";
  }
  if (/\b403\b/.test(msg) || /forbidden/i.test(msg)) {
    return "The principal has no tuple for this action. Grant it in the ACL tab, or switch View-as to someone with access.";
  }
  if (/\b404\b/.test(msg) || /not_found/i.test(msg)) {
    return "The resource doesn't exist, or has been soft-deleted. Double-check the uuid.";
  }
  if (/\b409\b/.test(msg) || /not_pending/i.test(msg)) {
    return "The draft is already sent or rejected; nothing to do.";
  }
  if (/schema_violation/i.test(msg)) {
    return "The document body didn't match its schema. Check the details[] field for the exact path + message.";
  }
  if (/admin_disabled/i.test(msg)) {
    return "DOMINION_ADMIN_TOKEN isn't configured on the gateway. Set it in infra/.env and redeploy.";
  }
  if (/cors/i.test(msg) || /failed to fetch/i.test(msg) || /network/i.test(msg)) {
    return "Browser blocked the request. Make sure DOMINION_CORS_ORIGIN on the gateway matches the URL in your address bar exactly (trailing slash, http vs https).";
  }
  return null;
}

function Btn(
  props: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "danger" | "ghost" }
) {
  const { variant = "primary", className = "", ...rest } = props;
  const styles = {
    primary:
      "bg-neutral-900 text-white dark:bg-neutral-100 dark:text-neutral-900 hover:opacity-90",
    danger: "bg-red-600 text-white hover:bg-red-700",
    ghost:
      "border border-neutral-300 dark:border-neutral-700 hover:bg-neutral-100 dark:hover:bg-neutral-800",
  }[variant];
  return (
    <button
      {...rest}
      className={`px-3 py-1 rounded text-sm disabled:opacity-50 ${styles} ${className}`}
    />
  );
}

// ---------- Agents ----------

type Agent = {
  id: string;
  display_name: string;
  thumbprint: string;
  owner_user_id?: string;
  active: boolean;
  created_at: string;
};

function AgentsTab() {
  const [displayName, setDisplayName] = useState("Astrid");
  const [ownerUserId, setOwnerUserId] = useState("");
  const [issued, setIssued] = useState<any>(null);
  const [err, setErr] = useState<string | null>(null);
  const [revokeId, setRevokeId] = useState("");

  const create = async () => {
    setErr(null);
    try {
      const res = await call<any>("/admin/agents", {
        admin: true,
        body: {
          display_name: displayName,
          owner_user_id: ownerUserId || undefined,
        },
      });
      setIssued(res);
    } catch (e: any) {
      setErr(String(e.message || e));
    }
  };

  const revoke = async () => {
    setErr(null);
    try {
      await call(`/admin/agents/${revokeId}`, { admin: true, method: "DELETE" });
      setIssued({ status: "revoked", id: revokeId });
    } catch (e: any) {
      setErr(String(e.message || e));
    }
  };

  return (
    <>
      <p className="text-sm text-neutral-600 dark:text-neutral-400 mb-4 max-w-3xl">
        AI agents are first-class identities in Dominion — each one gets an
        X.509 certificate signed by the internal CA and its own ACL
        footprint. A user's <i>personal assistant</i> is just an agent
        whose <code>owner_user_id</code> points to that user; the triage
        engine then auto-grants the PA reader access to incoming email.
      </p>

      <Card title="Create AI agent">
        <p className="text-xs text-neutral-500 mb-3">
          Issues a new cert + private key. The private key is returned
          <b> once</b>; copy it to the machine that will run the agent.
          Leaks the private key and anyone can impersonate the agent
          until you revoke it.
        </p>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">Display name</span>
            <input
              className="border rounded px-2 py-1 bg-transparent"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
            />
            <span className="text-[11px] text-neutral-500 mt-1">
              Human-friendly label for audit reports, e.g. "Astrid".
            </span>
          </label>
          <label className="flex flex-col md:col-span-2">
            <span className="text-xs text-neutral-500">Owner user id (optional)</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={ownerUserId}
              onChange={(e) => setOwnerUserId(e.target.value)}
              placeholder="uuid"
            />
            <span className="text-[11px] text-neutral-500 mt-1">
              When set, this agent becomes the user's PA and receives
              reader access on every email ingested into their mailbox.
              Leave blank for a firm-level or specialist agent.
            </span>
          </label>
        </div>
        <div className="mt-3">
          <Btn onClick={create}>create agent · issue cert (one-time reveal)</Btn>
        </div>
        <Err msg={err} />
      </Card>

      {issued && (
        <Card title="Result">
          <p className="text-xs text-neutral-500 mb-2">
            <b>Save <code>cert_pem</code> and <code>private_key_pem</code> now</b>
            {" — "}the gateway never stores the private key. Also save
            <code className="mx-1">ca_cert_pem</code>: the agent uses it
            to verify the gateway's TLS cert on <code>:3443</code>.
          </p>
          <pre className="text-xs overflow-auto p-2 bg-neutral-50 dark:bg-neutral-950 rounded max-h-[400px]">
            {JSON.stringify(issued, null, 2)}
          </pre>
        </Card>
      )}

      <Card title="Revoke agent (one-revoke)">
        <p className="text-xs text-neutral-500 mb-3">
          Three atomic effects: (1) the agent row flips to
          <code className="mx-1">active=false</code>, (2) its cert stops
          authenticating on the next request, (3) every FGA tuple where
          this agent is the subject is deleted. One-way; issue a new
          agent to replace it.
        </p>
        <div className="flex gap-2 items-end">
          <label className="flex-1 flex flex-col text-sm">
            <span className="text-xs text-neutral-500">Agent id</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={revokeId}
              onChange={(e) => setRevokeId(e.target.value)}
              placeholder="uuid (from the create result above, or from the Audit tab)"
            />
          </label>
          <Btn variant="danger" onClick={revoke} disabled={!revokeId}>
            revoke · one-way
          </Btn>
        </div>
      </Card>
    </>
  );
}

// ---------- Users ----------

function UsersTab() {
  const [email, setEmail] = useState("alice@example.com");
  const [users, setUsers] = useState<any[]>([]);
  const [revokeId, setRevokeId] = useState("");
  const [err, setErr] = useState<string | null>(null);

  const find = async () => {
    setErr(null);
    try {
      const q = `?filter=${encodeURIComponent(`userName eq "${email}"`)}`;
      const res = await call<any>(`/scim/v2/Users${q}`, { admin: true });
      setUsers(res.Resources || []);
    } catch (e: any) {
      setErr(String(e.message || e));
    }
  };

  const revoke = async () => {
    setErr(null);
    try {
      const res = await call<any>(`/admin/users/${revokeId}`, {
        admin: true,
        method: "DELETE",
      });
      setUsers([res.user]);
    } catch (e: any) {
      setErr(String(e.message || e));
    }
  };

  return (
    <>
      <p className="text-sm text-neutral-600 dark:text-neutral-400 mb-4 max-w-3xl">
        Humans in Dominion are provisioned via Entra ID SCIM and identified
        by their email. This tab looks them up by email and performs the
        one-revoke offboarding primitive — the same three-step atomic
        effect as agent revoke, plus a session kill for active browser
        logins.
      </p>

      <Card title="Look up user (SCIM)">
        <p className="text-xs text-neutral-500 mb-3">
          Reads from the SCIM v2 <code>/Users</code> endpoint with
          <code className="mx-1">userName eq "..."</code> filter.
          Returns the user's id (paste into the revoke box below) and
          their active flag.
        </p>
        <div className="flex gap-2 items-end">
          <label className="flex-1 flex flex-col text-sm">
            <span className="text-xs text-neutral-500">userName (email)</span>
            <input
              className="border rounded px-2 py-1 bg-transparent"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="alice@example.com"
            />
          </label>
          <Btn onClick={find}>find · SCIM lookup</Btn>
        </div>
        <Err msg={err} />
      </Card>

      {users.length > 0 && (
        <Card title={`${users.length} match${users.length === 1 ? "" : "es"}`}>
          <table className="w-full text-sm">
            <thead className="text-xs text-neutral-500">
              <tr>
                <th className="text-left p-1">id</th>
                <th className="text-left p-1">userName</th>
                <th className="text-left p-1">active</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id} className="border-t border-neutral-200 dark:border-neutral-800">
                  <td className="p-1 font-mono text-xs">{u.id}</td>
                  <td className="p-1">{u.userName}</td>
                  <td className={"p-1 " + (u.active ? "" : "text-red-600")}>
                    {String(u.active)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      <Card title="Revoke user (one-revoke)">
        <p className="text-xs text-neutral-500 mb-3">
          Four atomic effects: (1) user row flips to
          <code className="mx-1">active=false</code>, (2) every active
          session is terminated, (3) every FGA tuple where this user is
          the subject is deleted, (4) next SCIM request for this user
          surfaces the inactive state to the upstream IdP. One-way;
          re-provisioning requires a new SCIM POST.
        </p>
        <div className="flex gap-2 items-end">
          <label className="flex-1 flex flex-col text-sm">
            <span className="text-xs text-neutral-500">User id</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={revokeId}
              onChange={(e) => setRevokeId(e.target.value)}
              placeholder="uuid (copy from the find results above)"
            />
          </label>
          <Btn variant="danger" onClick={revoke} disabled={!revokeId}>
            revoke · one-way
          </Btn>
        </div>
      </Card>
    </>
  );
}

// ---------- ACL ----------

function ACLTab() {
  const [principal, setPrincipal] = useState("agent:");
  const [relation, setRelation] = useState<"reader" | "writer" | "owner">("reader");
  const [resource, setResource] = useState("document:");
  const [status, setStatus] = useState<any>(null);
  const [err, setErr] = useState<string | null>(null);

  const write = async (op: "grant" | "revoke") => {
    setErr(null);
    try {
      const res = await call<any>(`/admin/acl/${op}`, {
        admin: true,
        body: { principal, relation, resource },
      });
      setStatus({ op, ...res });
    } catch (e: any) {
      setErr(String(e.message || e));
    }
  };

  return (
    <>
      <p className="text-sm text-neutral-600 dark:text-neutral-400 mb-4 max-w-3xl">
        Dominion's ACL is Zanzibar-style relationship tuples in OpenFGA.
        Every tuple is a three-part statement:
        <b className="mx-1">principal</b> has <b>relation</b> on
        <b className="mx-1">resource</b>. Granting a tuple gives the
        principal access; revoking removes it. The triage engine and
        email ingester write owner/reader tuples automatically; use this
        tab for manual adjustments or ad-hoc sharing.
      </p>
      <Card title="Grant / revoke ACL tuple">
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">Principal</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={principal}
              onChange={(e) => setPrincipal(e.target.value)}
              placeholder="user:<uuid>  or  agent:<uuid>"
            />
            <span className="text-[11px] text-neutral-500 mt-1">
              Who gets the relation. Type prefix is required.
            </span>
          </label>
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">Relation</span>
            <select
              className="border rounded px-2 py-1 bg-transparent"
              value={relation}
              onChange={(e) => setRelation(e.target.value as any)}
            >
              <option value="reader">reader</option>
              <option value="writer">writer</option>
              <option value="owner">owner</option>
            </select>
            <span className="text-[11px] text-neutral-500 mt-1">
              <b>reader</b> = GET · <b>writer</b> = PATCH/approve ·
              <b className="ml-1">owner</b> = writer + transfer.
            </span>
          </label>
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">Resource</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={resource}
              onChange={(e) => setResource(e.target.value)}
              placeholder="document:<uuid>"
            />
            <span className="text-[11px] text-neutral-500 mt-1">
              What the relation is on. MVP supports
              <code className="mx-1">document:&lt;uuid&gt;</code>.
            </span>
          </label>
        </div>
        <div className="flex gap-2 mt-3">
          <Btn onClick={() => write("grant")}>grant · writes tuple</Btn>
          <Btn variant="danger" onClick={() => write("revoke")}>
            revoke · deletes tuple
          </Btn>
        </div>
        <Err msg={err} />
        {status && (
          <pre className="mt-3 text-xs p-2 bg-neutral-50 dark:bg-neutral-950 rounded">
            {JSON.stringify(status, null, 2)}
          </pre>
        )}
      </Card>
    </>
  );
}

// ---------- Audit ----------

function AuditTab() {
  const [actor, setActor] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [bundle, setBundle] = useState<any>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    const fiveMinsAgo = new Date(Date.now() - 5 * 60 * 1000).toISOString();
    setFrom(fiveMinsAgo.replace(/\.\d+Z$/, "Z"));
  }, []);

  const fetchBundle = async () => {
    setErr(null);
    try {
      const q = new URLSearchParams();
      if (actor) q.set("actor", actor);
      if (from) q.set("from", from);
      if (to) q.set("to", to);
      const res = await call<any>(`/admin/audit?${q.toString()}`, { admin: true });
      setBundle(res);
    } catch (e: any) {
      setErr(String(e.message || e));
    }
  };

  return (
    <>
      <p className="text-sm text-neutral-600 dark:text-neutral-400 mb-4 max-w-3xl">
        Every request through the gateway is recorded in an append-only,
        per-entry Ed25519-signed log. Export a filtered slice here; the
        bundle includes the current public key inline, so a compliance
        officer can verify every signature offline.
      </p>

      <Card title="Audit export">
        <p className="text-xs text-neutral-500 mb-3">
          All filters are optional. The default <i>from</i> is 5 minutes
          ago. Capped at 1000 entries per export — narrow the time range
          or filter by actor if you hit the cap.
        </p>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">actor</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={actor}
              onChange={(e) => setActor(e.target.value)}
              placeholder="agent:<uuid>  or  user:<uuid>"
            />
            <span className="text-[11px] text-neutral-500 mt-1">
              Exact match. Leave blank for every actor.
            </span>
          </label>
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">from</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
              placeholder="2026-04-18T00:00:00Z"
            />
            <span className="text-[11px] text-neutral-500 mt-1">
              RFC3339 UTC timestamp. Inclusive.
            </span>
          </label>
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">to (optional)</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={to}
              onChange={(e) => setTo(e.target.value)}
              placeholder="blank = now"
            />
            <span className="text-[11px] text-neutral-500 mt-1">
              RFC3339 UTC. Exclusive.
            </span>
          </label>
        </div>
        <div className="mt-3">
          <Btn onClick={fetchBundle}>export · signed JSON bundle</Btn>
        </div>
        <Err msg={err} />
      </Card>

      {bundle && bundle.count === 0 && (
        <Card title="Empty result">
          <p className="text-xs text-neutral-500">
            No events matched. Widen the time range, clear the actor
            filter, or run a request through the gateway (e.g. the
            Triage tab) and try again.
          </p>
        </Card>
      )}

      {bundle && bundle.count > 0 && (
        <Card title={`${bundle.count} entries · key_id ${bundle.key_id}`}>
          <p className="text-xs text-neutral-500 mb-3">
            <b>allow</b> = request succeeded · <b>deny</b> = 401 / 403 · <b>error</b> = 5xx / malformed.
          </p>
          <div className="max-h-[480px] overflow-auto">
            <table className="w-full text-xs">
              <thead className="text-neutral-500 sticky top-0 bg-white dark:bg-neutral-900">
                <tr>
                  <th className="text-left p-1">timestamp</th>
                  <th className="text-left p-1">actor</th>
                  <th className="text-left p-1">action</th>
                  <th className="text-left p-1">resource</th>
                  <th className="text-left p-1">decision</th>
                </tr>
              </thead>
              <tbody>
                {bundle.events.map((e: any) => (
                  <tr key={e.id} className="border-t border-neutral-200 dark:border-neutral-800">
                    <td className="p-1 font-mono">{e.timestamp}</td>
                    <td className="p-1 font-mono">{e.actor}</td>
                    <td className="p-1">{e.action}</td>
                    <td className="p-1 font-mono truncate max-w-[280px]">{e.resource}</td>
                    <td
                      className={
                        "p-1 " +
                        (e.decision === "deny"
                          ? "text-red-600"
                          : e.decision === "error"
                          ? "text-amber-600"
                          : "")
                      }
                    >
                      {e.decision}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}
    </>
  );
}

// ---------- Queue ----------

function QueueTab() {
  const [items, setItems] = useState<any[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [result, setResult] = useState<any>(null);

  const refresh = async () => {
    setErr(null);
    try {
      const res = await call<any>("/me/queue", { asPrincipal: true });
      setItems(res.items || []);
    } catch (e: any) {
      setErr(String(e.message || e));
    }
  };

  const act = async (id: string, verb: "approve" | "reject") => {
    setErr(null);
    try {
      const res = await call<any>(`/me/queue/${id}/${verb}`, {
        asPrincipal: true,
        method: "POST",
      });
      setResult({ verb, id, res });
      refresh();
    } catch (e: any) {
      setErr(String(e.message || e));
    }
  };

  return (
    <>
      <p className="text-sm text-neutral-600 dark:text-neutral-400 mb-4 max-w-3xl">
        The approval queue is the governance checkpoint for every email
        an agent drafts. The agent creates the draft autonomously; a
        human must approve before Microsoft Graph receives the send.
        This tab is user-scoped — set <i>View-as principal</i> at the
        top to <code>user:&lt;uuid&gt;</code> to see that user's queue.
      </p>

      <Card title="Approval queue (view-as principal)">
        <p className="text-xs text-neutral-500 mb-3">
          Lists <code>draft.v1</code> documents in
          <code className="mx-1">status=pending</code> that the configured
          view-as principal can read. Needs the view-as field set at the
          top; otherwise every request is unauthenticated.
        </p>
        <Btn onClick={refresh}>refresh · fetches current pending drafts</Btn>
        <Err msg={err} />
      </Card>

      {items.length === 0 && !err && (
        <Card title="Queue is empty">
          <p className="text-xs text-neutral-500">
            Either the view-as principal has no drafts yet, or they were
            all approved / rejected. Trigger a pass in the
            <b className="mx-1">Triage</b> tab to create new ones.
          </p>
        </Card>
      )}

      {items.map((d) => {
        let body: any = {};
        try {
          body = JSON.parse(d.body);
        } catch {
          body = { raw: d.body };
        }
        return (
          <Card key={d.id} title={`${body.subject || "(no subject)"} — ${d.id}`}>
            <p className="text-xs text-neutral-500">
              To {(body.to || []).join(", ")} · generated by{" "}
              <span className="font-mono">{body.generatedByAgent}</span>{" "}
              at {body.generatedAt}
            </p>
            <pre className="my-3 whitespace-pre-wrap text-sm bg-neutral-50 dark:bg-neutral-950 p-3 rounded">
              {body.body}
            </pre>
            <div className="flex gap-2">
              <Btn onClick={() => act(d.id, "approve")}>
                approve · send via Microsoft Graph
              </Btn>
              <Btn variant="ghost" onClick={() => act(d.id, "reject")}>
                reject · mark rejected, no send
              </Btn>
            </div>
          </Card>
        );
      })}

      {result && (
        <Card title="Last action">
          <pre className="text-xs p-2 bg-neutral-50 dark:bg-neutral-950 rounded">
            {JSON.stringify(result, null, 2)}
          </pre>
        </Card>
      )}
    </>
  );
}

// ---------- Triage ----------

function TriageTab() {
  const [stats, setStats] = useState<any>(null);
  const [err, setErr] = useState<string | null>(null);

  const run = async () => {
    setErr(null);
    try {
      const res = await call<any>("/admin/triage/run", {
        admin: true,
        method: "POST",
      });
      setStats(res);
    } catch (e: any) {
      setErr(String(e.message || e));
    }
  };

  return (
    <>
      <p className="text-sm text-neutral-600 dark:text-neutral-400 mb-4 max-w-3xl">
        Triage is the scheduled job that turns ingested email into draft
        replies. For every active PA, it scans the owner's recent
        <code className="mx-1">email.v1</code> documents and asks Claude
        whether each one deserves a draft. The background scheduler runs
        on <code>DOMINION_TRIAGE_INTERVAL</code> (default 5m); this button
        forces an immediate pass.
      </p>
      <Card title="Trigger triage pass">
        <p className="text-xs text-neutral-500 mb-3">
          Idempotent — emails that already have a draft from the same PA
          are skipped. Returns a stats object you can paste into the
          Audit tab's <i>actor</i> filter as
          <code className="mx-1">agent:&lt;uuid&gt;</code> to see what
          each agent did.
        </p>
        <Btn onClick={run}>run triage now · creates drafts for untriaged email</Btn>
        <Err msg={err} />
        {stats && (
          <pre className="mt-3 text-xs p-2 bg-neutral-50 dark:bg-neutral-950 rounded">
            {JSON.stringify(stats, null, 2)}
          </pre>
        )}
      </Card>
    </>
  );
}
