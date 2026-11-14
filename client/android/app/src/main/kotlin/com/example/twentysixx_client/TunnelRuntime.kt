package com.example.twentysixx_client

import android.os.Handler
import android.os.Looper

/** Process-local status fan-out for the Activity and the VPN service. */
internal object TunnelRuntime {
  private val mainHandler = Handler(Looper.getMainLooper())
  private val listeners = mutableSetOf<(Map<String, Any?>) -> Unit>()

  @Volatile
  private var latest: Map<String, Any?> = mapOf("state" to "disconnected")

  fun snapshot(): Map<String, Any?> = latest

  fun addListener(listener: (Map<String, Any?>) -> Unit) {
    mainHandler.post {
      listeners.add(listener)
      listener(latest)
    }
  }

  fun removeListener(listener: (Map<String, Any?>) -> Unit) {
    mainHandler.post { listeners.remove(listener) }
  }

  fun publish(state: String, code: String? = null) {
    val value = buildMap<String, Any?> {
      put("state", state)
      if (code != null) put("code", code)
      when (state) {
        "connected" -> put("message", "VPN 隧道已启动")
        "disconnected" -> put("message", "VPN 已断开")
        "connecting" -> put("message", "正在启动 VPN 隧道")
        "disconnecting" -> put("message", "正在断开 VPN 隧道")
        "error" -> put("message", "VPN 隧道启动失败")
      }
    }
    mainHandler.post {
      latest = value
      listeners.toList().forEach { it(value) }
    }
  }
}
