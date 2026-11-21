import { cookies } from "next/headers";
import { publicPlans } from "./account/member-data";
import { PlanList } from "./account/plans";
import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "26x VPN · 跨平台网络连接",
  description: "26x VPN 官方网站。查看 iPhone、macOS、Windows 和 Android 的官方下载入口。",
  alternates: { canonical: "/" },
};

const platforms = [
  { name: "iPhone", mark: "iOS", channels: [["ios-tf", "TestFlight"], ["ios-store", "App Store"]] },
  { name: "macOS", mark: "Mac", channels: [["macos", "下载 macOS 版"]] },
  { name: "Windows", mark: "PC", channels: [["windows", "下载 Windows 版"]] },
  { name: "Android", mark: "APK", channels: [["android", "下载 Android 版"]] },
];

async function downloadLinks(): Promise<Record<string, string>> {
  try {
    const response = await fetch(new URL("/api/v1/downloads", process.env.API_ORIGIN ?? "http://127.0.0.1:8080"), {
      cache: "no-store",
      signal: AbortSignal.timeout(3000),
    });
    if (response.ok) return (await response.json()).links;
  } catch {
    // The homepage remains available when the download service is unavailable.
  }
  return {};
}

export default async function HomePage({ searchParams }: {
  searchParams: Promise<{ ref?: string | string[] }>;
}) {
  const [links, { ref }, plans, jar] = await Promise.all([downloadLinks(), searchParams, publicPlans(), cookies()]);
  const signedIn = jar.has("26x_member_session");
  const referral = typeof ref === "string" && /^[A-Za-z0-9_-]{16}$/.test(ref) ? ref : null;
  return (
    <main className="site-shell final-site" id="top">
      <nav className="topbar wrap" aria-label="主导航">
        <a className="brand" href="#top" aria-label="26x 首页"><span className="brand-mark">26</span><span className="brand-x">x</span><span className="brand-domain">VPN</span></a>
        <div className="nav-links"><a href="#plans">套餐</a><a href="#downloads">下载</a><a href="/support">帮助</a></div>
        <a className="member-entry" href={signedIn ? "/account" : "/account/login"}>{signedIn ? "个人中心" : "登录 / 注册"}</a>
      </nav>
      <section className="hero wrap">
        <div className="hero-copy">
          <div className="eyebrow">26x VPN</div>
          <h1>一键连接<br /><em>随时切换地区</em></h1>
          <div className="hero-actions"><a className="button" href="#downloads">下载 26x <span aria-hidden="true">↓</span></a><a className="text-link" href="/support">获取帮助 →</a></div>
        </div>
        <div className="app-preview" aria-hidden="true">
          <span>26x</span><div className="app-preview-power">⏻</div><div className="app-preview-region">选择线路 <span>›</span></div>
        </div>
      </section>
      <section className="section wrap" id="downloads">
        <div className="section-heading"><div><h2>下载客户端</h2></div><p>请选择与你设备匹配的官方入口。</p></div>
        <div className="download-grid">
          {platforms.map(({ name, mark, channels }) => (
            <article className="download-card" key={name}>
              <span className="platform-mark">{mark}</span>
              <h3>{name}</h3>
              <div className="download-actions">
                {channels.map(([id, label]) => {
                  const address = links[id];
                  const store = id === "ios-store";
                  return <div className="download-option" key={id}>
                    {address ? <a className="button" href={referral ? `/api/v1/referrals/${referral}/downloads/${id}` : address}>{label}</a>
                      : <button className="button" type="button" disabled>{label}</button>}
                    {store && <span className="download-tax">商店税贵10%</span>}
                  </div>;
                })}
              </div>
            </article>
          ))}
        </div>
      </section>
      <section className="section wrap" id="plans">
        <div className="section-heading"><div><h2>会员套餐</h2></div><p>按次付费，可提前续费。支持支付宝和 USDT（TRC20）。</p></div>
        {plans?.length ? <PlanList plans={plans} /> : <p>套餐暂时无法显示，请稍后再试。</p>}
      </section>
      <section className="section wrap" id="features">
        <div className="section-heading"><div><h2>连接和设备，<em>一处管理</em></h2></div></div>
        <div className="feature-grid">
          <article className="feature-card"><h3>一键连接</h3><p>随时查看 VPN 状态，连接中也可以直接切换地区。</p></article>
          <article className="feature-card"><h3>隐私保护</h3><p>了解账户、设备和订单数据的用途。</p><a href="/privacy">查看隐私说明 →</a></article>
          <article className="feature-card"><h3>设备管理</h3><p>在个人中心查看已登记的设备，也可以撤销旧设备。</p></article>
        </div>
      </section>
      <footer className="footer"><div className="wrap footer-main">
        <a className="brand brand-footer" href="#top"><span className="brand-mark">26</span><span className="brand-x">x</span><span className="brand-domain">VPN</span></a>
        <p>© 2026 26x</p><div className="footer-links"><a href="/support">联系我们</a><a href="/privacy">隐私说明</a><a href="/terms">服务条款</a></div>
      </div></footer>
    </main>
  );
}
