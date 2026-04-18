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
        <div className="mt-3 grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">Gateway URL</span>
            <input
              className="border rounded px-2 py-1 bg-transparent"
              value={gateway}
              onChange={(e) => setGateway(e.target.value)}
              placeholder="http://localhost:3000"
            />
          </label>
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">Admin bearer token</span>
            <input
              type="password"
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={adminToken}
              onChange={(e) => setAdminToken(e.target.value)}
            />
          </label>
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">
              View-as principal (user:&lt;uuid&gt; / agent:&lt;uuid&gt;)
            </span>
            <div className="flex gap-2">
              <input
                className="border rounded px-2 py-1 bg-transparent font-mono flex-1"
                value={devPrincipal}
                onChange={(e) => setDevPrincipal(e.target.value)}
              />
              <button
                onClick={saveConfig}
                className="px-3 py-1 rounded bg-neutral-900 text-white dark:bg-neutral-100 dark:text-neutral-900"
              >
                save
              </button>
            </div>
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

function Err({ msg }: { msg: string | null }) {
  if (!msg) return null;
  return (
    <p className="text-sm text-red-600 dark:text-red-400 whitespace-pre-wrap">{msg}</p>
  );
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
      <Card title="Create AI agent">
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">Display name</span>
            <input
              className="border rounded px-2 py-1 bg-transparent"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
            />
          </label>
          <label className="flex flex-col md:col-span-2">
            <span className="text-xs text-neutral-500">Owner user id (optional)</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={ownerUserId}
              onChange={(e) => setOwnerUserId(e.target.value)}
              placeholder="uuid"
            />
          </label>
        </div>
        <div className="mt-3">
          <Btn onClick={create}>create agent</Btn>
        </div>
        <Err msg={err} />
      </Card>

      {issued && (
        <Card title="Result">
          <pre className="text-xs overflow-auto p-2 bg-neutral-50 dark:bg-neutral-950 rounded max-h-[400px]">
            {JSON.stringify(issued, null, 2)}
          </pre>
          <p className="text-xs text-neutral-500 mt-2">
            Save cert_pem and private_key_pem now — the gateway does not store the private key.
          </p>
        </Card>
      )}

      <Card title="Revoke agent (one-revoke)">
        <div className="flex gap-2 items-end">
          <label className="flex-1 flex flex-col text-sm">
            <span className="text-xs text-neutral-500">Agent id</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={revokeId}
              onChange={(e) => setRevokeId(e.target.value)}
              placeholder="uuid"
            />
          </label>
          <Btn variant="danger" onClick={revoke} disabled={!revokeId}>
            revoke
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
      <Card title="Look up user (SCIM)">
        <div className="flex gap-2 items-end">
          <label className="flex-1 flex flex-col text-sm">
            <span className="text-xs text-neutral-500">userName (email)</span>
            <input
              className="border rounded px-2 py-1 bg-transparent"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </label>
          <Btn onClick={find}>find</Btn>
        </div>
        <Err msg={err} />
      </Card>

      {users.length > 0 && (
        <Card title="Results">
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
                  <td className="p-1">{String(u.active)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      <Card title="Revoke user (one-revoke)">
        <p className="text-xs text-neutral-500 mb-2">
          Deactivates the row, revokes all sessions, strips every FGA tuple where this user is the subject.
        </p>
        <div className="flex gap-2 items-end">
          <label className="flex-1 flex flex-col text-sm">
            <span className="text-xs text-neutral-500">User id</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={revokeId}
              onChange={(e) => setRevokeId(e.target.value)}
            />
          </label>
          <Btn variant="danger" onClick={revoke} disabled={!revokeId}>
            revoke
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
    <Card title="Grant / revoke ACL tuple">
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
        <label className="flex flex-col">
          <span className="text-xs text-neutral-500">Principal</span>
          <input
            className="border rounded px-2 py-1 bg-transparent font-mono"
            value={principal}
            onChange={(e) => setPrincipal(e.target.value)}
          />
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
        </label>
        <label className="flex flex-col">
          <span className="text-xs text-neutral-500">Resource</span>
          <input
            className="border rounded px-2 py-1 bg-transparent font-mono"
            value={resource}
            onChange={(e) => setResource(e.target.value)}
          />
        </label>
      </div>
      <div className="flex gap-2 mt-3">
        <Btn onClick={() => write("grant")}>grant</Btn>
        <Btn variant="danger" onClick={() => write("revoke")}>
          revoke
        </Btn>
      </div>
      <Err msg={err} />
      {status && (
        <pre className="mt-3 text-xs p-2 bg-neutral-50 dark:bg-neutral-950 rounded">
          {JSON.stringify(status, null, 2)}
        </pre>
      )}
    </Card>
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
      <Card title="Audit export">
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 text-sm">
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">actor (e.g. agent:uuid)</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={actor}
              onChange={(e) => setActor(e.target.value)}
            />
          </label>
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">from (RFC3339)</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
            />
          </label>
          <label className="flex flex-col">
            <span className="text-xs text-neutral-500">to (RFC3339, optional)</span>
            <input
              className="border rounded px-2 py-1 bg-transparent font-mono"
              value={to}
              onChange={(e) => setTo(e.target.value)}
            />
          </label>
        </div>
        <div className="mt-3">
          <Btn onClick={fetchBundle}>export</Btn>
        </div>
        <Err msg={err} />
      </Card>

      {bundle && (
        <Card title={`${bundle.count} entries (key_id ${bundle.key_id})`}>
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
      <Card title="Approval queue (view-as principal)">
        <p className="text-xs text-neutral-500 mb-3">
          Lists <code>draft.v1</code> documents where status=pending that the configured
          view-as principal can read.
        </p>
        <Btn onClick={refresh}>refresh</Btn>
        <Err msg={err} />
      </Card>

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
              <span className="font-mono">{body.generatedByAgent}</span> at {body.generatedAt}
            </p>
            <pre className="my-3 whitespace-pre-wrap text-sm">{body.body}</pre>
            <div className="flex gap-2">
              <Btn onClick={() => act(d.id, "approve")}>approve &amp; send</Btn>
              <Btn variant="ghost" onClick={() => act(d.id, "reject")}>
                reject
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
    <Card title="Trigger triage pass">
      <p className="text-xs text-neutral-500 mb-3">
        Runs one triage pass across every active PA. The background scheduler runs the
        same logic on <code>DOMINION_TRIAGE_INTERVAL</code> (default 5m).
      </p>
      <Btn onClick={run}>run now</Btn>
      <Err msg={err} />
      {stats && (
        <pre className="mt-3 text-xs p-2 bg-neutral-50 dark:bg-neutral-950 rounded">
          {JSON.stringify(stats, null, 2)}
        </pre>
      )}
    </Card>
  );
}
