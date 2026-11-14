import NetworkExtension

final class PacketTunnelProvider: NEPacketTunnelProvider {
  override func startTunnel(
    options: [String: NSObject]?,
    completionHandler: @escaping (Error?) -> Void
  ) {
    // Intentionally fail closed until the protocol engine and a signed,
    // device-bound configuration envelope are integrated.
    let error = NSError(
      domain: "com.26x.vpn.tunnel",
      code: 1001,
      userInfo: [NSLocalizedDescriptionKey: "26x VPN 核心尚未配置。"]
    )
    completionHandler(error)
  }

  override func stopTunnel(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
    completionHandler()
  }
}
