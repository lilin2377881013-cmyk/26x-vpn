import Link from "next/link";

export default function TermsPage() {
  return <main className="legal-shell"><Link className="brand" href="/"><span className="brand-mark">26</span><span className="brand-x">x</span></Link><div className="eyebrow legal-eyebrow">26x / TERMS</div><h1>服务条款</h1><p className="legal-draft">开发稿 · 正式发布前需确认服务地区、套餐、退款和可接受使用政策。</p><h2>服务状态</h2><p>26x 目前处于开发阶段。正式功能、适用地区、价格和服务等级以 26x 发布的正式条款为准。</p><h2>账户与设备</h2><p>正式服务将说明账户安全、设备数量、订阅期限、暂停和注销规则。</p><h2>联系 26x</h2><p>正式客服渠道确认后会在此页公布。</p><Link className="text-link" href="/">返回首页 →</Link></main>;
}
