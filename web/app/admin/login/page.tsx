"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";

export default function AdminLoginPage() {
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
      const response = await fetch("/api/v1/admin/session", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      });
      if (!response.ok) {
        const payload = await response.json().catch(() => null);
        setError(payload?.error?.message ?? "无法登录，请检查账户信息和 API 状态。");
        return;
      }
      router.replace("/admin");
    } catch {
      setError("控制 API 暂时不可用，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="login-shell">
      <Link className="brand login-brand" href="/"><span className="brand-mark">26</span><span className="brand-x">x</span><small>CONTROL</small></Link>
      <section className="login-card">
        <div className="eyebrow">OPERATOR ACCESS</div>
        <h1>登录控制台</h1>
        <p>使用 26x 管理员账户继续。</p>
        <form onSubmit={submit}>
          <label>管理员邮箱<input autoComplete="username" type="email" value={email} onChange={(event) => setEmail(event.target.value)} required /></label>
          <label>密码<input autoComplete="current-password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} required /></label>
          {error && <div className="login-error" role="alert">{error}</div>}
          <button className="primary-action login-submit" disabled={busy}>{busy ? "正在验证…" : "登录 26x Control →"}</button>
        </form>
        <div className="login-note">所有写入操作都会进行权限和 CSRF 校验，并记录审计事件。</div>
      </section>
      <Link className="login-back" href="/">← 返回 26x 官网</Link>
    </main>
  );
}
