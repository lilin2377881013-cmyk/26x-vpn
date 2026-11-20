package com.example.twentysixx_client

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Intent
import android.net.IpPrefix
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import libXray.DialerController
import libXray.LibXray
import org.json.JSONArray
import org.json.JSONObject
import java.net.InetAddress
import java.util.concurrent.Executors

/** Owns the Android TUN descriptor and the single libXray runtime. */
class VpnTunnelService : VpnService() {
  private val worker = Executors.newSingleThreadExecutor()
  private var tunInterface: ParcelFileDescriptor? = null

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

      startInForeground()
      val config = ipv4TunnelConfig(rawConfig)
      val ping = JSONObject()
        .put("apiVersion", 3)
        .put("method", "pingBatch")
        .put("payload", JSONObject()
          .put("configs", JSONArray().put(JSONObject()
            .put("xrayJson", config.toString())
            .put("outboundTag", "26x-proxy")))
          .put("timeout", 8)
          .put("url", "https://cp.cloudflare.com/generate_204"))
      val checked = JSONObject(LibXray.invoke(ping.toString()))
      if (checked.optJSONObject("data")?.optJSONArray("results")
          ?.optJSONObject(0)?.optBoolean("success") != true) {
        stopForegroundCompat()
        TunnelRuntime.publish("error", if (checked.optBoolean("success")) "node_unreachable" else "probe_execution_failed")
        stopSelf(startId)
        return
      }
      val excluded = assets.open("direct-ipv4-cidrs.txt").bufferedReader().use {
        Ipv4Routes.parse(it.readText())
      }
      val builder = Builder()
        .setSession("26x VPN")
        .setMtu(1500)
        .addAddress("10.26.0.1", 30)
        .addAddress("fd26:26::1", 126)
        .addRoute("::", 0)
        .addDnsServer("1.1.1.1")
        .addDnsServer("1.0.0.1")
      if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        builder.addRoute("0.0.0.0", 0)
        for (network in excluded) {
          builder.excludeRoute(IpPrefix(InetAddress.getByName(network.address), network.prefix))
        }
      } else {
        for (network in Ipv4Routes.complement(excluded)) {
          builder.addRoute(network.address, network.prefix)
        }
      }
      val descriptor = builder.establish() ?: throw TunnelStartException()
      tunInterface = descriptor

      activeService = this
      if (!dialerRegistered) {
        LibXray.registerDialerController(dialerController)
        dialerRegistered = true
      }
      LibXray.setDNS(dialerController, "1.1.1.1:53")

      // VpnService owns the routing table. The protected dialer handles
      // outbound sockets without binding to a stale physical interface name.
      val tun = config.getJSONArray("inbounds").getJSONObject(0).getJSONObject("settings")
      tun.remove("autoOutboundsInterface")
      tun.remove("autoSystemRoutingTable")
      val env = config.optJSONObject("env") ?: JSONObject().also {
        config.put("env", it)
      }
      env.put("xray.tun.fd", descriptor.fd.toString())

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

  private fun ipv4TunnelConfig(rawConfig: String): JSONObject {
    val config = JSONObject(rawConfig)
    config.put("dns", JSONObject()
      .put("tag", "26x-doh")
      .put("queryStrategy", "UseIPv4")
      .put("servers", JSONArray()
        .put("https://1.1.1.1/dns-query")
        .put("https://1.0.0.1/dns-query")))
    config.getJSONArray("outbounds").put(JSONObject()
      .put("tag", "26x-dns").put("protocol", "dns"))
    val routing = config.getJSONObject("routing")
    val previous = routing.getJSONArray("rules")
    val rules = JSONArray()
      .put(JSONObject().put("type", "field")
        .put("inboundTag", JSONArray().put("26x-doh"))
        .put("outboundTag", "26x-proxy"))
      .put(JSONObject().put("type", "field")
        .put("inboundTag", JSONArray().put("26x-tun"))
        .put("port", "53").put("network", "tcp,udp")
        .put("outboundTag", "26x-dns"))
      .put(JSONObject().put("type", "field")
        .put("inboundTag", JSONArray().put("26x-tun"))
        .put("ip", JSONArray().put("::/0"))
        .put("outboundTag", "block"))
    for (index in 0 until previous.length()) rules.put(previous.get(index))
    routing.put("rules", rules)
    return config
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
      if (activeService == this) activeService = null
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
      .setSmallIcon(com.example.twentysixx_client.R.drawable.ic_vpn_status)
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
    worker.execute {
      if (tunInterface != null) {
        stopCoreQuietly()
        closeTun()
        TunnelRuntime.publish("disconnected")
      }
    }
    worker.shutdown()
    super.onDestroy()
  }

  private class TunnelStartException : Exception()
  private class TunnelStopException : Exception()

  companion object {
    @Volatile private var activeService: VpnTunnelService? = null
    private var dialerRegistered = false
    private val dialerController = object : DialerController {
      override fun protectFd(fd: Long): Boolean = activeService?.protect(fd.toInt()) ?: true
    }
    const val ACTION_CONNECT = "com.example.twentysixx_client.CONNECT"
    const val ACTION_DISCONNECT = "com.example.twentysixx_client.DISCONNECT"
    const val EXTRA_XRAY_JSON = "xrayJson"
    private const val CHANNEL_ID = "26x_vpn"
    private const val NOTIFICATION_ID = 2601
  }
}
