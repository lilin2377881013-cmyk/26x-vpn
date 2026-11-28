import Flutter
import NetworkExtension
import UIKit

@main
@objc class AppDelegate: FlutterAppDelegate, FlutterImplicitEngineDelegate {
  func didInitializeImplicitFlutterEngine(_ engineBridge: FlutterImplicitEngineBridge) {
    let registry = engineBridge.pluginRegistry
    GeneratedPluginRegistrant.register(with: registry)
    if let registrar = registry.registrar(forPlugin: "TunnelBridgePlugin") {
      TunnelBridgePlugin.register(with: registrar)
    }
  }
}

private final class TunnelBridgePlugin: NSObject, FlutterPlugin, FlutterStreamHandler {
  private var eventSink: FlutterEventSink?
  private var manager: NETunnelProviderManager?
  private var observer: NSObjectProtocol?
  private var changingConnection = false
  private let providerID = (Bundle.main.bundleIdentifier ?? "") + ".PacketTunnel"

  static func register(with registrar: FlutterPluginRegistrar) {
    let instance = TunnelBridgePlugin()
    let methods = FlutterMethodChannel(name: "26x/tunnel", binaryMessenger: registrar.messenger())
    registrar.addMethodCallDelegate(instance, channel: methods)
    let events = FlutterEventChannel(name: "26x/tunnel/events", binaryMessenger: registrar.messenger())
    events.setStreamHandler(instance)
  }

  func handle(_ call: FlutterMethodCall, result: @escaping FlutterResult) {
    switch call.method {
    case "deviceModel":
      var hardware = utsname()
      uname(&hardware)
      let model = withUnsafeBytes(of: hardware.machine) { String(decoding: $0.prefix { $0 != 0 }, as: UTF8.self) }
      result(model)
    case "deviceSystem":
      let device = UIDevice.current
      result(["osName": device.userInterfaceIdiom == .pad ? "iPadOS" : "iOS",
              "osVersion": device.systemVersion])
    case "status":
      load { _ in result(self.snapshot()) }
    case "connect":
      guard let arguments = call.arguments as? [String: Any],
            let config = arguments["xrayJson"] as? String, !config.isEmpty else {
        result(failure("profile_invalid", "VPN 配置缺失。"))
        return
      }
      guard !changingConnection else {
        result(failure("tunnel_busy", "VPN 状态正在变更。"))
        return
      }
      changingConnection = true
      connect(config, result: result)
    case "disconnect":
      guard !changingConnection else {
        result(failure("tunnel_busy", "VPN 状态正在变更。"))
        return
      }
      changingConnection = true
      load { error in
        guard error == nil else {
          self.changingConnection = false
          result(self.failure("tunnel_stop_failed", "无法读取 VPN 配置。"))
          return
        }
        guard let connection = self.manager?.connection,
              connection.status != .invalid && connection.status != .disconnected else {
          self.changingConnection = false
          result(["state": "disconnected"])
          return
        }
        connection.stopVPNTunnel()
        TunnelStatusWait.until(.disconnected, on: connection, timeout: 12) { stopped in
          self.changingConnection = false
          if stopped { result(["state": "disconnected"]) }
          else { result(self.failure("tunnel_stop_timeout", "系统未确认 VPN 已断开。")) }
        }
      }
    default:
      result(FlutterMethodNotImplemented)
    }
  }

  func onListen(withArguments arguments: Any?, eventSink events: @escaping FlutterEventSink) -> FlutterError? {
    eventSink = events
    observer = NotificationCenter.default.addObserver(
      forName: .NEVPNStatusDidChange, object: nil, queue: .main
    ) { [weak self] notification in
      guard let self, notification.object as? NEVPNConnection === self.manager?.connection else { return }
      self.publish()
    }
    load { _ in self.publish() }
    return nil
  }

  func onCancel(withArguments arguments: Any?) -> FlutterError? {
    if let observer { NotificationCenter.default.removeObserver(observer) }
    observer = nil
    eventSink = nil
    return nil
  }

  private func load(_ completion: @escaping (Error?) -> Void) {
    if manager != nil {
      completion(nil)
      return
    }
    NETunnelProviderManager.loadAllFromPreferences { managers, error in
      DispatchQueue.main.async {
        if error == nil && self.manager == nil {
          self.manager = managers?.first {
            ($0.protocolConfiguration as? NETunnelProviderProtocol)?.providerBundleIdentifier == self.providerID
          }
        }
        completion(error)
      }
    }
  }

  private func connect(_ config: String, result: @escaping FlutterResult) {
    let finish: (FlutterError?) -> Void = { error in
      self.changingConnection = false
      self.publish()
      if let error {
        result(error)
      } else {
        result(self.snapshot())
      }
    }
    load { error in
      guard error == nil else {
        finish(self.failure("tunnel_start_failed", "无法读取 VPN 配置。"))
        return
      }
      let manager = self.manager ?? NETunnelProviderManager()
      guard manager.connection.status == .disconnected || manager.connection.status == .invalid else {
        finish(self.failure("tunnel_busy", "请先断开当前 VPN 连接。"))
        return
      }
      let tunnel = NETunnelProviderProtocol()
      tunnel.providerBundleIdentifier = self.providerID
      tunnel.serverAddress = "26x VPN"
      manager.protocolConfiguration = tunnel
      manager.localizedDescription = "26x VPN"
      manager.isEnabled = true
      manager.saveToPreferences { error in
        guard error == nil else {
          DispatchQueue.main.async { finish(self.failure("tunnel_start_failed", "无法保存 VPN 配置。")) }
          return
        }
        manager.loadFromPreferences { error in
          DispatchQueue.main.async {
            guard error == nil, let session = manager.connection as? NETunnelProviderSession else {
              finish(self.failure("tunnel_start_failed", "VPN 扩展不可用。"))
              return
            }
            self.manager = manager
            do {
              try session.startTunnel(options: ["xrayJson": config as NSString])
              TunnelStatusWait.until(.connected, on: session, timeout: 35) { connected in
                if connected {
                  finish(nil)
                } else if #available(iOS 16.0, *), session.status == .disconnected {
                  session.fetchLastDisconnectError { error in
                    DispatchQueue.main.async { finish(self.startupFailure(error)) }
                  }
                } else {
                  session.stopTunnel()
                  finish(self.startupFailure(nil))
                }
              }
            } catch {
              finish(self.failure("tunnel_start_failed", "无法启动 VPN 隧道。"))
            }
          }
        }
      }
    }
  }

  private func startupFailure(_ error: Error?) -> FlutterError {
    var cause = error as NSError?
    for _ in 0..<5 {
      guard let current = cause, current.domain != "com.26xvpn.client.tunnel" else { break }
      cause = current.userInfo[NSUnderlyingErrorKey] as? NSError
    }
    guard let error = cause, error.domain == "com.26xvpn.client.tunnel" else {
      return failure("tunnel_start_failed", "VPN 隧道连接失败。")
    }
    switch error.code {
    case 1001:
      return failure("profile_invalid", "VPN 配置格式无效。")
    case 1003:
      return failure("tunnel_routes_failed", "系统未能配置 VPN 路由。")
    case 1005:
      return failure("tunnel_core_failed", "VPN 核心启动失败。")
    case 1006:
      return failure("tunnel_data_failed", "VPN 数据通道已中断。")
    case 1009:
      return failure("node_dns_failed", "无法解析节点地址。")
    case 1010:
      return failure("node_unreachable", "节点 HTTPS 检查失败。")
    case 1011:
      return failure("tunnel_dns_failed", "隧道 DNS 未收到应答。")
    case 1012:
      return failure("probe_execution_failed", "连通性检查模块执行失败。")
    case 1013:
      return failure("tunnel_route_file_unreadable", "无法读取 VPN 分流文件。")
    case 1014:
      return failure("tunnel_route_file_invalid", "VPN 分流文件内容无效。")
    case 1015:
      let systemError = error.userInfo[NSUnderlyingErrorKey] as? NSError
      return FlutterError(code: "tunnel_route_apply_failed", message: "系统未能应用 VPN 网络设置。",
                          details: systemError?.code)
    default:
      return failure("tunnel_start_failed", "VPN 隧道连接失败。")
    }
  }

  private func snapshot() -> [String: String] {
    let state: String
    switch manager?.connection.status ?? .disconnected {
    case .connecting, .reasserting: state = "connecting"
    case .connected: state = "connected"
    case .disconnecting: state = "disconnecting"
    case .disconnected, .invalid: state = "disconnected"
    @unknown default: state = "error"
    }
    return ["state": state]
  }

  private func publish() { eventSink?(snapshot()) }

  private func failure(_ code: String, _ message: String) -> FlutterError {
    FlutterError(code: code, message: message, details: nil)
  }
}
