import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "26x VPN — 网络连接，回到你的节奏",
  alternates: { canonical: "/" },
  openGraph: {
    title: "26x VPN — 网络连接，回到你的节奏",
    description: "26x VPN 多平台客户端与服务中心，查看开发进度和支持平台。",
    url: "https://26xvpn.com",
  },
};

const platforms = [
  { name: "iOS", detail: "TestFlight 版本准备中", icon: "◉" },
  { name: "macOS", detail: "Apple 芯片与 Intel", icon: "⌘" },
  { name: "Windows", detail: "Windows 10 / 11", icon: "⊞" },
  { name: "Android", detail: "Android 8.0 及以上", icon: "◈" },
];

const features = [
  {
    number: "01",
    title: "一个账号管理多台设备",
    body: "从账户和设备列表开始，让你清楚看到哪些设备正在使用 26x。",
    icon: "⌘",
  },
  {
    number: "02",
    title: "连接状态说得明白",
    body: "把节点探测与 VPN 隧道状态分开呈现，避免测速失败被误报成连接失败。",
    icon: "⌁",
  },
  {
    number: "03",
    title: "隐私信息公开说明",
    body: "在正式开放前说明收集哪些账户与设备数据、用途和保留期限。",
    icon: "◇",
  },
];

const faq = [
  {
    question: "26x 现在可以连接使用吗？",
    answer:
      "还不可以。客户端和控制服务仍在开发，当前版本不提供可用的 VPN 隧道。我们会在真实连接、账户与设备管理完成后再开放测试。",
  },
  {
    question: "iOS 会通过什么方式测试？",
    answer:
      "iOS 计划通过 TestFlight 分发。IPA 构建和签名配置完成后，26x 会在官网公布测试入口。",
  },
  {
    question: "26x 会保存浏览记录吗？",
    answer:
      "目前公开的是产品设计目标，不代表生产服务已经运行。正式上线前，隐私说明会列出数据类型、用途、保存期限与删除方式。",
  },
];

export default function HomePage() {
  return (
    <main className="site-shell" id="top">
      <nav className="topbar wrap" aria-label="主导航">
        <a className="brand" href="#top" aria-label="26x 首页">
          <span className="brand-mark">26</span>
          <span className="brand-x">x</span>
          <span className="brand-domain">VPN</span>
        </a>
        <div className="nav-links">
          <a href="#features">产品特点</a>
          <a href="#platforms">支持平台</a>
          <a href="#faq">常见问题</a>
        </div>
        <a className="button button-small" href="/account/login">
          账户登录 <span aria-hidden="true">↗</span>
        </a>
      </nav>

      <section className="hero wrap">
        <div className="hero-copy">
          <div className="eyebrow"><span className="status-dot" /> 26X VPN · 开发预览</div>
          <h1>网络连接，<br /><em>回到你的节奏。</em></h1>
          <p className="hero-lede">
            26x 正在构建覆盖 iOS、macOS、Windows 与 Android 的 VPN
            客户端。先把连接状态、设备管理和问题诊断做清楚，再开放公开下载。
          </p>
          <div className="hero-actions">
            <a className="button" href="#platforms">查看开发进度 <span aria-hidden="true">↘</span></a>
            <a className="text-link" href="#features">了解产品设计 <span aria-hidden="true">→</span></a>
          </div>
          <div className="hero-footnote"><span className="tiny-lock">◇</span> 当前开发版本暂不提供可用的 VPN 隧道。</div>
        </div>

        <div className="hero-art" role="img" aria-label="26x VPN 产品状态预览：服务仍在开发中">
          <div className="hero-glow" />
          <div className="orb orb-back" />
          <div className="orb orb-front" />
          <div className="orbit orbit-one" />
          <div className="orbit orbit-two" />
          <div className="node node-a"><i /> 26x / HK</div>
          <div className="node node-b"><i /> YOUR DEVICE</div>
          <div className="signal-card">
            <div className="signal-top"><span>26X VPN STATUS</span><b>DEVELOPMENT</b></div>
            <div className="route-preview" aria-hidden="true">
              <span className="route-device">⌘</span><i /><span className="route-core">26x</span><i /><span className="route-node">↗</span>
            </div>
            <div className="signal-bottom"><span>VPN TUNNEL</span><strong>尚未开放</strong></div>
          </div>
          <span className="art-coord coord-one">MULTI-PLATFORM</span>
          <span className="art-coord coord-two">26XVPN.COM</span>
        </div>
      </section>

      <section className="proof-band" aria-label="计划支持的平台">
        <div className="wrap proof-inner">
          <span>ONE ACCOUNT · FOUR PLATFORMS</span>
          <div><b>iOS</b><b>macOS</b><b>Windows</b><b>Android</b></div>
          <span>EARLY DEVELOPMENT</span>
        </div>
      </section>

      <section className="section wrap" id="features">
        <div className="section-heading">
          <div><div className="eyebrow">THE 26x APPROACH</div><h2>先看清每一步，<br /><em>再放心连接。</em></h2></div>
          <p>26x 的产品方向是让 VPN 的状态和管理更清楚。当前处于开发阶段，下面介绍的是设计目标，不是已上线的服务承诺。</p>
        </div>
        <div className="feature-grid">
          {features.map((feature) => (
            <article className="feature-card" key={feature.number}>
              <span>{feature.number}</span>
              <div className="feature-icon" aria-hidden="true">{feature.icon}</div>
              <h3>{feature.title}</h3>
              <p>{feature.body}</p>
              <a href="#faq" aria-label={`了解${feature.title}`}>↗</a>
            </article>
          ))}
        </div>
      </section>

      <section className="platform-section" id="platforms">
        <div className="wrap platform-inner">
          <div className="platform-copy">
            <div className="eyebrow">ONE ACCOUNT · FOUR PLATFORMS</div>
            <h2>从一台设备开始，<br /><em>逐步覆盖日常。</em></h2>
            <p>iOS 优先准备 TestFlight 版本，macOS、Windows 和 Android 客户端同步推进。所有下载入口会在版本可用后开放。</p>
            <a className="button button-light" href="/support">查看支持与更新 <span aria-hidden="true">→</span></a>
          </div>
          <div className="platform-list">
            {platforms.map((platform) => (
              <div className="platform-row" key={platform.name}>
                <span className="platform-icon" aria-hidden="true">{platform.icon}</span>
                <div><strong>{platform.name}</strong><small>{platform.detail}</small></div>
                <span className="platform-status">开发中</span>
                <span className="platform-arrow" aria-hidden="true">↗</span>
              </div>
            ))}
            <p className="platform-disclaimer">目前没有可下载版本或公开测试邀请。</p>
          </div>
        </div>
      </section>

      <section className="faq-section wrap" id="faq">
        <div className="faq-title"><div className="eyebrow">HELP & TRANSPARENCY</div><h2>常见问题</h2><p>功能和发布时间以 26x 官网后续公告为准。</p></div>
        <div className="faq-list">
          {faq.map((item) => (
            <details key={item.question}>
              <summary>{item.question}<span aria-hidden="true">+</span></summary>
              <p>{item.answer}</p>
            </details>
          ))}
        </div>
      </section>

      <section className="closing-band">
        <div className="wrap closing-inner">
          <div><div className="eyebrow">26XVPN.COM</div><h2>为清晰连接而做。</h2></div>
          <a className="button button-light" href="/support">查看开发动态 <span aria-hidden="true">→</span></a>
        </div>
      </section>

      <footer className="footer">
        <div className="wrap footer-main">
          <a className="brand brand-footer" href="#top"><span className="brand-mark">26</span><span className="brand-x">x</span><span className="brand-domain">VPN</span></a>
          <p>Clear routes. Your rhythm.</p>
          <div className="footer-links"><a href="/support">支持与动态</a><a href="/privacy">隐私说明</a><a href="/terms">服务条款</a><a href="/admin">管理入口</a></div>
        </div>
        <div className="wrap footer-meta"><span>© 2026 26x. All rights reserved.</span><span>26x VPN 正在开发中 · 尚未开放服务</span></div>
      </footer>
    </main>
  );
}
