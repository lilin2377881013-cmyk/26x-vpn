import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { MemberDashboard, type Member, type Order } from "./dashboard";
import { readAPI } from "./member-data";

export default async function AccountPage({ searchParams }: { searchParams: Promise<{ plan?: string; page?: string }> }) {
  const params = await searchParams;
  const selected = typeof params.plan === "string" ? params.plan.slice(0, 80) : "";
  const login = "/account/login" + (selected ? "?plan=" + encodeURIComponent(selected) : "");
  const token = (await cookies()).get("26x_member_session")?.value;
  if (!token) redirect(login);
  const page = Math.max(1, Math.min(1000000, Number.parseInt(params.page ?? "", 10) || 1));
  const [me, history] = await Promise.all([
    readAPI("me", token).catch(() => null),
    readAPI("me/orders?page=" + page, token).catch(() => null),
  ]);
  if (me?.status === 401 || history?.status === 401) redirect(login);
  if (!me?.ok) return <section className="member-card"><h1>个人中心暂时无法打开</h1><p>请稍后刷新页面重试。</p><a href="/account">重新加载</a></section>;
  const account: Member = await me.json();
  const orders: {items: Order[]; hasMore: boolean} | null = history?.ok ? await history.json() : null;
  return <MemberDashboard key={account.id} account={account} selected={selected} orders={orders} page={page} />;
}
