import type { Metadata } from "next";
import "./globals.css";
import "./refinements.css";
import "./site-theme.css";

export const metadata: Metadata = {
  title: { default: "26x VPN — 连接，自有节奏", template: "%s | 26x VPN" },
  description: "26x VPN 多平台客户端与服务中心。项目仍在开发中，暂未开放 VPN 连接。",
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
