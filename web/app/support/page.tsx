import Link from "next/link";

export default function SupportPage() {
  return <main className="legal-shell"><Link className="brand" href="/"><span className="brand-mark">26</span><span className="brand-x">x</span></Link><div className="eyebrow legal-eyebrow">26x / SUPPORT</div><h1>支持与发布通知</h1><p>26x 客户端正在开发。客服邮箱和发布通知入口确认后会在此更新。</p><p className="legal-draft">官方域名：26xvpn.com · 暂无公开下载或 TestFlight 邀请。</p><h2>当前进度</h2><p>我们正在完善客户端、账户中心与节点管理。当前账户页面可供内部联调，VPN 隧道暂未开放。</p><h2>联系我们</h2><p>客服邮箱和发布通知入口确认后会在此更新。请不要向非 26x 官方渠道提供账户密码或 VPN 配置。</p><Link className="button" href="/">返回 26x 首页 <span aria-hidden="true">→</span></Link></main>;
}
