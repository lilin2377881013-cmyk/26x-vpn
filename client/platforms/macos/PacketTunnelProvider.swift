import NetworkExtension

final class PacketTunnelProvider: NEPacketTunnelProvider {
  override func startTunnel(options: [String: NSObject]?, completionHandler: @escaping (Error?) -> Void) {
    completionHandler(NSError(
      domain: "com.26x.vpn.tunnel",
      code: 1001,
      userInfo: [NSLocalizedDescriptionKey: "26x VPN 核心尚未配置。"]
    ))
  }

  override func stopTunnel(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
    completionHandler()
  }
}
