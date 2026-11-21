"use client";

import { useRef, useState, type FormEvent } from "react";
import { portalRequest } from "../portal-client";

export function LoginForm({ plan }: { plan: string }) {
  const [register, setRegister] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting.current) return;
    const form = new FormData(event.currentTarget);
    const email = String(form.get("email")).trim();
    const password = String(form.get("password"));
    if (register && (new TextEncoder().encode(password).length < 12 || new TextEncoder().encode(password).length > 256)) {
      setError("密码长度须为 12 至 256 字节。"); return;
    }
    submitting.current = true; setBusy(true); setError("");
    try {
      await portalRequest("auth/" + (register ? "register" : "session"), "POST", { email, password });
      window.location.replace("/account" + (plan ? "?plan=" + encodeURIComponent(plan) : ""));
    } catch (e) {
      setError(e instanceof Error ? e.message : "暂时无法登录。");
      submitting.current = false; setBusy(false);
    }
  }
  return <div className="member-card member-login"><h1>{register ? "注册 26x 账号" : "登录 26x"}</h1>
    <p>官网与客户端共用账号和会员权益。</p>
    <form onSubmit={submit}><label>邮箱地址<input type="email" name="email" autoComplete="username" required maxLength={254} /></label>
      <label>密码<input key={register ? "new" : "existing"} name="password" type="password" required autoComplete={register ? "new-password" : "current-password"} /></label>
      {register && <small>请设置至少 12 字节的密码。</small>}
      {error && <p className="member-error" role="alert">{error}</p>}
      <button className="member-primary" disabled={busy}>{busy ? "请稍候…" : register ? "注册并登录" : "登录"}</button>
    </form>
    <button className="member-text" disabled={busy} onClick={() => { setRegister(!register); setError(""); }}>
      {register ? "已有账号？去登录" : "还没有账号？去注册"}
    </button>
  </div>;
}
