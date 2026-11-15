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
      load { error in
        if error != nil {
          result(self.failure("tunnel_stop_failed", "无法读取 VPN 配置。"))
          return
        }
        self.manager?.connection.stopVPNTunnel()
        result(nil)
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
              self.waitForConnection(session, deadline: .now() + 35, finish: finish)
            } catch {
              finish(self.failure("tunnel_start_failed", "无法启动 VPN 隧道。"))
            }
          }
        }
      }
    }
  }

  private func waitForConnection(
    _ session: NETunnelProviderSession,
    deadline: DispatchTime,
    sawConnecting: Bool = false,
    finish: @escaping (FlutterError?) -> Void
  ) {
    let status = session.status
    if status == .connected {
      finish(nil)
    } else if sawConnecting && status == .disconnected {
      if #available(iOS 16.0, *) {
        session.fetchLastDisconnectError { error in
          DispatchQueue.main.async {
            finish(self.startupFailure(error))
          }
        }
      } else {
        finish(startupFailure(nil))
      }
    } else if DispatchTime.now() >= deadline {
      session.stopTunnel()
      finish(failure("tunnel_start_failed", "VPN 隧道连接失败。"))
    } else {
      DispatchQueue.main.asyncAfter(deadline: .now() + 0.2) {
        self.waitForConnection(
          session, deadline: deadline,
          sawConnecting: sawConnecting || status == .connecting || status == .reasserting,
          finish: finish
        )
      }
    }
  }

  private func startupFailure(_ error: Error?) -> FlutterError {
    guard let error = error as NSError?, error.domain == "com.26xvpn.client.tunnel" else {
      return failure("tunnel_start_failed", "VPN 隧道连接失败。")
    }
    switch error.code {
    case 1001:
      return failure("profile_invalid", "VPN 配置格式无效。")
    case 1003:
      return failure("tunnel_routes_failed", "系统未能配置 VPN 路由。")
    case 1005:
      return failure("tunnel_core_failed", "VPN 核心启动失败。")
    case 1009, 1010:
      return failure("node_unreachable", "VPN 节点连接检查失败。")
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
