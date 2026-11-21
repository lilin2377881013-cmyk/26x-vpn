const messages: Record<string,string> = {
  invalid_credentials: "邮箱或密码不正确，请重新输入。", sign_in_required: "登录已过期，请重新登录。",
  invalid_email: "请填写有效的邮箱地址。", invalid_password: "密码长度须为 12 至 256 字节。",
  email_registered: "此邮箱已注册，请切换到登录。", rate_limited: "操作过于频繁，请稍后重试。",
  csrf_check_failed: "登录状态已更新，请刷新页面后再试。", unlimited_subscription: "当前账号已是永久会员。",
  trial_checkout: "请先登录正式账号再购买。", plan_unavailable: "此套餐暂不可购买，请刷新套餐。",
  payment_maintenance: "支付通道维护中，请稍后再试。", payment_unavailable: "支付暂未开放。",
};

export async function portalRequest(path: string, method: string, body?: object, csrf?: string) {
  const headers: Record<string,string> = { "X-26x-Portal": "1" };
  if (body) headers["Content-Type"] = "application/json";
  if (csrf) headers["X-CSRF-Token"] = csrf;
  let res: Response;
  try {
    res = await fetch("/api/v1/" + path, { method, headers, body: body ? JSON.stringify(body) : undefined,
      credentials: "same-origin", cache: "no-store", signal: AbortSignal.timeout(25000) });
  } catch {
    throw new Error("网络连接中断，请刷新查看最新状态。");
  }
  if (!res.ok) {
    const data = await res.json().catch(() => null);
    const code = data?.error?.code;
    if (code === "sign_in_required") window.location.replace("/account/login");
    throw new Error(messages[code] ?? "请求暂时无法完成，请稍后重试。");
  }
  return res;
}
