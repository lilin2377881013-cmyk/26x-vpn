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

type PaymentOrder = {
  orderNo: string; providerOrderNo: string | null; email: string; plan: string;
  product: string; amountCents: number; createdAt: string; paidAt: string | null;
  status: "created" | "unconfirmed" | "paid" | "selection" | "waiting" | "expired" | "review";
  paymentChannel: "selection" | "alipay" | "usdt"; usdtAmount: string | null; usdtAddress: string | null; txHash: string | null;
};

const paymentStatuses = { created: "未取得通道单号", unconfirmed: "未确认到账", paid: "已到账", selection: "待选付款方式", waiting: "等待链上确认", expired: "已过期", review: "待核查" };
const paymentChannels = { selection: "待选择", alipay: "支付宝", usdt: "USDT · TRC20" };
type USDTTransfer = { txHash: string; logIndex: number; amountUnits: string; address: string; orderNo: string | null; email: string | null; result: string; reason: string; blockTime: string };
const usdtUnitsLabel = (value: string) => { const digits = value.padStart(7, "0"); return `${digits.slice(0, -6)}.${digits.slice(-6)}`; };
const yuan = new Intl.NumberFormat("zh-CN", { style: "currency", currency: "CNY" });

export function PaymentOrders() {
  const [recheckHash, setRecheckHash] = useState("");
  const [recheckNote, setRecheckNote] = useState("");
  const [rechecking, setRechecking] = useState(false);
  const [page, setPage] = useState(1);
  const [refresh, setRefresh] = useState(0);
  const { data, error } = useData<{ items: PaymentOrder[]; hasMore: boolean }>(`/orders?page=${page}&refresh=${refresh}`);
  const { data: transfers } = useData<{items: USDTTransfer[]}>(`/usdt/transfers?refresh=${refresh}`);
  return <Page title="支付订单" action={<button onClick={() => setRefresh(refresh + 1)}>刷新</button>}>
    {error ? <ErrorMessage message={error} /> : !data ? <p>正在加载订单…</p> :
      data.items.length === 0 ? <p>暂无订单。</p> : <div className={styles.card} style={{ overflowX: "auto" }}>
        <table><thead><tr><th>订单号</th><th>用户</th><th>套餐</th><th>金额</th><th>渠道</th><th>状态</th><th>下单时间</th><th>到账时间</th></tr></thead>
          <tbody>{data.items.map((order) => <tr key={order.orderNo}>
            <td>{order.orderNo}{order.providerOrderNo && <small>通道单号 {order.providerOrderNo}</small>}</td>
            <td>{order.email}</td><td>{order.plan}</td><td>{yuan.format(order.amountCents / 100)}{order.usdtAmount && <small>{order.usdtAmount} USDT</small>}</td>
            <td>{paymentChannels[order.paymentChannel] ?? order.product}{order.usdtAddress && <small>{order.usdtAddress}</small>}{order.txHash && <small>TXID {order.txHash}</small>}</td><td>{paymentStatuses[order.status]}</td>
            <td>{dateLabel(order.createdAt)}</td><td>{dateLabel(order.paidAt)}</td>
          </tr>)}</tbody>
        </table>
      </div>}
    <div>
      <button disabled={page === 1} onClick={() => setPage(page - 1)}>上一页</button>{" "}
      <span>第 {page} 页</span>{" "}
      <button disabled={!data?.hasMore} onClick={() => setPage(page + 1)}>下一页</button>
    </div>
    <h2>USDT 链上到账记录</h2>
    <form onSubmit={async e => { e.preventDefault(); setRechecking(true); setRecheckNote(""); try { await write("/usdt/recheck", "POST", { txHash: recheckHash }); setRefresh(refresh + 1); setRecheckNote("链上补查完成，请核对下方记录；未确认或异常金额不会自动开通。"); } catch { setRecheckNote("暂时无法补查，请核实交易哈希，确认交易已上链后重试。"); } finally { setRechecking(false); } }}>
      <label>交易哈希（TXID）<input value={recheckHash} onChange={e => setRecheckHash(e.target.value)} required pattern="[a-fA-F0-9]{64}" maxLength={64} placeholder="输入 64 位交易哈希" /></label>
      <button disabled={rechecking}>{rechecking ? "正在核查…" : "从链上补查"}</button>
      {recheckNote && <p role="status">{recheckNote}</p>}
    </form>
    <p>展示最近 200 条已确认转账。未匹配或超时付款需核查；不可仅凭付款截图补发会员。</p>
    {transfers && <div className={styles.card} style={{overflowX: "auto"}}><table>
      <thead><tr><th>交易哈希</th><th>实际到账 USDT</th><th>收款地址</th><th>订单 / 用户</th><th>处理结果</th><th>链上时间</th></tr></thead>
      <tbody>{transfers.items.map(t => <tr key={`${t.txHash}:${t.logIndex}`}>
        <td style={{maxWidth: 240, overflowWrap: "anywhere"}}>{t.txHash}<small>事件 #{t.logIndex}</small></td>
        <td>{usdtUnitsLabel(t.amountUnits)}</td><td style={{maxWidth: 220, overflowWrap: "anywhere"}}>{t.address}</td>
        <td>{t.orderNo ?? "未匹配"}<small>{t.email}</small></td><td>{t.result === "paid" ? "已开通会员" : "待人工核查"}</td><td>{dateLabel(t.blockTime)}</td>
      </tr>)}</tbody></table></div>}

  </Page>;
}

type Referral = { code: string; name: string; visits: number; downloads: number };
type ReferralMetric = { day: string; device: string; channel: string; count: number };
const referralChannels: Record<string, string> = {
  "": "官网访问", "ios-tf": "TestFlight", "ios-store": "App Store",
  android: "Android 下载", macos: "macOS 下载", windows: "Windows 下载",
};

function ReferralReport({ code }: { code: string }) {
  const { data, error } = useData<{ items: ReferralMetric[] }>(`/referrals/${code}`);
  return <section className={styles.card} aria-label="代理统计明细" style={{ overflowX: "auto" }}>
    <h2>最近 30 天</h2>
    {error ? <ErrorMessage message={error} /> : !data ? <p>正在读取明细…</p> :
      data.items.length === 0 ? <p>暂无访问或下载记录。</p> :
      <table><thead><tr><th>日期（北京时间）</th><th>访客设备</th><th>事件</th><th>次数</th></tr></thead>
        <tbody>{data.items.map(({ day, device, channel, count }) =>
          <tr key={JSON.stringify([day, device, channel])}>
            <td>{day}</td><td>{device}</td><td>{referralChannels[channel] ?? channel}</td><td>{count}</td>
          </tr>)}</tbody></table>}
  </section>;
}

type BotMetric = { event: string; people: number; count: number };
type BotChannel = { code: string; started: number; packages: number; testflight: number };
const botPlatforms: Record<string, string> = { "ios-tf": "iPhone / iPad", android: "安卓", windows: "Windows", macos: "Mac" };
const botEvents: Record<string, string> = { start: "启动", choose: "选择平台", request: "点击领取", sent: "发出安装包" };

function useTelegramStats<T>(code?: string) {
  const [rows, setRows] = useState<T[]>();
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    const path = code ? `/admin/telegram-stats?code=${encodeURIComponent(code)}` : "/admin/telegram-stats";
    fetch(path, { signal: controller.signal, cache: "no-store" })
      .then(async response => {
        if (response.status === 401) throw new Error("登录已过期，请重新登录后台。");
        if (response.status === 403) throw new Error("当前账号没有查看推广统计的权限。");
        if (!response.ok) throw new Error("暂时无法读取机器人统计，请点击刷新重试。");
        const data = await response.json();
        if (!Array.isArray(data?.items)) throw new Error("机器人统计响应异常，请点击刷新重试。");
        if (!controller.signal.aborted) setRows(data.items);
      }).catch(error => { if (!controller.signal.aborted) setError(error.message || "无法读取机器人统计"); });
    return () => controller.abort();
  }, [code]);
  return { rows, error };
}

function TelegramOverview({ referrals }: { referrals: Referral[] }) {
  const { rows, error } = useTelegramStats<BotChannel>();
  const byCode = new Map(rows?.map(row => [row.code, row]));
  return <section className={styles.card} aria-label="TG 机器人渠道统计" style={{ overflowX: "auto" }}>
    <h2>TG 机器人渠道统计</h2>
    <p>每个渠道按人累计。同一人领取多个平台的安装包只计 1 人。iOS 的 TestFlight 邀请单独统计。</p>
    {error ? <ErrorMessage message={error} /> : !rows ? <p>正在读取机器人统计…</p> :
      <table><thead><tr><th>推广渠道</th><th>点「开始」人数</th><th>领取安装包人数</th><th>领取 TestFlight 链接人数</th></tr></thead>
        <tbody>{referrals.map(channel => {
          const metric = byCode.get(channel.code);
          return <tr key={channel.code}><th scope="row">{channel.name}</th><td>{metric?.started ?? 0}</td>
            <td>{metric?.packages ?? 0}</td><td>{metric?.testflight ?? 0}</td></tr>;
        })}</tbody></table>}
  </section>;
}

function TelegramReport({ code }: { code: string }) {
  const { rows, error } = useTelegramStats<BotMetric>(code);
  return <section className={styles.card} aria-label="Telegram 渠道统计明细">
    <h2>Telegram 渠道明细</h2>
    {error ? <ErrorMessage message={error} /> : !rows ? <p>正在读取…</p> :
      rows.length === 0 ? <p>这个推广链接还没有机器人互动。</p> :
      <table><thead><tr><th>平台</th><th>事件</th><th>人数（去重）</th><th>次数</th></tr></thead>
        <tbody>{rows.map(({event, people, count}) => {
          const [action, platform] = event.split("/");
          return <tr key={event}><td>{botPlatforms[platform] ?? "全部"}</td>
            <td>{action === "sent" && platform === "ios-tf" ? "发出 TestFlight 链接" : botEvents[action] ?? action}</td>
            <td>{people}</td><td>{count}</td></tr>;
        })}</tbody></table>}
  </section>;
}

export function ReferralLinks() {
  const [refresh, setRefresh] = useState(0);
  const [selected, setSelected] = useState("");
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState("");
  const { data, error } = useData<{ items: Referral[] }>(`/referrals?refresh=${refresh}`);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    setStatus("");
    setBusy(true);
    try {
      const item = await write<Referral>("/referrals", "POST", { name: new FormData(form).get("name") });
      form.reset();
      setSelected(item.code);
      setRefresh((value) => value + 1);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "无法生成代理链接");
    } finally {
      setBusy(false);
    }
  }

  async function copy(link: string) {
    try {
      await navigator.clipboard.writeText(new URL(link, window.location.origin).href);
      setStatus("已复制代理链接");
    } catch {
      setStatus("请右键点击代理链接，选择复制链接地址");
    }
  }

  return <Page title="代理推广" action={<button onClick={() => setRefresh(refresh + 1)}>刷新</button>}>
    {data && data.items.length > 0 && <TelegramOverview key={refresh} referrals={data.items} />}
    <form className={styles.card + " admin-form"} onSubmit={create}>
      <label>代理名称<input name="name" required maxLength={80} /></label>
      <div><button className="primary-action" disabled={busy}>{busy ? "生成中…" : "生成代理链接"}</button></div>
      {status && <p role="status">{status}</p>}
    </form>
    {error ? <ErrorMessage message={error} /> : !data ? <p>正在加载代理数据…</p> :
      data.items.length === 0 ? <p>暂无代理链接。</p> : <div className={styles.card} style={{ overflowX: "auto" }}>
        <table><thead><tr><th>代理</th><th>官网推广链接</th><th>Telegram 推广链接</th><th>官网访问次数</th><th>下载点击</th><th>统计明细</th></tr></thead>
          <tbody>{data.items.map((item) => {
            const link = `/api/v1/referrals/${item.code}`;
            const tg = `https://t.me/Get26xbot?start=${item.code}`;
            return <tr key={item.code}>
              <td>{item.name}</td>
              <td><a href={link}>打开链接</a>{" "}<button onClick={() => copy(link)}>复制</button></td>
              <td><a href={tg}>打开机器人</a>{" "}<button onClick={() => copy(tg)}>复制</button></td>
              <td>{item.visits}</td><td>{item.downloads}</td>
              <td><button aria-pressed={selected === item.code} onClick={() => setSelected(item.code)}>查看</button></td>
            </tr>;
          })}</tbody>
        </table>
      </div>}
    {selected && <TelegramReport key={`tg:${selected}:${refresh}`} code={selected} />}
    {selected && <ReferralReport key={`${selected}:${refresh}`} code={selected} />}
  </Page>;
}

type WebsiteVisit = { id: string; time: string; page: string; source: string; device: string; os: string; browser: string };
const visitTime = new Intl.DateTimeFormat("zh-CN", { dateStyle: "short", timeStyle: "medium", timeZone: "Asia/Shanghai", hour12: false });

export function WebsiteVisits() {
  const [cursors, setCursors] = useState<string[]>([]);
  const [refresh, setRefresh] = useState(0);
  const cursor = cursors.at(-1) ?? "";
  const { data, error } = useData<{ items: WebsiteVisit[]; hasMore: boolean }>(
    `/website-visits?before=${encodeURIComponent(cursor)}&refresh=${refresh}`
  );
  return <Page title="官网访客" action={<button onClick={() => { setCursors([]); setRefresh(refresh + 1); }}>刷新</button>}>
    <section className={styles.card}>
      <p>最近 30 天的公开页面访问，每次加载单独记录。系统和浏览器版本以设备上报为准。</p>
      {error ? <ErrorMessage message={error} /> : !data ? <p>正在加载访客记录…</p> :
        data.items.length === 0 ? <p>暂无访问记录。</p> :
        <table><thead><tr><th>访问时间（北京）</th><th>页面</th><th>推广来源</th><th>设备</th><th>系统</th><th>浏览器</th></tr></thead>
          <tbody>{data.items.map(row => <tr key={row.id}>
            <td>{visitTime.format(new Date(row.time))}</td><td>{row.page}</td><td>{row.source}</td>
            <td>{row.device}</td><td>{row.os}</td><td>{row.browser}</td>
          </tr>)}</tbody></table>}
      <div className={styles.pager}>
        <button disabled={!cursors.length} onClick={() => setCursors(cursors.slice(0, -1))}>上一页</button>
        <button disabled={!data?.hasMore} onClick={() => {
          const last = data?.items.at(-1);
          if (last) setCursors([...cursors, last.id]);
        }}>下一页</button>
      </div>
    </section>
  </Page>;
}
