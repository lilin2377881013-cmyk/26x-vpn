package com.example.twentysixx_client

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import libXray.LibXray
import org.json.JSONObject
import java.util.concurrent.Executors

/** Owns the Android TUN descriptor and the single libXray runtime. */
class VpnTunnelService : VpnService() {
  private val worker = Executors.newSingleThreadExecutor()
  private var tunInterface: ParcelFileDescriptor? = null
  private var dialerController: LibXray.DialerController? = null

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    when (intent?.action) {
      ACTION_CONNECT -> {
        val xrayJson = intent.getStringExtra(EXTRA_XRAY_JSON)
        if (xrayJson.isNullOrBlank()) {
          TunnelRuntime.publish("error", "profile_invalid")
          stopSelf(startId)
          return START_NOT_STICKY
        }
        TunnelRuntime.publish("connecting")
        worker.execute { connect(xrayJson, startId) }
      }

      ACTION_DISCONNECT -> {
        TunnelRuntime.publish("disconnecting")
        worker.execute { disconnect(startId) }
      }

      else -> stopSelf(startId)
    }
    return START_NOT_STICKY
  }

  private fun connect(rawConfig: String, startId: Int) {
    try {
      if (tunInterface != null) {
        TunnelRuntime.publish("connected")
        return
      }

      val descriptor = Builder()
        .setSession("26x VPN")
        .setMtu(1500)
        .addAddress("10.26.0.1", 30)
        .addAddress("fd26:26::1", 126)
        .addRoute("0.0.0.0", 0)
        .addRoute("::", 0)
        .addDnsServer("1.1.1.1")
        .establish() ?: throw TunnelStartException()
      tunInterface = descriptor
      startInForeground()

      val controller = object : LibXray.DialerController {
        override fun protectFd(fd: Int): Boolean = protect(fd)
      }
      dialerController = controller
      LibXray.registerDialerController(controller)
      LibXray.setDNS(controller, "1.1.1.1:53")

      val config = JSONObject(rawConfig)
      val env = config.optJSONObject("env") ?: JSONObject().also {
        config.put("env", it)
      }
      env.put("xray.tun.fd", descriptor.fd)

      val request = JSONObject()
        .put("apiVersion", 3)
        .put("method", "runXray")
        .put("payload", JSONObject().put("xrayJson", config.toString()))
      val response = JSONObject(LibXray.invoke(request.toString()))
      if (!response.optBoolean("success")) throw TunnelStartException()

      TunnelRuntime.publish("connected")
    } catch (_: Throwable) {
      stopCoreQuietly()
      closeTun()
      stopForegroundCompat()
      TunnelRuntime.publish("error", "tunnel_start_failed")
      stopSelf(startId)
    }
  }

  private fun disconnect(startId: Int) {
    try {
      val request = JSONObject()
        .put("apiVersion", 3)
        .put("method", "stopXray")
      val response = JSONObject(LibXray.invoke(request.toString()))
      if (!response.optBoolean("success")) throw TunnelStopException()
      LibXray.resetDNS()
      closeTun()
      TunnelRuntime.publish("disconnected")
    } catch (_: Throwable) {
      closeTun()
      TunnelRuntime.publish("error", "tunnel_stop_failed")
    } finally {
      stopForegroundCompat()
      stopSelf(startId)
    }
  }

  private fun stopCoreQuietly() {
    try {
      LibXray.invoke(
        JSONObject().put("apiVersion", 3).put("method", "stopXray").toString(),
      )
      LibXray.resetDNS()
    } catch (_: Throwable) {
      // The service still closes the OS-owned descriptor below.
    }
  }

  private fun closeTun() {
    try {
      tunInterface?.close()
    } catch (_: Exception) {
      // Closing a TUN descriptor is best-effort during teardown.
    } finally {
      tunInterface = null
      dialerController = null
    }
  }

  @Suppress("DEPRECATION")
  private fun startInForeground() {
    val manager = getSystemService(NOTIFICATION_SERVICE) as NotificationManager
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
      manager.createNotificationChannel(
        NotificationChannel(CHANNEL_ID, "26x VPN", NotificationManager.IMPORTANCE_LOW),
      )
    }
    val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
      Notification.Builder(this, CHANNEL_ID)
    } else {
      Notification.Builder(this)
    }
    val notification = builder
      .setSmallIcon(android.R.drawable.stat_sys_warning)
      .setContentTitle("26x VPN")
      .setContentText("正在保护网络连接")
      .setOngoing(true)
      .setCategory(Notification.CATEGORY_SERVICE)
      .build()
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
      startForeground(NOTIFICATION_ID, notification, android.content.pm.ServiceInfo.FOREGROUND_SERVICE_TYPE_SYSTEM_EXEMPTED)
    } else {
      startForeground(NOTIFICATION_ID, notification)
    }
  }

  private fun stopForegroundCompat() {
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
      stopForeground(STOP_FOREGROUND_REMOVE)
    } else {
      @Suppress("DEPRECATION")
      stopForeground(true)
    }
  }

  override fun onRevoke() {
    TunnelRuntime.publish("disconnecting")
    worker.execute {
      stopCoreQuietly()
      closeTun()
      stopForegroundCompat()
      TunnelRuntime.publish("disconnected")
      stopSelf()
    }
    super.onRevoke()
  }

  override fun onDestroy() {
    worker.shutdown()
    super.onDestroy()
  }

  private class TunnelStartException : Exception()
  private class TunnelStopException : Exception()

  companion object {
    const val ACTION_CONNECT = "com.example.twentysixx_client.CONNECT"
    const val ACTION_DISCONNECT = "com.example.twentysixx_client.DISCONNECT"
    const val EXTRA_XRAY_JSON = "xrayJson"
    private const val CHANNEL_ID = "26x_vpn"
    private const val NOTIFICATION_ID = 2601
  }
}
