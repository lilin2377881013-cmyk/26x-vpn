"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { PlanList, money, type MemberPlan } from "./plans";
import { portalRequest } from "./portal-client";

export type Member = { id: string; email: string; csrfToken: string; planName: string | null; planTier: string | null; subscriptionEndsAt: string | null };
export type Order = { orderNo: string; plan: string; createdAt: string; amountCents: number; usdtAmount?: string; paymentChannel: string; status: string };
const statuses: Record<string, string> = { paid: "已支付", pending: "等待到账", expired: "已过期", selection: "待选支付方式", review: "待核查" };

function date(value: string) {
  return new Intl.DateTimeFormat("zh-CN", { timeZone: "Asia/Shanghai", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false }).format(new Date(value));
}

export function MemberDashboard({ account, selected, orders, page }: {
  account: Member; selected: string; page: number; orders: {items: Order[]; hasMore: boolean} | null;
}) {
  const router = useRouter();
  const [plans, setPlans] = useState<MemberPlan[] | null>(null);
  const [choice, setChoice] = useState(selected);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const lock = useRef(false);
  const plan = plans?.find(p => p.code === choice) ?? plans?.find(p => p.tier === (account.planTier ?? "standard")) ?? plans?.[0];
  const permanent = !!account.planName && !account.subscriptionEndsAt;
  const pending = orders?.items.some(o => ["pending", "selection", "review"].includes(o.status));
  useEffect(() => {
    const refresh = () => { if (document.visibilityState === "visible") router.refresh(); };
    const onShow = (event: PageTransitionEvent) => { if (event.persisted) { window.location.reload(); } };
    document.addEventListener("visibilitychange", refresh);
    window.addEventListener("pageshow", onShow);
    const timer = pending ? window.setInterval(refresh, 20000) : undefined;
    return () => { document.removeEventListener("visibilitychange", refresh); window.removeEventListener("pageshow", onShow); window.clearInterval(timer); };
  }, [router, pending]);
  async function act(action: "plans" | "checkout" | "logout") {
    if (lock.current) return;
    lock.current = true; setBusy(action); setError("");
    try {
      if (action === "logout") {
        await portalRequest("auth/session", "DELETE", undefined, account.csrfToken);
        window.location.replace("/"); return;
      }
      if (action === "plans") {
        const response = await portalRequest("me/plans", "GET");
        const { items } = await response.json();
        if (!Array.isArray(items) || !items.length) throw new Error("套餐暂不可用，请稍后重试。");
        setPlans(items); setBusy(""); lock.current = false;
        return;
      }
      if (!plan) throw new Error("请选择可购买的套餐。");
      const res = await portalRequest("me/checkout", "POST", {planCode: plan.code}, account.csrfToken);
      const { payUrl } = await res.json();
      const url = new URL(payUrl);
      if (url.protocol !== "https:" || url.username || url.password) throw new Error("收银台地址无效，请刷新后重试。");
      window.location.assign(url.href);
    } catch (e) {
      setError(e instanceof Error ? e.message : "操作失败，请重试。");
      lock.current = false; setBusy("");
    }
  }
  return <>
    <header className="member-heading"><div><h1>个人中心</h1><p>{account.email}</p></div>
      <button className="member-text" onClick={() => act("logout")} disabled={!!busy}>退出登录</button></header>
    <section className="member-summary"><div><small>当前套餐</small><h2>{account.planName || "尚未开通会员"}</h2></div>
      <div><small>会员有效期</small><strong>{permanent ? "永久有效" : account.subscriptionEndsAt ? date(account.subscriptionEndsAt) + "（北京时间）" : "待开通"}</strong></div>
    </section>
    <section className="member-card"><h2>{permanent ? "永久会员" : plans ? "选择套餐" : "会员服务"}</h2>
      {permanent ? <p>此账号已享有永久会员权益。</p> : <>
        <p>{account.planName ? "新套餐从现有会员有效期结束时生效，剩余天数保留。" : "选择套餐后，可在收银台使用支付宝或 USDT（TRC20）付款。"}</p>
        {plans?.length ? <><PlanList plans={plans} selected={plan?.code} onSelect={setChoice} />
          <button className="member-primary" disabled={!!busy} onClick={() => act("checkout")}>{busy === "checkout" ? "正在打开收银台…" : "前往收银台 · " + (plan ? money(plan.priceCents, plan.currency) : "")}</button>
        </> : <button className="member-primary" disabled={!!busy} onClick={() => act("plans")}>
          {busy === "plans" ? "正在读取套餐…" : account.planName ? "续费会员" : "开通会员"}
        </button>}
      </>}
      {error && <p className="member-error" role="alert">{error}</p>}
    </section>
    <section className="member-card"><div className="member-heading"><h2>订单记录</h2><button className="member-text" onClick={() => router.refresh()}>刷新状态</button></div>
      {!orders ? <p role="alert">订单暂时无法加载，请刷新重试。</p> : orders.items.length ? <ul className="member-orders">{orders.items.map(o => <li key={o.orderNo}>
        <div><strong>{o.plan}</strong><small>{date(o.createdAt)} · {o.paymentChannel === "usdt" ? "USDT（TRC20）" : o.paymentChannel === "alipay" ? "支付宝" : "待选支付方式"}</small><small>订单号 {o.orderNo}</small></div>
        <div>{plans && <strong>{o.usdtAmount ? o.usdtAmount + " USDT" : money(o.amountCents)}</strong>}<small data-paid={o.status === "paid"}>{statuses[o.status] ?? "处理中"}</small></div>
      </li>)}</ul> : <p>暂无订单。</p>}
      <div className="member-pager">{page > 1 && <a href={"?page=" + (page-1)}>上一页</a>}{orders?.hasMore && <a href={"?page=" + (page+1)}>下一页</a>}</div>
    </section>
  </>;
}
