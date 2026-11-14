import Cocoa
import FlutterMacOS
import NetworkExtension
import SystemExtensions

class MainFlutterWindow: NSWindow {
  private let tunnelEvents = TunnelBridgeEventHandler()

  override func awakeFromNib() {
    let flutterViewController = FlutterViewController()
    let windowFrame = self.frame
    self.contentViewController = flutterViewController
    self.setFrame(windowFrame, display: true)
    RegisterGeneratedPlugins(registry: flutterViewController)
    registerTunnelBridge(on: flutterViewController)
    super.awakeFromNib()
  }

  private func registerTunnelBridge(on controller: FlutterViewController) {
    let messenger = controller.engine.binaryMessenger
    let commands = FlutterMethodChannel(name: "26x/tunnel", binaryMessenger: messenger)
    commands.setMethodCallHandler { call, result in
      switch call.method {
      case "status":
        MacTunnelManager.load(createIfMissing: false) { manager, error in
          if let error {
            result(FlutterError(code: "tunnel_status_failed", message: error.localizedDescription, details: nil))
            return
          }
          result(MacTunnelManager.snapshot(manager?.connection.status ?? .disconnected))
        }
      case "connect":
        guard let arguments = call.arguments as? [String: Any],
              let xrayJson = arguments["xrayJson"] as? String,
              !xrayJson.isEmpty else {
          result(FlutterError(code: "profile_invalid", message: "VPN profile is missing.", details: nil))
          return
        }
        MacTunnelManager.connect(xrayJson: xrayJson, needsApproval: { [weak self] in
          self?.tunnelEvents.emit([
            "state": "connecting",
            "message": "请在系统设置中允许 26x VPN 系统扩展。",
          ])
        }) { error in
          if let error {
            result(FlutterError(code: error.code, message: error.message, details: nil))
          } else {
            result(["state": "connected"])
          }
        }
      case "disconnect":
        MacTunnelManager.disconnect { error in
          if let error {
            result(FlutterError(code: "tunnel_stop_failed", message: error.localizedDescription, details: nil))
          } else {
            result(["state": "disconnected"])
          }
        }
      default:
        result(FlutterMethodNotImplemented)
      }
    }

    let events = FlutterEventChannel(name: "26x/tunnel/events", binaryMessenger: messenger)
    events.setStreamHandler(tunnelEvents)
  }
}

private struct TunnelStartError {
  let code: String
  let message: String
}

private enum MacTunnelManager {
  private static let description = "26x VPN"
  private static let startupTimeout: TimeInterval = 30
  private static let shutdownTimeout: TimeInterval = 12
  private(set) static var currentManager: NETunnelProviderManager?
  private static var activation: TunnelExtensionActivation?
  private static var isConnecting = false
  private static var providerID: String? {
    Bundle.main.object(forInfoDictionaryKey: "26xTunnelExtensionIdentifier") as? String
  }

  static func load(
    createIfMissing: Bool,
    completion: @escaping (NETunnelProviderManager?, Error?) -> Void
  ) {
    if let currentManager {
      completion(currentManager, nil)
      return
    }
    NETunnelProviderManager.loadAllFromPreferences { managers, error in
      DispatchQueue.main.async {
        if let error {
          completion(nil, error)
          return
        }
        if let currentManager {
          completion(currentManager, nil)
          return
        }
        let existing = managers?.first {
          guard let providerID else { return false }
          return ($0.protocolConfiguration as? NETunnelProviderProtocol)?.providerBundleIdentifier == providerID
        }
        if let existing {
          currentManager = existing
        } else if createIfMissing {
          currentManager = NETunnelProviderManager()
        }
        completion(currentManager, nil)
      }
    }
  }

  static func snapshot(_ status: NEVPNStatus) -> [String: String] {
    let state: String
    switch status {
    case .connecting, .reasserting: state = "connecting"
    case .connected: state = "connected"
    case .disconnecting: state = "disconnecting"
    case .invalid: state = "unavailable"
    case .disconnected: state = "disconnected"
    @unknown default: state = "error"
    }
    return ["state": state]
  }

  static func connect(
    xrayJson: String,
    needsApproval: @escaping () -> Void,
    completion: @escaping (TunnelStartError?) -> Void
  ) {
    guard !isConnecting else {
      completion(TunnelStartError(code: "tunnel_busy", message: "VPN 连接正在建立。"))
      return
    }
    guard let providerID, !providerID.isEmpty else {
      completion(TunnelStartError(code: "engine_unavailable", message: "VPN 系统扩展未配置。"))
      return
    }
    isConnecting = true
    let finished: (TunnelStartError?) -> Void = { error in
      isConnecting = false
      completion(error)
    }
    let request = OSSystemExtensionRequest.activationRequest(
      forExtensionWithIdentifier: providerID, queue: .main
    )
    let delegate = TunnelExtensionActivation(needsApproval: needsApproval) { error in
      activation = nil
      if let error {
        finished(error)
      } else {
        startVPN(xrayJson: xrayJson, providerID: providerID, completion: finished)
      }
    }
    activation = delegate
    request.delegate = delegate
    OSSystemExtensionManager.shared.submitRequest(request)
  }

  private static func startVPN(
    xrayJson: String, providerID: String, completion: @escaping (TunnelStartError?) -> Void
  ) {
    load(createIfMissing: true) { manager, error in
      if let error {
        completion(TunnelStartError(code: "tunnel_start_failed", message: error.localizedDescription))
        return
      }
      guard let manager else {
        completion(TunnelStartError(code: "tunnel_start_failed", message: "VPN settings could not be loaded."))
        return
      }

      let tunnel = (manager.protocolConfiguration as? NETunnelProviderProtocol) ?? NETunnelProviderProtocol()
      guard manager.connection.status == .disconnected || manager.connection.status == .invalid else {
        completion(TunnelStartError(code: "tunnel_busy", message: "请先断开当前 VPN 连接。"))
        return
      }
      tunnel.providerBundleIdentifier = providerID
      tunnel.serverAddress = "26x"
      manager.protocolConfiguration = tunnel
      manager.localizedDescription = description
      manager.isEnabled = true

      manager.saveToPreferences { saveError in
        if let saveError {
          completion(TunnelStartError(code: "tunnel_start_failed", message: saveError.localizedDescription))
          return
        }
        manager.loadFromPreferences { loadError in
          if let loadError {
            completion(TunnelStartError(code: "tunnel_start_failed", message: loadError.localizedDescription))
            return
          }
          do {
            guard let session = manager.connection as? NETunnelProviderSession else {
              completion(TunnelStartError(code: "tunnel_start_failed", message: "The VPN extension is unavailable."))
              return
            }
            try session.startTunnel(options: ["xrayJson": xrayJson as NSString])
            waitForStatus(manager, expected: .connected, timeout: startupTimeout) { connected in
              if connected {
                completion(nil)
              } else {
                let report: (Error?) -> Void = { error in
                  let underlying = error as NSError?
                  let nodeFailure = underlying?.domain == "com.26xvpn.client.tunnel" && underlying?.code == 1010
                  let code = nodeFailure ? "node_unreachable" : "tunnel_start_failed"
                  completion(TunnelStartError(code: code, message: "VPN 隧道连接失败。"))
                }
                if #available(macOS 13.0, *), session.status == .disconnected {
                  session.fetchLastDisconnectError { error in DispatchQueue.main.async { report(error) } }
                } else {
                  session.stopTunnel()
                  report(nil)
                }
              }
            }
          } catch {
            completion(TunnelStartError(code: "tunnel_start_failed", message: error.localizedDescription))
          }
        }
      }
    }
  }

  static func disconnect(completion: @escaping (Error?) -> Void) {
    load(createIfMissing: false) { manager, error in
      if let error {
        completion(error)
        return
      }
      guard let manager else {
        completion(nil)
        return
      }
      guard let session = manager.connection as? NETunnelProviderSession else {
        completion(nil)
        return
      }
      if session.status == .disconnected || session.status == .invalid {
        completion(nil)
        return
      }
      session.stopTunnel()
      waitForStatus(manager, expected: .disconnected, timeout: shutdownTimeout) { stopped in
        if stopped {
          completion(nil)
        } else {
          completion(NSError(domain: "com.26x.vpn", code: 2, userInfo: [NSLocalizedDescriptionKey: "macOS did not stop the VPN tunnel."]))
        }
      }
    }
  }

  private static func waitForStatus(
    _ manager: NETunnelProviderManager,
    expected: NEVPNStatus,
    timeout: TimeInterval,
    completion: @escaping (Bool) -> Void
  ) {
    let deadline = DispatchTime.now() + timeout
    var sawConnecting = false
    func poll() {
      let status = manager.connection.status
      if status == expected {
        completion(true)
      } else if expected == .disconnected && status == .invalid {
        completion(true)
      } else if DispatchTime.now() >= deadline ||
                  (expected == .connected && sawConnecting && (status == .invalid || status == .disconnected)) {
        completion(false)
      } else {
        sawConnecting = sawConnecting || status == .connecting || status == .reasserting
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.2, execute: poll)
      }
    }
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.2, execute: poll)
  }
}

private final class TunnelExtensionActivation: NSObject, OSSystemExtensionRequestDelegate {
  private let needsApproval: () -> Void
  private let completion: (TunnelStartError?) -> Void

  init(needsApproval: @escaping () -> Void, completion: @escaping (TunnelStartError?) -> Void) {
    self.needsApproval = needsApproval
    self.completion = completion
  }

  func requestNeedsUserApproval(_ request: OSSystemExtensionRequest) {
    needsApproval()
  }

  func request(_ request: OSSystemExtensionRequest, didFinishWithResult result: OSSystemExtensionRequest.Result) {
    if result == .completed {
      completion(nil)
    } else {
      completion(TunnelStartError(code: "extension_restart_required", message: "重启 Mac 后再连接。"))
    }
  }

  func request(_ request: OSSystemExtensionRequest, didFailWithError error: Error) {
    completion(TunnelStartError(code: "extension_activation_failed", message: error.localizedDescription))
  }

  func request(
    _ request: OSSystemExtensionRequest,
    actionForReplacingExtension existing: OSSystemExtensionProperties,
    withExtension replacement: OSSystemExtensionProperties
  ) -> OSSystemExtensionRequest.ReplacementAction {
    .replace
  }
}

private final class TunnelBridgeEventHandler: NSObject, FlutterStreamHandler {
  private var eventSink: FlutterEventSink?
  private var statusObserver: NSObjectProtocol?

  func onListen(withArguments arguments: Any?, eventSink events: @escaping FlutterEventSink) -> FlutterError? {
    eventSink = events
    statusObserver = NotificationCenter.default.addObserver(
      forName: .NEVPNStatusDidChange,
      object: nil,
      queue: .main
    ) { [weak self] notification in
      guard let connection = notification.object as? NEVPNConnection,
            let current = MacTunnelManager.currentManager?.connection,
            connection === current else { return }
      self?.emit(MacTunnelManager.snapshot(current.status))
      if current.status == .disconnected {
        current.fetchLastDisconnectError { [weak self, weak current] error in
          DispatchQueue.main.async {
            guard current?.status == .disconnected,
                  let error = error as NSError?,
                  error.domain == "com.26xvpn.client.tunnel", error.code == 1006 else { return }
            self?.emit(["state": "error", "message": "VPN 数据通道已中断，请重新连接。"])
          }
        }
      }
    }
    MacTunnelManager.load(createIfMissing: false) { [weak self] manager, _ in
      self?.emit(MacTunnelManager.snapshot(manager?.connection.status ?? .disconnected))
    }
    return nil
  }

  func emit(_ state: [String: String]) {
    eventSink?(state)
  }

  func onCancel(withArguments arguments: Any?) -> FlutterError? {
    if let statusObserver {
      NotificationCenter.default.removeObserver(statusObserver)
      self.statusObserver = nil
    }
    eventSink = nil
    return nil
  }
}
