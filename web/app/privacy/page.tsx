import Link from "next/link";

export default function PrivacyPage() {
  return <main className="legal-shell"><Link className="brand" href="/"><span className="brand-mark">26</span><span className="brand-x">x</span></Link><div className="eyebrow legal-eyebrow">26x / PRIVACY</div><h1>隐私说明</h1><p className="legal-draft">开发稿 · 正式发布前需由 26x 运营方确认实际数据处理方式。</p><h2>数据最小化原则</h2><p>26x 的产品设计目标是不记录用户访问的网页、DNS 查询目的地或网络内容。账号、设备、订阅状态、连接结果和必要的字节用量可能用于提供服务、排障和执行服务规则。</p><h2>控制与删除</h2><p>正式版本将说明每类数据的用途、保存期限、访问权限和删除方式。当前开发稿不代表生产系统已经完成这些控制。</p><h2>联系 26x</h2><p>正式客服渠道确认后会在此页公布。</p><Link className="text-link" href="/">返回首页 →</Link></main>;
}
