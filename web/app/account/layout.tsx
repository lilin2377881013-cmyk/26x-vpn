import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = { title: "会员中心", robots: { index: false, follow: false } };

export default function AccountLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <main className="site-shell member-shell">
    <nav className="topbar wrap" aria-label="会员导航"><Link className="brand" href="/" aria-label="26x 首页"><span className="brand-mark">26</span><span className="brand-x">x</span><span className="brand-domain">VPN</span></Link>
      <Link href="/">返回官网</Link></nav>
    <div className="member-wrap">{children}</div>
  </main>;
}
