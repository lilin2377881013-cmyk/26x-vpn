import type { Metadata } from "next";
import "./globals.css";
import "./refinements.css";
import "./site-theme.css";
import "./member.css";

export const metadata: Metadata = {
  title: { default: "26x VPN — 连接，自有节奏", template: "%s | 26x VPN" },
  description: "26x VPN 官方网站，提供多平台客户端下载、账户与支持服务。",
  metadataBase: new URL(process.env.NEXT_PUBLIC_SITE_URL ?? "https://26xvpn.com"),
  openGraph: {
    siteName: "26x VPN",
    locale: "zh_CN",
    type: "website",
  },
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body>{children}</body>
    </html>
  );
}
