"use client";

import { deviceSystemLabel } from "./device-system";
import { FormEvent, Suspense, useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { AdminNav } from "./admin-nav";
import { useRouter, useSearchParams } from "next/navigation";

type Tab = "概览" | "用户" | "设备" | "节点" | "套餐" | "审计记录";
type UserRow = { id: string; email: string; status: string; activeDevices: number; planName: string | null; subscriptionEndsAt: string | null };
type DeviceRow = { id: string; userId: string; email: string; platform: string; osName?: string | null; osVersion?: string | null; label: string; appVersion: string | null; lastSeenAt: string | null; createdAt: string; revokedAt: string | null };
type Endpoint = { id: string; protocol: string; host: string; port: number; transport: string; serverName: string | null; publicKey: string | null; shortId: string | null; enabled?: boolean };
type NodeRow = { id: string; code: string; name: string; region: string; status: string; healthCheckedAt?: string | null; endpoints: Endpoint[] };
type PlanRow = { id: string; code: string; name: string; tier: "standard" | "premium"; durationDays: number; priceCents: number | null; currency: string; quotaBytes: number | null; maxDevices: number; active: boolean };
type AuditRow = { id: number; actor: string; action: string; targetType: string; targetId: string; createdAt: string };
type Overview = { activeUsers: number; activeDevices: number; onlineNodes: number; activeSubscriptions: number; asOf: string };
type Dialog = "user" | "node" | "plan" | "subscription" | null;

const isDemo = process.env.NEXT_PUBLIC_ADMIN_DEMO_MODE === "true";
const tabs: Tab[] = ["概览", "用户", "设备", "节点", "套餐", "审计记录"];
const demoOverview: Overview = { activeUsers: 128, activeDevices: 214, onlineNodes: 1, activeSubscriptions: 121, asOf: "2026-11-13T10:49:00Z" };
const demoUsers: UserRow[] = [
  { id: "demo-u-0001", email: "demo.one@example.test", status: "active", activeDevices: 2, planName: "开发体验", subscriptionEndsAt: "2026-12-01T00:00:00Z" },
  { id: "demo-u-0002", email: "demo.two@example.test", status: "active", activeDevices: 1, planName: "开发体验", subscriptionEndsAt: "2026-11-30T00:00:00Z" },
  { id: "demo-u-0003", email: "demo.three@example.test", status: "suspended", activeDevices: 0, planName: null, subscriptionEndsAt: null },
];
const demoNodes: NodeRow[] = [{ id: "demo-node-1", code: "DEMO-HK", name: "Demo HK", region: "HK", status: "online", healthCheckedAt: "2026-11-13T10:48:00Z", endpoints: [
  { id: "demo-ep-hy2", protocol: "hysteria2", host: "hk.demo.example.invalid", port: 443, transport: "udp", serverName: "example.invalid", publicKey: null, shortId: null, enabled: true },
  { id: "demo-ep-vless", protocol: "vless_reality", host: "hk.demo.example.invalid", port: 8443, transport: "tcp", serverName: "example.invalid", publicKey: "[演示]", shortId: "[演示]", enabled: true },
]}];
const demoPlans: PlanRow[] = [
  { id: "demo-plan-1", code: "standard-30d", name: "普通档 30 天", tier: "standard", durationDays: 30, priceCents: 3000, currency: "CNY", quotaBytes: null, maxDevices: 1, active: true },
  { id: "demo-plan-2", code: "standard-90d", name: "普通档 90 天", tier: "standard", durationDays: 90, priceCents: 7500, currency: "CNY", quotaBytes: null, maxDevices: 1, active: true },
  { id: "demo-plan-3", code: "standard-365d", name: "普通档 365 天", tier: "standard", durationDays: 365, priceCents: 22500, currency: "CNY", quotaBytes: null, maxDevices: 1, active: true },
  { id: "demo-plan-4", code: "premium-30d", name: "豪华档 30 天", tier: "premium", durationDays: 30, priceCents: 6000, currency: "CNY", quotaBytes: null, maxDevices: 1, active: true },
  { id: "demo-plan-5", code: "premium-90d", name: "豪华档 90 天", tier: "premium", durationDays: 90, priceCents: 15000, currency: "CNY", quotaBytes: null, maxDevices: 1, active: true },
  { id: "demo-plan-6", code: "premium-365d", name: "豪华档 365 天", tier: "premium", durationDays: 365, priceCents: 50000, currency: "CNY", quotaBytes: null, maxDevices: 1, active: true },
];
const demoDevices: DeviceRow[] = [
  { id: "demo-device-01", userId: "demo-u-0001", email: "demo.one@example.test", platform: "ios", label: "iPhone", appVersion: "0.1.0", lastSeenAt: "2026-11-13T10:35:00Z", createdAt: "2026-11-10T06:00:00Z", revokedAt: null },
  { id: "demo-device-02", userId: "demo-u-0002", email: "demo.two@example.test", platform: "macos", label: "MacBook", appVersion: "0.1.0", lastSeenAt: "2026-11-12T18:20:00Z", createdAt: "2026-11-09T05:00:00Z", revokedAt: null },
];
const demoAudit: AuditRow[] = [
  { id: 1, actor: "Owner", action: "node.created", targetType: "node", targetId: "AWS-HK", createdAt: "2026-11-13T09:39:00Z" },
  { id: 2, actor: "Owner", action: "user.created", targetType: "user", targetId: "demo-u-0001", createdAt: "2026-11-13T08:04:00Z" },
  { id: 3, actor: "Operations", action: "device.revoked", targetType: "device", targetId: "demo-u-0003", createdAt: "2026-11-13T06:32:00Z" },
];

export default function AdminPage() {
  return <Suspense fallback={<main className="admin-loading">正在加载后台…</main>}><AdminDashboard /></Suspense>;
}

function AdminDashboard() {
  const router = useRouter();
  const selected = useSearchParams().get("tab");
  const active = tabs.find((tab) => tab === selected) ?? "概览";
  const setActive = (tab: Tab) => router.push(tab === "概览" ? "/admin" : "/admin?tab=" + encodeURIComponent(tab));
  const [query, setQuery] = useState("");
  const [overview, setOverview] = useState<Overview | null>(isDemo ? demoOverview : null);
  const [users, setUsers] = useState<UserRow[]>(isDemo ? demoUsers : []);
  const [devices, setDevices] = useState<DeviceRow[]>(isDemo ? demoDevices : []);
  const [nodes, setNodes] = useState<NodeRow[]>(isDemo ? demoNodes : []);
  const [plans, setPlans] = useState<PlanRow[]>(isDemo ? demoPlans : []);
  const [audit, setAudit] = useState<AuditRow[]>(isDemo ? demoAudit : []);
  const [csrf, setCsrf] = useState("");
  const [operator, setOperator] = useState(isDemo ? "Demo Owner" : "Owner");
  const [loading, setLoading] = useState(!isDemo);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [dialog, setDialog] = useState<Dialog>(null);
  const [subscriptionUser, setSubscriptionUser] = useState<UserRow | null>(null);

  const refresh = useCallback(async () => {
    if (isDemo) return;
    setLoading(true);
    setError("");
    try {
      const meResponse = await fetch("/api/v1/admin/me", { credentials: "same-origin", cache: "no-store" });
      if (meResponse.status === 401) {
        router.replace("/admin/login");
        return;
      }
      if (!meResponse.ok) throw new Error("控制 API 暂时不可用");
      const me = await meResponse.json();
      setCsrf(me.csrfToken ?? "");
      setOperator(me.email ?? me.role ?? "Owner");
      const [overviewResponse, usersResponse, devicesResponse, nodesResponse, plansResponse, auditResponse] = await Promise.all([
        fetch("/api/v1/admin/overview", { credentials: "same-origin", cache: "no-store" }),
        fetch("/api/v1/admin/users?limit=100", { credentials: "same-origin", cache: "no-store" }),
        fetch("/api/v1/admin/devices", { credentials: "same-origin", cache: "no-store" }),
        fetch("/api/v1/admin/nodes/records", { credentials: "same-origin", cache: "no-store" }),
        fetch("/api/v1/admin/plans", { credentials: "same-origin", cache: "no-store" }),
        fetch("/api/v1/admin/audit", { credentials: "same-origin", cache: "no-store" }),
      ]);
      if ([overviewResponse, usersResponse, devicesResponse, nodesResponse, plansResponse, auditResponse].some((response) => response.status === 401)) {
        router.replace("/admin/login");
        return;
      }
      if ([overviewResponse, usersResponse, devicesResponse, nodesResponse, plansResponse, auditResponse].some((response) => !response.ok)) throw new Error("读取控制台数据失败");
      const [overviewData, usersData, devicesData, nodesData, plansData, auditData] = await Promise.all([
        overviewResponse.json(), usersResponse.json(), devicesResponse.json(), nodesResponse.json(), plansResponse.json(), auditResponse.json(),
      ]);
      setOverview(overviewData);
      setUsers(usersData.items ?? []);
      setDevices(devicesData.items ?? []);
      setNodes(nodesData.items ?? []);
      setPlans(plansData.items ?? []);
      setAudit(auditData.items ?? []);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "控制台暂时不可用");
    } finally {
      setLoading(false);
    }
  }, [router]);

  useEffect(() => {
    const task = window.setTimeout(() => { void refresh(); }, 0);
    return () => window.clearTimeout(task);
  }, [refresh]);

  const filteredUsers = useMemo(() => users.filter((user) => `${user.email} ${user.id}`.toLowerCase().includes(query.toLowerCase())), [users, query]);

  async function writeApi(path: string, method: string, body?: unknown) {
    if (isDemo) {
      setNotice("演示模式只展示界面，写入操作不会保存。");
      return null;
    }
    const response = await fetch(path, {
      method,
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const payload = response.status === 204 ? null : await response.json().catch(() => null);
    if (!response.ok) throw new Error(payload?.error?.message ?? "操作失败，请重试");
    return payload;
  }

  async function changeUserStatus(user: UserRow) {
    try {
      await writeApi(`/api/v1/admin/users/${encodeURIComponent(user.id)}/status`, "PATCH", { status: user.status === "active" ? "suspended" : "active" });
      setNotice(isDemo ? "演示模式未保存变更。" : "账户状态已更新。");
      if (!isDemo) await refresh();
    } catch (caught) { setNotice(caught instanceof Error ? caught.message : "操作失败"); }
  }

  async function revokeDevice(device: DeviceRow) {
    if (device.revokedAt) return;
    if (!window.confirm(`撤销 ${device.email} 的设备“${device.label}”？此操作会阻止该设备继续获取节点配置。`)) return;
    try {
      await writeApi(`/api/v1/admin/devices/${encodeURIComponent(device.id)}`, "DELETE");
      setNotice(isDemo ? "演示模式未保存变更。" : "设备已撤销；节点代理同步完成前，凭据状态仍会显示为撤销中。");
      if (!isDemo) await refresh();
    } catch (caught) { setNotice(caught instanceof Error ? caught.message : "撤销设备失败"); }
  }

  async function logout() {
    try {
      if (!isDemo) await writeApi("/api/v1/admin/session", "DELETE");
    } finally {
      router.push("/");
    }
  }

  async function submitUser(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    try {
      await writeApi("/api/v1/admin/users", "POST", { email: String(data.get("email")), temporaryPassword: String(data.get("temporaryPassword")), planCode: String(data.get("planCode") ?? "") });
      setDialog(null);
      setNotice(isDemo ? "演示模式未保存新用户。" : "用户已创建。临时密码只显示给当前操作员一次，请通过安全渠道交付。");
      if (!isDemo) await refresh();
    } catch (caught) { setNotice(caught instanceof Error ? caught.message : "创建用户失败"); }
  }

  async function submitNode(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const protocol = String(data.get("protocol"));
    const body = {
      code: String(data.get("code")), name: String(data.get("name")), region: String(data.get("region")), host: String(data.get("host")),
      protocol, port: Number(data.get("port")), transport: protocol === "hysteria2" ? "udp" : "tcp",
      serverName: String(data.get("serverName") ?? ""), publicKey: String(data.get("publicKey") ?? ""), shortId: String(data.get("shortId") ?? ""),
    };
    try {
      await writeApi("/api/v1/admin/nodes/register", "POST", body);
      setDialog(null);
      setNotice(isDemo ? "演示模式未保存节点。" : "节点已登记，状态为待配置。接入节点代理后才能签发凭据。");
      if (!isDemo) await refresh();
    } catch (caught) { setNotice(caught instanceof Error ? caught.message : "创建节点失败"); }
  }

  async function toggleNode(node: NodeRow) {
    const enabled = node.status === "disabled";
    const action = enabled ? "重新启用" : "停用";
    if (!window.confirm(`${action}节点 ${node.name}？停用时该节点的凭据会进入回收状态。`)) return;
    try {
      await writeApi(`/api/v1/admin/nodes/${encodeURIComponent(node.id)}/enabled`, "PATCH", { enabled });
      setNotice(isDemo ? "演示模式未保存变更。" : enabled ? "节点已重新启用，正在等待健康检查。" : "节点已停用，凭据已进入回收队列状态。");
      if (!isDemo) await refresh();
    } catch (caught) { setNotice(caught instanceof Error ? caught.message : "更新节点失败"); }
  }

  async function submitPlan(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const price = Number(data.get("priceYuan"));
    const quota = String(data.get("quotaBytes") ?? "");
    try {
      await writeApi("/api/v1/admin/plans", "POST", { code: String(data.get("code")), name: String(data.get("name")), tier: String(data.get("tier")), durationDays: Number(data.get("durationDays")), priceCents: Math.round(price * 100), maxDevices: Number(data.get("maxDevices")), quotaBytes: quota === "" ? null : Number(quota) });
      setDialog(null);
      setNotice(isDemo ? "演示模式未保存套餐。" : "套餐已创建。");
      if (!isDemo) await refresh();
    } catch (caught) { setNotice(caught instanceof Error ? caught.message : "创建套餐失败"); }
  }

  async function submitSubscription(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!subscriptionUser) return;
    const data = new FormData(event.currentTarget);
    try {
      await writeApi(`/api/v1/admin/users/${encodeURIComponent(subscriptionUser.id)}/subscriptions`, "POST", { planCode: String(data.get("planCode")) });
      setDialog(null);
      setNotice(isDemo ? "演示模式未保存变更。" : `已为 ${subscriptionUser.email} 分配套餐。`);
      if (!isDemo) await refresh();
    } catch (caught) { setNotice(caught instanceof Error ? caught.message : "分配套餐失败"); }
  }

  if (loading) return <main className="admin-loading"><span className="live-dot" /> 正在验证会话并载入 26x 数据…</main>;

  return (
    <main className="admin-shell">
      <AdminNav demo={isDemo} current={active === "概览" ? "/admin" : "/admin?tab=" + active}
        footer={<><div className="admin-avatar">26</div><div><strong>{operator}</strong><small>{isDemo ? "开发演示账户" : "26x 管理员"}</small></div><button onClick={logout}>退出</button></>} />

      <section className="admin-content">
        <header className="admin-topbar"><div><span className="breadcrumb">26x Control /</span> {active}</div><div className="topbar-tools"><span className="env-pill"><i /> {isDemo ? "DEVELOPMENT DEMO" : "LIVE DATA"}</span><button className="icon-button" aria-label="刷新数据" onClick={() => void refresh()}>↻</button><a className="admin-help" href="/support">帮助中心 ↗</a></div></header>
        <div className="admin-page">
          <div className="admin-heading"><div><div className="eyebrow">26x CONTROL PLANE</div><h1>{active === "概览" ? "运营概览" : active}</h1>{active === "概览" && <p>查看用户、设备、订阅与节点状态。</p>}</div>{["用户", "节点"].includes(active) && <button className="primary-action" onClick={() => setDialog(active === "节点" ? "node" : active === "套餐" ? "plan" : "user")}>＋ {active === "节点" ? "新建节点" : active === "套餐" ? "新建套餐" : "新建用户"}</button>}</div>
          {isDemo && <div className="demo-banner"><span className="demo-banner-icon">i</span><span><strong>开发演示数据</strong>　此页面使用本地演示数据，写入操作不会保存。</span><Link href="/">查看 26x 官网 ↗</Link></div>}
          {error && <div className="console-error" role="alert"><span>{error}</span><button onClick={() => void refresh()}>重新载入</button></div>}
          {notice && <div className="console-notice" role="status"><span>{notice}</span><button onClick={() => setNotice("")}>×</button></div>}

          {active === "概览" && <>
            <div className="metric-grid"><Metric title="活跃用户" value={overview?.activeUsers ?? "—"} foot="账号状态为 active" glyph="♙" /><Metric title="已登记设备" value={overview?.activeDevices ?? "—"} foot="未撤销设备" glyph="▣" /><Metric title="在线节点" value={`${overview?.onlineNodes ?? "—"} / ${nodes.length}`} foot="最近健康检查状态" glyph="⌁" /><Metric title="有效订阅" value={overview?.activeSubscriptions ?? "—"} foot="当前有效的服务权限" glyph="◇" /></div>
            <div className="admin-columns"><section className="panel"><PanelHeading title="节点健康" subtitle={overview?.asOf ? `更新于 ${formatTime(overview.asOf)}` : "节点运行状态"} action={isDemo ? undefined : <Link className="subtle-button" href="/admin/nodes">管理节点 →</Link>} /><NodeTable nodes={nodes} /></section><section className="panel activity-panel"><PanelHeading title="最近操作" subtitle="管理后台审计记录" action={<button className="icon-button" onClick={() => setActive("审计记录")} aria-label="查看记录">···</button>} />{audit.slice(0, 3).map((item) => <div className="activity-item" key={item.id}><span className="activity-mark">↗</span><div><strong>{actionLabel(item.action)}</strong><small>{item.actor} · {item.targetId} · {formatTime(item.createdAt)}</small></div></div>)}{audit.length === 0 && <p className="empty-hint">暂时没有审计记录。</p>}<button className="view-all" onClick={() => setActive("审计记录")}>查看全部记录 <span>→</span></button></section></div>
            <section className="panel users-panel"><PanelHeading title="最近用户" subtitle={`${users.length} 个账户`} action={<div className="panel-actions"><SearchBox query={query} setQuery={setQuery} /><button className="subtle-button" onClick={() => setActive("用户")}>全部用户 →</button></div>} /><UserTable rows={filteredUsers.slice(0, 5)} onChangeStatus={changeUserStatus} onAssignPlan={(user) => { setSubscriptionUser(user); setDialog("subscription"); }} /></section>
          </>}

          {active === "用户" && <section className="panel users-panel"><PanelHeading title="用户与设备" subtitle={`${filteredUsers.length} 个匹配账户`} action={<SearchBox query={query} setQuery={setQuery} />} /><UserTable rows={filteredUsers} onChangeStatus={changeUserStatus} onAssignPlan={(user) => { setSubscriptionUser(user); setDialog("subscription"); }} /></section>}
          {active === "设备" && <section className="panel users-panel"><PanelHeading title="已登记设备" subtitle={`${devices.length} 台设备 · 最近 200 条`} /><DeviceTable rows={devices} onRevoke={revokeDevice} /></section>}
          {active === "节点" && <section className="panel users-panel"><PanelHeading title="VPN 节点" subtitle="节点健康状态由节点代理报告；此处只控制启用状态。" action={<button className="primary-action" onClick={() => setDialog("node")}>＋ 添加节点</button>} />{nodes.length ? nodes.map((node) => <NodeCard key={node.id} node={node} onToggle={() => toggleNode(node)} />) : <EmptyState title="还没有节点" body="登记第一台 26x VPN 节点后，它会显示在这里。" />}</section>}
          {active === "套餐" && <section className="panel users-panel"><PanelHeading title="套餐与订阅" subtitle="普通档与豪华档；期限、人民币售价、设备数和流量额度分别设置。" action={<button className="primary-action" onClick={() => setDialog("plan")}>＋ 新建套餐</button>} />{plans.length ? <PlanTable plans={plans} /> : <EmptyState title="还没有套餐" body="可在普通档或豪华档下创建不同有效期的套餐。" />}</section>}
          {active === "审计记录" && <section className="panel users-panel"><PanelHeading title="审计记录" subtitle="最近 100 条敏感操作" />{audit.length ? <AuditList rows={audit} /> : <EmptyState title="暂无审计记录" body="用户、套餐和节点变更会记录在这里。" />}</section>}
        </div>
      </section>

      {dialog && <DialogShell onClose={() => setDialog(null)} title={dialog === "user" ? "创建用户" : dialog === "node" ? "登记 VPN 节点" : dialog === "subscription" ? "分配订阅" : "创建套餐"}>
        {dialog === "user" && <form className="admin-form" onSubmit={submitUser}><label>用户邮箱<input type="email" name="email" autoComplete="off" required /></label><label>临时密码<input type="password" name="temporaryPassword" minLength={16} autoComplete="new-password" required /><small>至少 16 位。创建后通过安全渠道交付给用户。</small></label><label>初始套餐<select name="planCode"><option value="">暂不分配</option>{plans.filter((plan) => plan.active).map((plan) => <option key={plan.id} value={plan.code}>{plan.name} · {plan.durationDays} 天</option>)}</select></label><FormActions onCancel={() => setDialog(null)} submit="创建用户" /></form>}
        {dialog === "node" && <form className="admin-form" onSubmit={submitNode}><div className="form-grid"><label>节点代码<input name="code" placeholder="HK-01" required /></label><label>显示名称<input name="name" placeholder="AWS-HK" required /></label><label>地区代码<input name="region" placeholder="HK" required /></label><label>服务器地址<input name="host" placeholder="vpn.example.com" required /></label></div><label>协议<select name="protocol"><option value="hysteria2">Hysteria2 · UDP</option><option value="vless_reality">VLESS + REALITY · TCP</option></select></label><label>端口<input name="port" type="number" min="1" max="65535" defaultValue="443" required /></label><label>REALITY SNI<input name="serverName" placeholder="仅 VLESS REALITY 需要" /></label><label>REALITY 公钥<input name="publicKey" placeholder="仅 VLESS REALITY 需要" /></label><label>REALITY Short ID<input name="shortId" placeholder="仅 VLESS REALITY 需要" /></label><div className="form-hint">私钥和客户端认证凭据不会通过此表单提交。节点创建后仍需配置节点代理。</div><FormActions onCancel={() => setDialog(null)} submit="登记节点" /></form>}
        {dialog === "plan" && <form className="admin-form" onSubmit={submitPlan}><label>套餐档位<select name="tier" defaultValue="standard"><option value="standard">普通档</option><option value="premium">豪华档</option></select></label><label>套餐代码<input name="code" placeholder="standard-30d" required /></label><label>显示名称<input name="name" placeholder="普通档 30 天" required /></label><div className="form-grid"><label>有效天数<input name="durationDays" type="number" min="1" max="3650" defaultValue="30" required /></label><label>售价（人民币元）<input name="priceYuan" type="number" min="0.01" step="0.01" placeholder="30" required /></label></div><label>最多设备数<input name="maxDevices" type="number" min="1" max="32" defaultValue="1" required /></label><label>流量上限（字节）<input name="quotaBytes" type="number" min="0" placeholder="留空表示不限制" /></label><FormActions onCancel={() => setDialog(null)} submit="创建套餐" /></form>}
        {dialog === "subscription" && <form className="admin-form" onSubmit={submitSubscription}><p className="form-hint">为 {subscriptionUser?.email ?? "用户"} 分配新套餐。现有订阅记录会保留作审计。</p><label>套餐<select name="planCode" required defaultValue=""><option value="" disabled>请选择套餐</option>{plans.filter((plan) => plan.active).map((plan) => <option key={plan.id} value={plan.code}>{plan.name} · {plan.durationDays} 天</option>)}</select></label><FormActions onCancel={() => setDialog(null)} submit="分配套餐" /></form>}
      </DialogShell>}
    </main>
  );
}

function Metric({ title, value, foot, glyph }: { title: string; value: string | number; foot: string; glyph: string }) {
  return <article className="metric-card"><span>{title}</span><strong>{value}</strong><small>{foot}</small><div className="metric-glyph">{glyph}</div></article>;
}

function PanelHeading({ title, subtitle, action }: { title: string; subtitle?: string; action?: React.ReactNode }) {
  return <div className="panel-heading"><div><h2>{title}</h2>{subtitle && <p>{subtitle}</p>}</div>{action}</div>;
}

function SearchBox({ query, setQuery }: { query: string; setQuery: (value: string) => void }) {
  return <label className="search-box"><span>⌕</span><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索邮箱或用户 ID" /></label>;
}

function UserTable({ rows, onChangeStatus, onAssignPlan }: { rows: UserRow[]; onChangeStatus: (user: UserRow) => void; onAssignPlan: (user: UserRow) => void }) {
  return <div className="data-table"><div className="table-head"><span>用户</span><span>套餐</span><span>设备</span><span>状态</span><span>到期时间</span><span>操作</span></div>{rows.map((user) => <div className="user-row" key={user.id}><div className="table-user"><span>{user.email.slice(0, 1).toUpperCase()}</span><div><strong>{user.email}</strong><small>{user.id}</small></div></div><span>{user.planName ?? "无有效套餐"}</span><span>{user.activeDevices}</span><span className={user.status === "active" ? "user-status" : "user-status suspended"}><i />{user.status === "active" ? "正常" : "暂停"}</span><span>{user.subscriptionEndsAt ? formatDate(user.subscriptionEndsAt) : "—"}</span><div className="user-actions"><button className="row-menu" onClick={() => onAssignPlan(user)} aria-label={`为 ${user.email} 分配套餐`}>套餐</button><button className="row-menu" onClick={() => onChangeStatus(user)} aria-label={user.status === "active" ? "暂停账户" : "恢复账户"}>{user.status === "active" ? "暂停" : "恢复"}</button></div></div>)}{rows.length === 0 && <div className="table-empty">没有匹配的用户</div>}</div>;
}

function DeviceTable({ rows, onRevoke }: { rows: DeviceRow[]; onRevoke: (device: DeviceRow) => void }) {
  return <div className="data-table device-table"><div className="table-head"><span>设备</span><span>账户</span><span>操作系统</span><span>最近在线</span><span>登记时间</span><span>状态 / 操作</span></div>{rows.map((device) => <div className="user-row device-row" key={device.id}><div className="table-user"><span>▣</span><div><strong>{device.label}</strong><small>{device.id.slice(0, 12)}… · {device.appVersion ?? "版本未知"}</small></div></div><span>{device.email}</span><span>{deviceSystemLabel(device)}</span><span>{device.lastSeenAt ? formatTime(device.lastSeenAt) : "从未"}</span><span>{formatDate(device.createdAt)}</span><span className="device-actions"><span className={device.revokedAt ? "user-status suspended" : "user-status"}><i />{device.revokedAt ? "已撤销" : "有效"}</span>{!device.revokedAt && <button className="row-menu" onClick={() => onRevoke(device)}>撤销</button>}</span></div>)}{rows.length === 0 && <div className="table-empty">还没有登记设备</div>}</div>;
}

function NodeTable({ nodes }: { nodes: NodeRow[] }) {
  return <div className="node-table"><div className="table-head"><span>节点</span><span>协议 / 入口</span><span>状态</span><span>健康检查</span></div>{nodes.map((node) => <div className="node-row" key={node.id}><div className="node-name"><span className="flag">{node.region === "HK" ? "🇭🇰" : "◇"}</span><div><strong>{node.name}</strong><small>{node.region} · {node.code}</small></div></div><div>{node.endpoints.map((endpoint) => <span className="protocol-chip" key={endpoint.id}>{protocolLabel(endpoint.protocol)} · {endpoint.transport.toUpperCase()} {endpoint.port}</span>)}</div><span className={node.status === "online" ? "health" : "health health-warn"}><i />{statusLabel(node.status)}</span><strong className="latency">{node.healthCheckedAt ? formatTime(node.healthCheckedAt) : "待检查"}</strong></div>)}</div>;
}

function NodeCard({ node, onToggle }: { node: NodeRow; onToggle: () => void }) {
  return <article className="node-detail-card"><div className="node-detail-head"><span className="flag">{node.region === "HK" ? "🇭🇰" : "◇"}</span><div><strong>{node.name}</strong><small>{node.code} · {node.region} · {node.id}</small></div><span className={node.status === "online" ? "health" : "health health-warn"}><i />{statusLabel(node.status)}</span><button className="subtle-button" onClick={onToggle}>{node.status === "disabled" ? "重新启用" : "停用节点"}</button></div><div className="endpoint-grid">{node.endpoints.map((endpoint) => <div key={endpoint.id}><span>{protocolLabel(endpoint.protocol)}</span><strong>{endpoint.transport.toUpperCase()} :{endpoint.port}</strong><small>{endpoint.host}</small><small>{endpoint.protocol === "vless_reality" ? `SNI ${endpoint.serverName ?? "未设置"}` : "认证凭据由节点代理管理"}</small></div>)}{node.endpoints.length === 0 && <div><span>节点入口</span><strong>待配置</strong><small>添加端点后可发布到客户端。</small></div>}</div></article>;
}

function PlanTable({ plans }: { plans: PlanRow[] }) {
  return <div className="data-table plan-table"><div className="table-head"><span>档位</span><span>套餐</span><span>代码</span><span>有效期</span><span>售价</span><span>设备数</span><span>流量</span><span>状态</span></div>{plans.map((plan) => <div className="user-row plan-row" key={plan.id}><span className={`plan-tier ${plan.tier}`}>{plan.tier === "premium" ? "豪华" : "普通"}</span><div><strong>{plan.name}</strong><small>{plan.id}</small></div><span>{plan.code}</span><span>{plan.durationDays} 天</span><strong>{plan.priceCents == null ? "未定价" : `¥${(plan.priceCents / 100).toFixed(2)}`}</strong><span>{plan.maxDevices}</span><span>{plan.quotaBytes != null ? `${(plan.quotaBytes / 1_000_000_000).toFixed(0)} GB` : "不限"}</span><span className={plan.active ? "user-status" : "user-status suspended"}><i />{plan.active ? "启用" : "停用"}</span></div>)}</div>;
}

function AuditList({ rows }: { rows: AuditRow[] }) {
  return <div className="audit-list">{rows.map((item) => <div key={item.id}><strong>{actionLabel(item.action)}</strong><span>{item.actor} / {item.targetType} / {item.targetId}</span><time>{formatTime(item.createdAt)}</time></div>)}</div>;
}

function EmptyState({ title, body }: { title: string; body: string }) {
  return <div className="empty-state"><span>◇</span><h2>{title}</h2><p>{body}</p></div>;
}

function DialogShell({ title, children, onClose }: { title: string; children: React.ReactNode; onClose: () => void }) {
  return <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}><section className="dialog-card" role="dialog" aria-modal="true" aria-label={title}><div className="dialog-heading"><h2>{title}</h2><button onClick={onClose} aria-label="关闭">×</button></div>{children}</section></div>;
}

function FormActions({ onCancel, submit }: { onCancel: () => void; submit: string }) {
  return <div className="form-actions"><button type="button" className="subtle-button" onClick={onCancel}>取消</button><button type="submit" className="primary-action">{submit}</button></div>;
}

function protocolLabel(protocol: string) { return protocol === "hysteria2" ? "Hysteria2" : protocol === "vless_reality" ? "VLESS REALITY" : protocol; }
function statusLabel(status: string) { return status === "online" ? "在线" : status === "degraded" ? "降级" : status === "offline" ? "离线" : status === "provisioning" ? "待配置" : "停用"; }
function actionLabel(action: string) { return ({ "node.created": "节点已登记", "node.enabled_changed": "节点启停状态已更新", "user.created": "账户已创建", "user.status_changed": "账户状态已更新", "subscription.assigned": "已分配套餐", "plan.created": "套餐已创建", "device.revoked": "设备已撤销" } as Record<string, string>)[action] ?? action; }
function formatDate(value: string) { return new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date(value)); }
function formatTime(value: string) { return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(new Date(value)); }
