"use client";

import { deviceSystemLabel } from "./device-system";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { FormEvent, ReactNode, useEffect, useState } from "react";
import { AdminNav } from "./admin-nav";
import styles from "./operations.module.css";

type Node = {
  code: string; name: string; region_code: string; status: string;
  capacity_limit: number | null; users: number; healthCheckedAt: string | null;
  endpoints: { id: string; host: string; port: number; protocol: string }[];
};
type User = { id: string; email: string; status: string; activeDevices: number; planName: string | null };
type Daily = { day: string; bytes_up: number; bytes_down: number };
type Stats = { users: number; traffic_used_gb: number; traffic_limit_gb: number | null; daily: Daily[] };
type Usage = { user: User; bytes_up: number; bytes_down: number; daily: Daily[] };
const base = "/api/v1/admin";
const regions = [["HK", "香港"], ["JP", "日本"], ["SG", "新加坡"], ["US", "美国"]];

class SessionExpired extends Error {}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(base + path, { cache: "no-store", ...options });
  if (response.status === 401) throw new SessionExpired("请重新登录");
  const data = await response.json().catch(() => null);
  if (!response.ok) throw new Error(data?.error?.message ?? "请求失败");
  return data as T;
}

async function write<T>(path: string, method: string, body?: unknown) {
  const { csrfToken } = await request<{ csrfToken: string }>("/me");
  return request<T>(path, {
    method,
    headers: { "Content-Type": "application/json", "X-CSRF-Token": csrfToken },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

function useData<T>(path: string): { data?: T; error?: string } {
  const router = useRouter();
  const [result, setResult] = useState<{ path: string; data?: T; error?: string }>();
  useEffect(() => {
    const controller = new AbortController();
    request<T>(path, { signal: controller.signal }).then(
      (data) => { if (!controller.signal.aborted) setResult({ path, data }); },
      (error: Error) => {
        if (controller.signal.aborted) return;
        if (error instanceof SessionExpired) router.replace("/admin/login");
        setResult({ path, error: error.message });
      },
    );
    return () => controller.abort();
  }, [path, router]);
  return result && result.path === path ? result : {};
}

function Page({ title, action, children }: { title: string; action?: ReactNode; children: ReactNode }) {
  const pathname = usePathname();
  return <main className="admin-shell">
    <AdminNav current={pathname} />
    <section className="admin-content">
      <header className="admin-topbar">
        <div><Link className="breadcrumb" href="/admin">26x 管理后台</Link> / {title}</div>
        <Link className="admin-help" href="/">访问官网 ↗</Link>
      </header>
      <div className={styles.page}>
        <header className={styles.heading}><h1>{title}</h1>{action}</header>
        {children}
      </div>
    </section>
  </main>;
}

function ErrorMessage({ message }: { message: string }) {
  return <div className="console-error" role="alert">{message}</div>;
}

const dateLabel = (value: string | null) => value ? new Date(value).toLocaleString("zh-CN") : "—";
const gigabytes = (bytes: number) => new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 3 }).format(bytes / 1e9);

export function NodeList() {
  const { data, error } = useData<{ nodes: Node[] }>("/nodes");
  return <Page title="节点列表" action={<Link className="primary-action" href="/admin/nodes/new">添加节点</Link>}>
    {error ? <ErrorMessage message={error} /> : !data ? <p>正在加载节点…</p> : <div className={styles.card}>
      <table><thead><tr><th>节点</th><th>地区</th><th>状态</th><th>用户数</th><th>在线用户上限</th><th>最近检查</th></tr></thead>
        <tbody>{(data.nodes ?? []).map((node) => <tr key={node.code}>
          <td><Link href={`/admin/nodes/${encodeURIComponent(node.code)}`}>{node.name}</Link><small>{node.code}</small></td>
          <td>{node.region_code}</td><td>{node.status}</td><td>{node.users ?? "—"}</td><td>{node.capacity_limit ?? "—"}</td>
          <td>{dateLabel(node.healthCheckedAt)}</td>
        </tr>)}</tbody></table>{!data.nodes?.length && <p>暂无节点。</p>}
    </div>}
  </Page>;
}

function NodeForm({ node, submit }: { node?: Node; submit: (body: object) => Promise<void> }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const fields = new FormData(form);
    const capacity = fields.get("capacity_limit");
    const body = {
      name: fields.get("name"), region_code: fields.get("region_code"), capacity_limit: capacity ? Number(capacity) : null,
      ...(!node ? {
        code: fields.get("code"), host: fields.get("host"), ssh_port: Number(fields.get("ssh_port")),
        ssh_user: fields.get("ssh_user"), ssh_password: fields.get("ssh_password"), port: Number(fields.get("port")),
      } : {}),
    };
    setBusy(true);
    setError("");
    try {
      await submit(body);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "保存失败");
    } finally {
      if (!node) (form.elements.namedItem("ssh_password") as HTMLInputElement).value = "";
      setBusy(false);
    }
  }
  return <form className="admin-form" onSubmit={handleSubmit}>
    {error && <ErrorMessage message={error} />}
    <fieldset className={styles.fields} disabled={busy}>
      {!node && <label>节点代码<input name="code" placeholder="jp-02" pattern={"[a-zA-Z0-9][a-zA-Z0-9\\-]{1,47}"} required /></label>}
      <label>名称<input name="name" defaultValue={node?.name} maxLength={80} required /></label>
      <label>地区<select name="region_code" defaultValue={node?.region_code ?? "HK"}>{regions.map(([code, label]) => <option key={code} value={code}>{label}</option>)}</select></label>
      <label>同时在线用户上限<input name="capacity_limit" type="number" min={1} defaultValue={node?.capacity_limit ?? undefined} /></label>
      {!node && <>
        <label>服务器地址<input name="host" placeholder="1.2.3.4" required /></label>
        <label>VPN 端口<input name="port" type="number" defaultValue={8443} min={1} max={65535} required /></label>
        <label>SSH 端口<input name="ssh_port" type="number" defaultValue={22} min={1} max={65535} required /></label>
        <label>SSH 用户<input name="ssh_user" defaultValue="root" required /></label>
        <label>SSH 密码<input name="ssh_password" type="password" autoComplete="new-password" required /></label>
      </>}
    </fieldset>
    <div><button className="primary-action" disabled={busy}>{busy ? (node ? "保存中…" : "部署中…") : node ? "保存设置" : "部署节点"}</button></div>
  </form>;
}

export function NewNode() {
  const router = useRouter();
  return <Page title="添加节点"><section className={styles.card}><NodeForm submit={async (body) => {
    const { node } = await write<{ node: Node }>("/nodes", "POST", body);
    router.replace(`/admin/nodes/${encodeURIComponent(node.code)}`);
  }} /></section></Page>;
}

function UsageChart({ daily }: { daily: Daily[] }) {
  if (daily.length === 0) return <p>暂无流量采集记录。</p>;
  const max = Math.max(1, ...daily.map((day) => day.bytes_up + day.bytes_down));
  return <div className={styles.chart}>
    {daily.map((day) => <div className={styles.barRow} key={day.day}>
      <time dateTime={day.day}>{day.day}</time>
      <meter min={0} max={max} value={day.bytes_up + day.bytes_down} aria-label={`${day.day} 总流量`} />
      <span>{gigabytes(day.bytes_up + day.bytes_down)} GB</span>
    </div>)}
  </div>;
}

type TrafficLimits = { monthlyQuotaBytes: number | null; bandwidthMbps: number | null };

function TrafficLimitsForm({ path, user = false }: { path: string; user?: boolean }) {
  const { data, error } = useData<TrafficLimits>(path);
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const fields = new FormData(event.currentTarget);
    const gb = fields.get("quota");
    const bandwidth = fields.get("bandwidth");
    setBusy(true);
    setStatus("");
    try {
      await write(path, "PUT", {
        monthlyQuotaBytes: gb ? Math.round(Number(gb) * 1e9) : null,
        bandwidthMbps: bandwidth ? Number(bandwidth) : null,
      });
      setStatus("已保存");
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "保存失败");
    } finally {
      setBusy(false);
    }
  }

  return <section className={styles.card}>
    <h2>流量控制</h2>
    {error ? <ErrorMessage message={error} /> : !data ? <p>正在读取限制…</p> :
      <form className="admin-form" onSubmit={save}>
        <label>每月流量额度（GB，留空不限额）
          <input name="quota" type="number" min="0.001" max="9000000" step="any"
            defaultValue={data.monthlyQuotaBytes == null ? "" : data.monthlyQuotaBytes / 1e9} />
        </label>
        <label>{user ? "用户带宽上限（Mbps）" : "该节点总带宽上限（Mbps，留空不限速）"}
          <input name="bandwidth" type="number" min="1" max={user ? 20 : 100000} step="1" required={user}
            defaultValue={data.bandwidthMbps ?? (user ? 20 : "")} />
        </label>
        <p>{user ? "每个用户默认 20 Mbps（约 2.5 MB/s），可调低。" : "仅保存此节点的带宽设置。"} 实际限速需由节点执行。</p>
        <div><button className="primary-action" disabled={busy}>{busy ? "保存中…" : "保存限制"}</button></div>
        {status && <p role="status">{status}</p>}
      </form>}
  </section>;
}

export function NodeDetail({ code }: { code: string }) {
  const path = "/nodes/" + encodeURIComponent(code);
  const router = useRouter();
  const detail = useData<{ node: Node }>(path);
  const stats = useData<Stats>(path + "/stats");
  const [saved, setSaved] = useState<Node>();
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const node = saved ?? detail.data?.node;

  async function change(action: "remove" | "pause" | "resume") {
    if (action === "remove" && !window.confirm(`删除节点 ${code}？`)) return;
    setBusy(true);
    setNotice("");
    try {
      if (action === "remove") {
        await write(path, "DELETE");
        router.replace("/admin/nodes");
        return;
      }
      await write(path + "/" + action, "POST");
      const { node: refreshed } = await request<{ node: Node }>(path);
      setSaved(refreshed);
      setNotice(action === "pause" ? "节点已暂停" : "节点已恢复");
      setBusy(false);
    } catch (caught) {
      setNotice(caught instanceof Error ? caught.message : "删除失败");
      setBusy(false);
    }
  }

  return <Page title={node?.name ?? code} action={node && <button type="button" className="subtle-button" disabled={busy} onClick={() => void change("remove")}>删除节点</button>}>
    {detail.error && <ErrorMessage message={detail.error} />}
    {!node && !detail.error && <p>正在加载节点…</p>}
    {notice && <p role="status">{notice}</p>}
    {node && <>
      <section className={styles.card}>
        <h2>节点设置</h2>
        <button type="button" className="subtle-button" disabled={busy} onClick={() => void change(node.status === "disabled" ? "resume" : "pause")}>{node.status === "disabled" ? "恢复节点" : "暂停节点"}</button>
        <p>{node.code} · {node.status} · {node.users} 位用户</p>
        <ul>{node.endpoints.map((endpoint) => <li key={endpoint.id}>{endpoint.host}:{endpoint.port} · {endpoint.protocol}</li>)}</ul>
        <NodeForm node={node} submit={async (body) => {
          const { node: updated } = await write<{ node: Node }>(path, "PUT", body);
          setSaved(updated);
          setNotice("节点设置已保存");
        }} />
      </section>
      <TrafficLimitsForm path={path + "/traffic-limits"} />
      <section className={styles.card}>
        <h2>最近 30 天流量</h2>
        {stats.error ? <ErrorMessage message={stats.error} /> : stats.data ? <>
          <p>已用 {stats.data.traffic_used_gb} GB · 额度 {stats.data.traffic_limit_gb === null ? "未设置" : `${stats.data.traffic_limit_gb} GB`} · 用户 {stats.data.users}</p>
          <UsageChart daily={stats.data.daily} />
        </> : <p>正在加载流量…</p>}
      </section>
    </>}
  </Page>;
}

export function UserList() {
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(0);
  const { data, error } = useData<{ items: User[]; hasMore: boolean }>(`/users?limit=50&offset=${page * 50}&q=${encodeURIComponent(query)}`);
  return <Page title="用户列表">
    <form className={`admin-form ${styles.search}`} onSubmit={(event) => {
      event.preventDefault();
      setPage(0);
      setQuery(String(new FormData(event.currentTarget).get("q") ?? ""));
    }}><label>搜索邮箱<input name="q" type="search" /></label><button className="subtle-button">搜索</button></form>
    {error ? <ErrorMessage message={error} /> : !data ? <p>正在加载用户…</p> : <div className={styles.card}>
      <table><thead><tr><th>邮箱</th><th>状态</th><th>套餐</th><th>设备</th></tr></thead>
        <tbody>{data.items.map((user) => <tr key={user.id}>
          <td><Link href={`/admin/users/${encodeURIComponent(user.id)}`}>{user.email}</Link></td>
          <td>{user.status}</td><td>{user.planName ?? "—"}</td><td>{user.activeDevices}</td>
        </tr>)}</tbody></table>
      {data.items.length === 0 && <p>暂无匹配用户。</p>}
    </div>}
    <div className={styles.pager}>
      <button className="subtle-button" type="button" disabled={page === 0} onClick={() => setPage(page - 1)}>上一页</button>
      <span>第 {page + 1} 页</span>
      <button className="subtle-button" type="button" disabled={!data?.hasMore} onClick={() => setPage(page + 1)}>下一页</button>
    </div>
  </Page>;
}

export function UserDetail({ id }: { id: string }) {
  const { data, error } = useData<Usage>(`/users/${encodeURIComponent(id)}/usage`);
  const devices = useData<{ items: { id: string; label: string; platform: string; osName?: string | null; osVersion?: string | null }[] }>(
    `/devices?userId=${encodeURIComponent(id)}`,
  );
  return <Page title={data?.user.email ?? "用户详情"}>
    {error ? <ErrorMessage message={error} /> : data ? <section className={styles.card}>
      <p>{data.user.status} · {data.user.id}</p>
      <h2>最近 30 天流量</h2>
      <p>上传 {gigabytes(data.bytes_up)} GB · 下载 {gigabytes(data.bytes_down)} GB</p>
      <UsageChart daily={data.daily} />
    </section> : <p>正在加载流量…</p>}
    {data && <section className={styles.card}>
      <h2>设备操作系统</h2>
      {devices.error ? <ErrorMessage message={devices.error} /> : !devices.data ? <p>正在加载设备…</p> :
        devices.data.items.length === 0 ? <p>暂无登记设备。</p> :
          <table><thead><tr><th>设备</th><th>操作系统</th></tr></thead><tbody>
            {devices.data.items.map((device) => <tr key={device.id}><td>{device.label}</td><td>{deviceSystemLabel(device)}</td></tr>)}
          </tbody></table>}
    </section>}
    {data && <TrafficLimitsForm key={id} path={`/users/${encodeURIComponent(id)}/traffic-limits`} user />}
  </Page>;
}

type SupportTicket = { id: string; user: string; message: string; createdAt: string; notified: boolean };

export function SupportInbox() {
  const { data, error } = useData<{ items: SupportTicket[] }>("/support");
  return <Page title="客服留言">
    {error ? <ErrorMessage message={error} /> : !data ? <p>正在加载留言…</p> :
      data.items.length === 0 ? <p>暂无客服留言。</p> :
      data.items.map((ticket) => <article className={styles.card} key={ticket.id}>
        <strong>{ticket.user}</strong> · <time dateTime={ticket.createdAt}>{dateLabel(ticket.createdAt)}</time>
        <p style={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{ticket.message}</p>
        <small>工单 {ticket.id} · {ticket.notified ? "Telegram 已通知" : "Telegram 未通知"}</small>
      </article>)
    }
  </Page>;
}

const channels = [["ios-tf", "iPhone · TestFlight"], ["ios-store", "iPhone · App Store"], ["macos", "macOS"], ["windows", "Windows"], ["android", "Android"]];

function DownloadForm({ channel, label, initial }: { channel: string; label: string; initial: string }) {
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const url = new FormData(event.currentTarget).get("url");
    setBusy(true);
    setStatus("");
    try {
      await write("/downloads/" + channel, "PUT", { url });
      setStatus(url ? "已发布到官网" : "已隐藏下载入口");
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "保存失败");
    } finally {
      setBusy(false);
    }
  }
  return <form className={styles.card + " admin-form"} onSubmit={save}>
    <label>{label}<input name="url" type="url" maxLength={2048} defaultValue={initial} placeholder="https://" /></label>
    <div><button className="primary-action" disabled={busy}>{busy ? "保存中…" : "保存地址"}</button></div>
    {status && <p role="status">{status}</p>}
  </form>;
}

export function DownloadSettings() {
  const { data, error } = useData<{ links: Record<string, string> }>("/downloads");
  return <Page title="下载管理">
    <p>留空并保存即可隐藏对应的下载入口。iPhone 优先显示 TestFlight。</p>
    {error ? <ErrorMessage message={error} /> : !data ? <p>正在加载下载地址…</p> : channels.map(([channel, label]) =>
      <DownloadForm key={channel} channel={channel} label={label} initial={data.links[channel] ?? ""} />
    )}
  </Page>;
}
