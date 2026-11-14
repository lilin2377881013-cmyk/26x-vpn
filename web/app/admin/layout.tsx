import type { Metadata } from "next";

export const metadata: Metadata = {
  title: "管理控制台",
  robots: { index: false, follow: false },
};

export default async function AdminLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return children;
}
