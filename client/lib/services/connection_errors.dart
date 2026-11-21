// Public diagnostics identify the failing stage without exposing URLs or keys.
String apiRequestStage(String method, String path) {
  if (path.endsWith('/connection-lease')) {
    return method == 'DELETE'
        ? '名额释放 [API-LEASE-RELEASE]'
        : '名额申请 [API-LEASE-ACQUIRE]';
  }
  if (path.startsWith('/api/v1/nodes') ||
      path.startsWith('/api/v1/client/bootstrap')) {
    return '节点列表 [API-NODES]';
  }
  if (path.endsWith('/credentials')) return '节点配置 [API-CONFIG]';
  if (path.contains('/checkout')) return '会员下单 [API-PAYMENT]';
  if (path == '/api/v1/auth/session') {
    return method == 'DELETE' ? '退出账号 [API-SIGNOUT]' : '账号登录 [API-AUTH]';
  }
  if (path == '/api/v1/auth/register') return '注册账号 [API-REGISTER]';
  if (path == '/api/v1/auth/trial') return '启动试用 [API-TRIAL-START]';
  if (path.endsWith('/devices')) return '登录设备 [API-DEVICES]';
  return '账号服务 [API-ACCOUNT]';
}

String tunnelErrorMessage(String code, {bool windows = false}) =>
    switch (code) {
      'vpn_permission_denied' =>
        windows
            ? '系统授权 [VPN-AUTH]：请以管理员身份运行 26x VPN。'
            : '系统授权 [VPN-AUTH]：未获得 VPN 权限，请允许后重试。',
      'profile_invalid' => '节点配置 [VPN-CONFIG]：配置格式无效，请联系支持。',
      'engine_unavailable' => '核心安装 [VPN-INSTALL]：VPN 核心尚未安装。',
      'extension_activation_failed' =>
        '系统扩展 [VPN-EXTENSION]：macOS 未能启用 VPN 扩展。',
      'extension_restart_required' => '系统扩展 [VPN-RESTART]：请重启 Mac 后再连接。',
      'tunnel_busy' => '连接操作 [VPN-BUSY]：上一次操作仍在处理中，请稍后重试。',
      'node_dns_failed' => '节点解析 [VPN-NODE-DNS]：无法解析节点服务器地址。',
      'node_unreachable' => 'HTTPS 检查 [VPN-HTTPS]：经节点访问测试网站失败，请更换节点或稍后重试。',
      'probe_execution_failed' => '连通性检查 [VPN-CHECK]：检测模块未能完成检查，请重试。',
      'tunnel_dns_failed' => '隧道 DNS [VPN-TUN-DNS]：隧道启动后未收到 DNS 应答。',
      'tunnel_data_failed' => '隧道传输 [VPN-DATA]：VPN 数据通道已中断。',
      'tunnel_routes_failed' => '系统路由 [VPN-ROUTES]：系统未能配置 VPN 路由。',
      'tunnel_core_failed' => '核心启动 [VPN-CORE]：VPN 核心启动失败。',
      'tunnel_start_timeout' => '隧道启动 [VPN-START-TIMEOUT]：启动超时，已取消，请重新连接。',
      'worker_exit_2' => '隧道进程 [VPN-WORKER-EXIT-2]：启动时意外退出，退出码 2。',
      'worker_cleanup_timeout' => '隧道清理 [VPN-CLEANUP-TIMEOUT]：关闭过程超时，隧道进程已退出。',
      'worker_dns_rollback_timeout' =>
        '隧道 DNS [VPN-DNS-ROLLBACK]：DNS 检查失败，清理超时后隧道进程已退出。',
      'worker_data_rollback_timeout' =>
        '隧道传输 [VPN-DATA-ROLLBACK]：HTTPS 检查失败，清理超时后隧道进程已退出。',
      'tunnel_start_failed' => '隧道启动 [VPN-START]：系统未能启动 VPN 隧道。',
      'tunnel_stop_failed' => '隧道断开 [VPN-STOP]：系统未能正常断开 VPN。',
      'tunnel_stop_timeout' => '隧道断开 [VPN-STOP-TIMEOUT]：断开超时，请检查系统 VPN 状态。',
      _ => '隧道操作 [VPN-UNKNOWN]：操作失败，请联系支持并提供此错误码。',
    };
