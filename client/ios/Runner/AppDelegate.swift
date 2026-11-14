import Flutter
import UIKit

@main
@objc class AppDelegate: FlutterAppDelegate, FlutterImplicitEngineDelegate {
  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  func didInitializeImplicitFlutterEngine(_ engineBridge: FlutterImplicitEngineBridge) {
    let registry = engineBridge.pluginRegistry
    GeneratedPluginRegistrant.register(with: registry)
    TunnelBridgePlugin.register(with: registry.registrar(forPlugin: "TunnelBridgePlugin"))
  }
}

private final class TunnelBridgePlugin: NSObject, FlutterPlugin, FlutterStreamHandler {
  private var eventSink: FlutterEventSink?

  static func register(with registrar: FlutterPluginRegistrar) {
    let instance = TunnelBridgePlugin()
    let methodChannel = FlutterMethodChannel(
      name: "26x/tunnel",
      binaryMessenger: registrar.messenger()
    )
    registrar.addMethodCallDelegate(instance, channel: methodChannel)

    let eventChannel = FlutterEventChannel(
      name: "26x/tunnel/events",
      binaryMessenger: registrar.messenger()
    )
    eventChannel.setStreamHandler(instance)
  }

  func handle(_ call: FlutterMethodCall, result: @escaping FlutterResult) {
    switch call.method {
    case "status":
      result([
        "state": "unavailable",
        "message": "iOS Packet Tunnel target and VPN core are not configured.",
      ])
    case "connect":
      result(FlutterError(
        code: "engine_unavailable",
        message: "VPN core and Packet Tunnel target are not configured.",
        details: nil
      ))
    case "disconnect":
      result(nil)
    default:
      result(FlutterMethodNotImplemented)
    }
  }

  func onListen(withArguments arguments: Any?, eventSink events: @escaping FlutterEventSink) -> FlutterError? {
    eventSink = events
    events([
      "state": "unavailable",
      "message": "iOS Packet Tunnel target and VPN core are not configured.",
    ])
    return nil
  }

  func onCancel(withArguments arguments: Any?) -> FlutterError? {
    eventSink = nil
    return nil
  }
}
