package com.example.twentysixx_client

import android.app.Activity
import android.content.Intent
import android.net.VpnService
import android.os.Build
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel

class MainActivity : FlutterActivity() {
  private var eventSink: EventChannel.EventSink? = null
  private var pendingResult: MethodChannel.Result? = null
  private var pendingConnectConfig: String? = null
  private var pendingOperation: String? = null

  private val runtimeObserver: (Map<String, Any?>) -> Unit = { snapshot ->
    eventSink?.success(snapshot)
    val terminal = when (pendingOperation) {
      "connect" -> snapshot["state"] == "connected" || snapshot["state"] == "error"
      "disconnect" -> snapshot["state"] == "disconnected" || snapshot["state"] == "error"
      else -> false
    }
    if (terminal) {
      pendingResult?.success(snapshot)
      clearPendingOperation()
    }
  }

  override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
    super.configureFlutterEngine(flutterEngine)

    MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "26x/tunnel")
      .setMethodCallHandler { call, result ->
        when (call.method) {
          "status" -> result.success(TunnelRuntime.snapshot())
          "connect" -> requestConnect(call.argument("xrayJson"), result)
          "disconnect" -> requestDisconnect(result)
          else -> result.notImplemented()
        }
      }

    EventChannel(flutterEngine.dartExecutor.binaryMessenger, "26x/tunnel/events")
      .setStreamHandler(object : EventChannel.StreamHandler {
        override fun onListen(arguments: Any?, events: EventChannel.EventSink) {
          eventSink = events
          TunnelRuntime.addListener(runtimeObserver)
        }

        override fun onCancel(arguments: Any?) {
          eventSink = null
          TunnelRuntime.removeListener(runtimeObserver)
        }
      })
  }

  private fun requestConnect(xrayJson: String?, result: MethodChannel.Result) {
    if (xrayJson.isNullOrBlank()) {
      result.error("profile_invalid", "Missing VPN profile", null)
      return
    }
    if (pendingResult != null) {
      result.error("tunnel_start_failed", "Another tunnel operation is pending", null)
      return
    }
    pendingResult = result
    pendingOperation = "connect"
    pendingConnectConfig = xrayJson

    val permissionIntent = VpnService.prepare(this)
    if (permissionIntent == null) {
      startTunnelService()
    } else {
      @Suppress("DEPRECATION")
      startActivityForResult(permissionIntent, VPN_PERMISSION_REQUEST)
    }
  }

  private fun requestDisconnect(result: MethodChannel.Result) {
    if (pendingResult != null) {
      result.error("tunnel_stop_failed", "Another tunnel operation is pending", null)
      return
    }
    if (TunnelRuntime.snapshot()["state"] == "disconnected") {
      result.success(TunnelRuntime.snapshot())
      return
    }
    pendingResult = result
    pendingOperation = "disconnect"
    try {
      startService(Intent(this, VpnTunnelService::class.java).apply {
        action = VpnTunnelService.ACTION_DISCONNECT
      })
    } catch (_: Exception) {
      result.error("tunnel_stop_failed", "Unable to stop VPN service", null)
      clearPendingOperation()
    }
  }

  private fun startTunnelService() {
    val config = pendingConnectConfig
    if (config.isNullOrBlank()) {
      pendingResult?.error("profile_invalid", "Missing VPN profile", null)
      clearPendingOperation()
      return
    }
    try {
      val intent = Intent(this, VpnTunnelService::class.java).apply {
        action = VpnTunnelService.ACTION_CONNECT
        putExtra(VpnTunnelService.EXTRA_XRAY_JSON, config)
      }
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
        startForegroundService(intent)
      } else {
        startService(intent)
      }
    } catch (_: Exception) {
      pendingResult?.error("tunnel_start_failed", "Unable to start VPN service", null)
      clearPendingOperation()
    }
  }

  @Deprecated("Deprecated in Android API")
  override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
    super.onActivityResult(requestCode, resultCode, data)
    if (requestCode != VPN_PERMISSION_REQUEST) return
    if (resultCode == Activity.RESULT_OK) {
      startTunnelService()
    } else {
      pendingResult?.error("vpn_permission_denied", "VPN permission was denied", null)
      clearPendingOperation()
    }
  }

  private fun clearPendingOperation() {
    pendingResult = null
    pendingOperation = null
    pendingConnectConfig = null
  }

  companion object {
    private const val VPN_PERMISSION_REQUEST = 2602
  }
}
