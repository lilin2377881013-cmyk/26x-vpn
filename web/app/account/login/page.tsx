"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";

export default function AccountLoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const response = await fetch("/api/v1/auth/session", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });
      if (!response.ok) {
        const payload = await response.json().catch(() => null);
        setError(payload?.error?.message ?? "登录失败，请检查账户信息后重试。");
        return;
      }
      router.replace("/account");
    } catch {
      setError("账户服务暂时不可用，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="account-auth-shell">
      <Link className="brand account-auth-brand" href="/" aria-label="26x 首页"><span className="brand-mark">26</span><span className="brand-x">x</span><span className="brand-domain">VPN</span></Link>
      <section className="account-auth-card">
        <div className="eyebrow">26X VPN · ACCOUNT</div>
        <h1>登录你的账户</h1>
        <p>查看账户状态和已登记设备。</p>
        <form onSubmit={submit}>
          <label>账户邮箱<input autoComplete="username" type="email" value={email} onChange={(event) => setEmail(event.target.value)} required /></label>
          <label>密码<input autoComplete="current-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} required /></label>
          {error && <div className="login-error" role="alert">{error}</div>}
          <button className="primary-action login-submit" disabled={busy}>{busy ? "正在验证…" : "登录 26x 账户 →"}</button>
        </form>
        <div className="account-auth-note">账户由 26x 管理员开通。公开注册与密码找回功能尚未开放。</div>
      </section>
      <Link className="login-back" href="/">← 返回 26x 官网</Link>
    </main>
  );
}
