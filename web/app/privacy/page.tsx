import Link from "next/link";

export default function PrivacyPage() {
  return <main className="legal-shell">
    <Link className="brand" href="/" aria-label="26x 首页"><span className="brand-mark">26</span><span className="brand-x">x</span></Link>
    <h1>隐私说明</h1>
    <h2>账号与设备</h2><p>官网和客户端使用同一个邮箱账号。密码以加盐哈希保存。登录官网时，浏览器会保存会话和请求验证 Cookie，用于识别账号和保护付款等操作。网页登录不会登记为 VPN 设备。</p>
    <h2>连接与服务</h2><p>客户端登记设备信息。服务端使用设备、连接名额、订阅状态和流量计数来提供连接、执行套餐规则和排查故障。</p>
    <h2>订单与付款</h2><p>开通或续费时，订单会关联到当前账号。系统使用订单号、金额和支付状态核对到账。USDT 使用公开区块链，链上转账记录可公开查询。</p>
    <h2>推广统计</h2><p>通过推广链接访问官网时，系统按日期和设备类别汇总访问和下载点击次数，供推广方查看。</p>
    <p>有关账号或订单的数据问题，可在客户端个人中心联系 26x。</p>
    <Link className="text-link" href="/">返回官网 →</Link>
  </main>;
}
