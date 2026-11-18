import Link from "next/link";

const sections = [
  { title: "工作台", links: [["概览", "/admin"]] },
  { title: "资源管理", links: [["节点管理", "/admin/nodes"], ["用户管理", "/admin?tab=用户"], ["设备管理", "/admin?tab=设备"], ["套餐管理", "/admin?tab=套餐"], ["支付订单", "/admin/orders"]] },
  { title: "运营支持", links: [["用户流量", "/admin/users"], ["客服留言", "/admin/support"], ["下载管理", "/admin/downloads"], ["代理推广", "/admin/referrals"], ["审计记录", "/admin?tab=审计记录"]] },
];

export function AdminNav({ current, footer, demo = false }: { current: string; footer?: React.ReactNode; demo?: boolean }) {
  return <aside className="admin-sidebar">
    <Link className="brand admin-brand" href="/admin"><span className="brand-mark">26</span><span className="brand-x">x</span><small>管理后台</small></Link>
    <nav aria-label="后台导航">
      {sections.map(({ title, links }) => <div className="admin-nav-group" key={title}>
        <div className="sidebar-label">{title}</div>
        {links.map(([label, href]) => {
          const target = demo && href === "/admin/nodes" ? "/admin?tab=节点" : href;
          if (demo && (href === "/admin/users" || href === "/admin/support" || href === "/admin/orders" || href === "/admin/referrals")) return null;
          const selected = current === target || (target !== "/admin" && current.startsWith(target + "/"));
          return <Link className={selected ? "admin-nav active" : "admin-nav"} aria-current={selected ? "page" : undefined}
            href={target} key={target}>{label}</Link>;
        })}
      </div>)}
    </nav>
    {footer && <div className="sidebar-bottom">{footer}</div>}
  </aside>;
}
