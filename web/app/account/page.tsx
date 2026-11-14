"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";

type Account = { id: string; email: string; status: string; subscriptionEndsAt: string | null; csrfToken: string };
type Device = { id: string; platform: string; label: string; appVersion: string | null; coreVersion: string | null; lastSeenAt: string | null; createdAt: string };

export default function AccountPage() {
  const router = useRouter();
  const [account, setAccount] = useState<Account | null>(null);
  const [devices, setDevices] = useState<Device[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [passwordBusy, setPasswordBusy] = useState(false);

  const refresh = useCallback(async () => {
    setError("");
    try {
      const meResponse = await fetch("/api/v1/me", { credentials: "same-origin", cache: "no-store" });
      if (meResponse.status === 401) {
        router.replace("/account/login");
        return;
      }
      if (!meResponse.ok) throw new Error("暂时无法读取账户信息。");
      const me: Account = await meResponse.json();
      const deviceResponse = await fetch("/api/v1/me/devices", { credentials: "same-origin", cache: "no-store" });
      if (deviceResponse.status === 401) {
        router.replace("/account/login");
        return;
      }
      if (!deviceResponse.ok) throw new Error("暂时无法读取设备列表。");
      const deviceData = await deviceResponse.json();
      setAccount(me);
      setDevices(deviceData.items ?? []);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "账户服务暂时不可用。");
    } finally {
      setLoading(false);
    }
  }, [router]);

  useEffect(() => {
    const task = window.setTimeout(() => { void refresh(); }, 0);
    return () => window.clearTimeout(task);
  }, [refresh]);

  async function revokeDevice(device: Device) {
    if (!account || !window.confirm(`撤销设备“${device.label}”？此设备将无法继续使用账户服务。`)) return;
    try {
      const response = await fetch(`/api/v1/me/devices/${encodeURIComponent(device.id)}`, {
        method: "DELETE",
        credentials: "same-origin",
        headers: { "X-CSRF-Token": account.csrfToken },
      });
      if (!response.ok) {
        const payload = await response.json().catch(() => null);
        throw new Error(payload?.error?.message ?? "撤销设备失败，请重试。");
      }
      setNotice("设备已撤销。");
      await refresh();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "撤销设备失败。");
    }
  }

  async function changePassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!account) return;
    const data = new FormData(event.currentTarget);
    const currentPassword = String(data.get("currentPassword") ?? "");
    const newPassword = String(data.get("newPassword") ?? "");
    const confirmPassword = String(data.get("confirmPassword") ?? "");
    if (newPassword.length < 16) {
      setError("新密码至少需要 16 个字符。");
      return;
    }
    if (newPassword !== confirmPassword) {
      setError("两次输入的新密码不一致。");
      return;
    }
    setPasswordBusy(true);
    setError("");
    try {
      const response = await fetch("/api/v1/me/password", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json", "X-CSRF-Token": account.csrfToken },
        body: JSON.stringify({ currentPassword, newPassword }),
      });
      if (!response.ok) {
        const payload = await response.json().catch(() => null);
        throw new Error(payload?.error?.message ?? "密码更新失败，请重试。");
      }
      setNotice("密码已更新。为保护账户，所有登录会话已退出，请重新登录。");
      window.setTimeout(() => router.replace("/account/login"), 1200);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "密码更新失败。");
    } finally {
      setPasswordBusy(false);
    }
  }

  async function logout() {
    try {
      if (account) {
        await fetch("/api/v1/auth/session", {
          method: "DELETE",
          credentials: "same-origin",
          headers: { "X-CSRF-Token": account.csrfToken },
        });
      }
    } finally {
      router.replace("/");
    }
  }

  if (loading) return <main className="admin-loading"><span className="live-dot" /> 正在验证 26x 账户…</main>;

  return (
    <main className="account-shell">
      <header className="account-header wrap">
        <Link className="brand" href="/" aria-label="26x 首页"><span className="brand-mark">26</span><span className="brand-x">x</span><span className="brand-domain">VPN</span></Link>
        <div className="account-header-actions"><a href="/support">支持与动态</a><button className="subtle-button" onClick={() => void logout()}>退出登录</button></div>
      </header>

      <div className="account-page wrap">
        <div className="account-page-heading"><div><div className="eyebrow">26X VPN · MY ACCOUNT</div><h1>账户与设备</h1><p>管理账户状态、已登记设备和登录安全。</p></div><span className="account-state"><i />{account ? account.status === "active" ? "账户正常" : "账户暂停" : "暂不可用"}</span></div>
        <div className="account-status-note"><strong>服务开发中</strong><span>网页账户管理已接入控制 API，但 VPN 客户端和可用隧道尚未开放。</span></div>
        {error && <div className="console-error" role="alert"><span>{error}</span><button onClick={() => void refresh()}>重新载入</button></div>}
        {notice && <div className="console-notice" role="status"><span>{notice}</span><button onClick={() => setNotice("")}>×</button></div>}

        <div className="account-overview-grid">
          <section className="account-panel account-profile-panel">
            <div className="eyebrow">ACCOUNT</div>
            <h2>{account?.email ?? "账户信息暂不可用"}</h2>
            <p>账户状态 <strong>{account ? account.status === "active" ? "正常" : "暂停" : "—"}</strong></p>
            <p>订阅到期 <strong>{account ? account.subscriptionEndsAt ? formatDate(account.subscriptionEndsAt) : "暂无有效订阅" : "—"}</strong></p>
          </section>
          <section className="account-panel account-device-summary">
            <div className="eyebrow">REGISTERED DEVICES</div>
            <strong>{devices.length}</strong>
            <span>台有效设备</span>
            <p>设备由已登录的 26x 客户端登记。</p>
          </section>
        </div>

        <section className="account-panel account-devices-panel">
          <div className="account-section-heading"><div><h2>已登记设备</h2><p>撤销设备后，该设备将不能再使用账户获取服务配置。</p></div><button className="subtle-button" onClick={() => void refresh()}>刷新列表 ↻</button></div>
          <div className="account-device-list">
            {devices.map((device) => (
              <article className="account-device-row" key={device.id}>
                <span className="account-device-icon" aria-hidden="true">{platformIcon(device.platform)}</span>
                <div className="account-device-main"><strong>{device.label}</strong><small>{platformLabel(device.platform)} · {device.appVersion ?? "应用版本未知"}{device.coreVersion ? ` · 核心 ${device.coreVersion}` : ""}</small></div>
                <div className="account-device-meta"><span>最近在线</span><strong>{device.lastSeenAt ? formatDateTime(device.lastSeenAt) : "尚未连接"}</strong></div>
                <div className="account-device-meta"><span>登记时间</span><strong>{formatDate(device.createdAt)}</strong></div>
                <button className="danger-quiet-button" onClick={() => void revokeDevice(device)}>撤销设备</button>
              </article>
            ))}
            {devices.length === 0 && <div className="account-empty"><span>⌘</span><strong>还没有登记设备</strong><p>登录 26x 客户端后，设备会显示在这里。</p></div>}
          </div>
        </section>

        <section className="account-panel account-password-panel">
          <div><h2>更改密码</h2><p>新密码至少 16 个字符。更新后，所有已登录会话都会退出。</p></div>
          <form className="password-form" onSubmit={changePassword}>
            <label>当前密码<input name="currentPassword" type="password" autoComplete="current-password" minLength={1} required /></label>
            <label>新密码<input name="newPassword" type="password" autoComplete="new-password" minLength={16} required /><small>至少 16 个字符</small></label>
            <label>确认新密码<input name="confirmPassword" type="password" autoComplete="new-password" minLength={16} required /></label>
            <button className="primary-action" disabled={passwordBusy}>{passwordBusy ? "正在更新…" : "更新密码"}</button>
          </form>
        </section>
      </div>

      <footer className="account-footer"><div className="wrap"><span>© 2026 26x VPN</span><Link href="/privacy">隐私说明</Link><Link href="/terms">服务条款</Link><Link href="/admin">管理入口</Link></div></footer>
    </main>
  );
}

function platformLabel(platform: string) {
  return ({ ios: "iOS", macos: "macOS", windows: "Windows", android: "Android" } as Record<string, string>)[platform] ?? platform;
}

function platformIcon(platform: string) {
  return ({ ios: "◉", macos: "⌘", windows: "⊞", android: "◈" } as Record<string, string>)[platform] ?? "◇";
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date(value));
}

function formatDateTime(value: string) {
  return new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
}
