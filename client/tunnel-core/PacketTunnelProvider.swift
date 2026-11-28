import Darwin
import Foundation
import LibXray
import NetworkExtension

final class PacketTunnelProvider: NEPacketTunnelProvider {
  private let queue = DispatchQueue(label: "com.26xvpn.client.tunnel")
  private var bridge: PacketBridge?
  private var startup: ((Error?) -> Void)?
  private var generation = 0
  private var coreStarted = false

  override func startTunnel(
    options: [String: NSObject]?,
    completionHandler: @escaping (Error?) -> Void
  ) {
    let xrayJson = options?["xrayJson"] as? String
    queue.async {
      guard self.startup == nil, self.bridge == nil else {
        completionHandler(self.tunnelError(1001, "VPN 隧道正在运行。"))
        return
      }
      self.generation += 1
      let generation = self.generation
      self.startup = completionHandler
      do {
        guard let xrayJson else {
          throw self.tunnelError(1001, "VPN 配置缺失。")
        }
        let (config, address) = try self.prepareConfig(xrayJson)
        try self.checkOutbound(config)
        let network = try self.networkSettings(address: address)
        self.setTunnelNetworkSettings(network) { error in
          self.queue.async {
            guard self.generation == generation else { return }
            if let systemError = error as NSError? {
              let cause = NSError(domain: systemError.domain, code: systemError.code)
              NSLog("26x VPN route settings failed (%@ %ld)", cause.domain, cause.code)
              self.failStart(NSError(domain: "com.26xvpn.client.tunnel", code: 1015, userInfo: [
                NSLocalizedDescriptionKey: "系统未能应用 VPN 网络设置。",
                NSUnderlyingErrorKey: cause,
              ]))
              return
            }
            do {
              try self.startCore(config, generation: generation)
            } catch {
              self.failStart(error)
            }
          }
        }
      } catch {
        self.finishStart(error)
      }
    }
  }

  override func stopTunnel(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
    queue.async {
      self.generation += 1
      self.stopCore()
      self.finishStart(self.tunnelError(1008, "VPN 连接已取消。"))
      // The system removes the routes and DNS settings when it stops the provider.
      completionHandler()
    }
  }

  private func prepareConfig(_ json: String) throws -> ([String: Any], String) {
    guard var config = (try? JSONSerialization.jsonObject(with: Data(json.utf8))) as? [String: Any],
          var inbounds = config["inbounds"] as? [[String: Any]],
          inbounds.count == 1,
          inbounds[0]["protocol"] as? String == "tun",
          var tun = inbounds[0]["settings"] as? [String: Any],
          var outbounds = config["outbounds"] as? [[String: Any]],
          let index = outbounds.firstIndex(where: { $0["tag"] as? String == "26x-proxy" }),
          var settings = outbounds[index]["settings"] as? [String: Any],
          let host = settings["address"] as? String,
          !host.isEmpty else {
      throw tunnelError(1001, "VPN 配置格式无效。")
    }

    // Network Extension owns routes. The socket pair has no interface index.
    tun.removeValue(forKey: "autoOutboundsInterface")
    tun.removeValue(forKey: "autoSystemRoutingTable")
    inbounds[0]["settings"] = tun
    config["inbounds"] = inbounds
    let address = try resolveAddress(host)
    settings["address"] = address
    outbounds[index]["settings"] = settings
    // Resolve through the proxy over IPv4 HTTPS. IPv4-only nodes must not
    // advertise AAAA answers that the browser cannot reach through the tunnel.
    config["dns"] = [
      "tag": "26x-doh",
      "queryStrategy": "UseIPv4",
      "servers": ["https://1.1.1.1/dns-query", "https://1.0.0.1/dns-query"],
    ]
    outbounds.append(["tag": "26x-dns", "protocol": "dns"])
    config["outbounds"] = outbounds
    guard var routing = config["routing"] as? [String: Any],
          let rules = routing["rules"] as? [[String: Any]] else {
      throw tunnelError(1001, "VPN 路由配置格式无效。")
    }
    routing["rules"] = [
      ["type": "field", "inboundTag": ["26x-doh"], "outboundTag": "26x-proxy"],
      ["type": "field", "inboundTag": ["26x-tun"], "port": "53",
       "network": "tcp,udp", "outboundTag": "26x-dns"],
      ["type": "field", "inboundTag": ["26x-tun"], "ip": ["::/0"],
       "outboundTag": "block"],
    ] + rules
    config["routing"] = routing
    config["log"] = ["loglevel": "none"]
    config.removeValue(forKey: "env")
    return (config, address)
  }

  private func resolveAddress(_ host: String) throws -> String {
    var result: UnsafeMutablePointer<addrinfo>?
    guard getaddrinfo(host, nil, nil, &result) == 0, let result else {
      throw tunnelError(1009, "无法解析 VPN 节点地址。")
    }
    defer { freeaddrinfo(result) }
    var current: UnsafeMutablePointer<addrinfo>? = result
    while let entry = current {
      var buffer = [CChar](repeating: 0, count: Int(NI_MAXHOST))
      let address = entry.pointee
      if (address.ai_family == AF_INET || address.ai_family == AF_INET6),
         getnameinfo(address.ai_addr, address.ai_addrlen, &buffer, socklen_t(buffer.count), nil, 0, NI_NUMERICHOST) == 0 {
        return String(cString: buffer)
      }
      current = address.ai_next
    }
    throw tunnelError(1009, "无法解析 VPN 节点地址。")
  }

  private func networkSettings(address: String) throws -> NEPacketTunnelNetworkSettings {
    let network = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: address)
    let ipv4 = NEIPv4Settings(addresses: [TunnelDNSProbe.sourceAddress], subnetMasks: ["255.255.255.252"])
    ipv4.includedRoutes = [NEIPv4Route.default()]
    var directRoutes = try directIPv4Routes()
    let ipv6 = NEIPv6Settings(addresses: ["fd26:26::1"], networkPrefixLengths: [126])
    ipv6.includedRoutes = [NEIPv6Route.default()]
    if address.contains(":") {
      ipv6.excludedRoutes = [NEIPv6Route(destinationAddress: address, networkPrefixLength: 128)]
    } else {
      directRoutes.append(NEIPv4Route(destinationAddress: address, subnetMask: "255.255.255.255"))
    }
    ipv4.excludedRoutes = directRoutes
    network.ipv4Settings = ipv4
    network.ipv6Settings = ipv6
    let dns = NEDNSSettings(servers: [TunnelDNSProbe.resolverAddress, "1.0.0.1"])
    dns.matchDomains = [""]
    network.dnsSettings = dns
    network.mtu = 1500
    return network
  }

  private func directIPv4Routes() throws -> [NEIPv4Route] {
    guard let url = Bundle.main.url(forResource: "direct-ipv4-cidrs", withExtension: "txt"),
          let bytes = try? Data(contentsOf: url) else {
      throw tunnelError(1013, "无法读取 VPN 扩展中的 IPv4 分流文件。")
    }
    let invalidRoutes = tunnelError(1014, "VPN 的 IPv4 分流文件内容无效。")
    guard let contents = String(data: bytes, encoding: .utf8) else {
      throw invalidRoutes
    }
    let lines = contents.split(whereSeparator: \.isNewline)
    guard !lines.isEmpty else { throw invalidRoutes }

    return try lines.map { line in
      let parts = line.split(separator: "/", omittingEmptySubsequences: false)
      var address = in_addr()
      guard parts.count == 2, let prefix = Int(parts[1]), (1...32).contains(prefix),
            inet_pton(AF_INET, String(parts[0]), &address) == 1 else {
        throw invalidRoutes
      }
      let mask = UInt32.max << (32 - prefix)
      let subnetMask = "\(mask >> 24).\((mask >> 16) & 255).\((mask >> 8) & 255).\(mask & 255)"
      return NEIPv4Route(destinationAddress: String(parts[0]), subnetMask: subnetMask)
    }
  }

  private func checkOutbound(_ config: [String: Any]) throws {
    let response = try invoke("pingBatch", payload: [
      "configs": [["xrayJson": try encode(config), "outboundTag": "26x-proxy"]],
      "timeout": 8,
      "url": "https://cp.cloudflare.com/generate_204",
    ])
    guard let data = response["data"] as? [String: Any],
          let results = data["results"] as? [[String: Any]],
          results.first?["success"] as? Bool == true else {
      throw tunnelError(1010, "VPN 节点未能转发 HTTPS 请求。")
    }
  }

  private func startCore(_ config: [String: Any], generation: Int) throws {
    let bridge = try PacketBridge(flow: packetFlow, queue: queue) { [weak self] in
      guard let self, self.generation == generation else { return }
      let error = self.tunnelError(1006, "VPN 数据通道已中断。")
      if self.startup != nil {
        self.failStart(error)
      } else {
        self.cancelTunnelWithError(error)
      }
    }
    var runtime = config
    // Xray decodes environment values as strings and duplicates this descriptor.
    runtime["env"] = ["xray.tun.fd": String(bridge.engineDescriptor)]
    do {
      _ = try invoke("runXray", payload: ["xrayJson": try encode(runtime)])
    } catch {
      bridge.stop()
      throw error
    }
    coreStarted = true
    self.bridge = bridge
    bridge.start { [weak self] ready in
      guard let self, self.generation == generation else { return }
      if ready {
        self.finishStart(nil)
      } else {
        self.failStart(self.tunnelError(1011, "VPN 数据通道未收到 DNS 应答。"))
      }
    }
  }

  private func invoke(_ method: String, payload: [String: Any] = [:]) throws -> [String: Any] {
    let request = try encode(["apiVersion": 3, "method": method, "payload": payload])
    let response = LibXrayInvoke(request)
    guard let result = (try? JSONSerialization.jsonObject(with: Data(response.utf8))) as? [String: Any],
          result["success"] as? Bool == true else {
      let code = method == "pingBatch" ? 1012 : 1005
      throw tunnelError(code, method == "pingBatch" ? "VPN 连通性检查模块执行失败。" : "VPN 核心启动失败。")
    }
    return result
  }

  private func encode(_ object: [String: Any]) throws -> String {
    String(decoding: try JSONSerialization.data(withJSONObject: object), as: UTF8.self)
  }

  private func failStart(_ error: Error) {
    generation += 1
    stopCore()
    let generation = generation
    setTunnelNetworkSettings(nil) { _ in
      self.queue.async {
        guard self.generation == generation else { return }
        self.finishStart(error)
      }
    }
  }

  private func finishStart(_ error: Error?) {
    let completion = startup
    startup = nil
    completion?(error)
  }

  private func stopCore() {
    if coreStarted {
      _ = try? invoke("stopXray")
      coreStarted = false
    }
    bridge?.stop()
    bridge = nil
  }

  private func tunnelError(_ code: Int, _ message: String) -> NSError {
    NSError(domain: "com.26xvpn.client.tunnel", code: code, userInfo: [NSLocalizedDescriptionKey: message])
  }
}

/// Transfers IP datagrams between Apple's packetFlow and Xray's Darwin TUN
/// adapter. Its four-byte header stores the address family in network order.
private final class PacketBridge {
  private let flow: NEPacketTunnelFlow
  private let queue: DispatchQueue
  private let failure: () -> Void
  private let reader: DispatchSourceRead
  private let flowDescriptor: Int32
  let engineDescriptor: Int32
  private var running = false
  private var closed = false
  private var probe: TunnelDNSProbe?
  private var probeCompletion: ((Bool) -> Void)?
  private var probeGeneration = 0
  private var missedChecks = 0

  init(flow: NEPacketTunnelFlow, queue: DispatchQueue, failure: @escaping () -> Void) throws {
    var sockets: [Int32] = [-1, -1]
    guard socketpair(AF_UNIX, SOCK_DGRAM, 0, &sockets) == 0 else {
      throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO)
    }
    var suppressBrokenPipe = Int32(1)
    for socket in sockets {
      guard fcntl(socket, F_SETFL, O_NONBLOCK) == 0,
            fcntl(socket, F_SETFD, FD_CLOEXEC) == 0,
            setsockopt(socket, SOL_SOCKET, SO_NOSIGPIPE, &suppressBrokenPipe, socklen_t(MemoryLayout<Int32>.size)) == 0 else {
        let code = POSIXErrorCode(rawValue: errno) ?? .EIO
        sockets.forEach { _ = close($0) }
        throw POSIXError(code)
      }
    }
    self.flow = flow
    self.queue = queue
    self.failure = failure
    flowDescriptor = sockets[0]
    engineDescriptor = sockets[1]
    reader = DispatchSource.makeReadSource(fileDescriptor: sockets[0], queue: queue)
    reader.setCancelHandler { [descriptor = sockets[0]] in _ = close(descriptor) }
    reader.setEventHandler { [weak self] in self?.writeToFlow() }
  }

  func start(ready: @escaping (Bool) -> Void) {
    running = true
    reader.activate()
    readFromFlow()
    checkPath { [weak self] passed in
      ready(passed)
      if passed { self?.schedulePathCheck(after: 30) }
    }
  }

  private func checkPath(completion: @escaping (Bool) -> Void) {
    probeGeneration += 1
    let generation = probeGeneration
    let probe = TunnelDNSProbe()
    self.probe = probe
    probeCompletion = completion
    if !sendPacket(probe.packet, family: AF_INET) {
      finishProbe(false)
      return
    }
    queue.asyncAfter(deadline: .now() + 8) { [weak self] in
      guard let self, self.running, self.probeGeneration == generation else { return }
      self.finishProbe(false)
    }
  }

  private func schedulePathCheck(after delta: TimeInterval) {
    queue.asyncAfter(deadline: .now() + delta) { [weak self] in
      guard let self, self.running else { return }
      self.checkPath { [weak self] passed in
        guard let self, self.running else { return }
        self.missedChecks = passed ? 0 : self.missedChecks + 1
        if self.missedChecks >= 3 {
          self.failure()
        } else {
          self.schedulePathCheck(after: passed ? 30 : 5)
        }
      }
    }
  }

  func stop() {
    guard !closed else { return }
    closed = true
    if !running { reader.activate() }
    running = false
    probe = nil
    probeCompletion = nil
    reader.cancel()
    _ = close(engineDescriptor)
  }

  private func readFromFlow() {
    flow.readPackets { [weak self] packets, protocols in
      guard let self else { return }
      self.queue.async {
        guard self.running else { return }
        for (packet, addressFamily) in zip(packets, protocols) {
          let family = addressFamily.int32Value
          guard family == AF_INET || family == AF_INET6, !packet.isEmpty, packet.count <= 1500 else { continue }
          if !self.sendPacket(packet, family: family) &&
             errno != EAGAIN && errno != ENOBUFS && errno != EINTR {
            self.failure()
            return
          }
        }
        self.readFromFlow()
      }
    }
  }

  private func sendPacket(_ packet: Data, family: Int32) -> Bool {
    var header = UInt32(family).bigEndian
    var frame = Data(bytes: &header, count: 4)
    frame.append(packet)
    // A nonblocking socket can drop packets under backpressure.
    return frame.withUnsafeBytes { send(flowDescriptor, $0.baseAddress, $0.count, 0) } >= 0
  }

  private func finishProbe(_ ready: Bool) {
    let completion = probeCompletion
    probe = nil
    probeCompletion = nil
    completion?(ready)
  }

  private func writeToFlow() {
    guard running else { return }
    var frame = [UInt8](repeating: 0, count: 1505)
    var packets: [Data] = []
    var protocols: [NSNumber] = []
    var receivedProbe = false
    for _ in 0..<64 {
      let count = recv(flowDescriptor, &frame, frame.count, 0)
      if count < 0 {
        if errno == EINTR { continue }
        if errno != EAGAIN {
          failure()
          return
        }
        break
      }
      guard count > 4, count <= 1504 else { continue }
      let family = frame.prefix(4).reduce(UInt32(0)) { ($0 << 8) | UInt32($1) }
      guard (family == UInt32(AF_INET) && frame[4] >> 4 == 4) ||
            (family == UInt32(AF_INET6) && frame[4] >> 4 == 6) else { continue }
      if let probe, probe.matches(frame[4..<count]) {
        receivedProbe = true
        continue
      }
      protocols.append(NSNumber(value: family))
      packets.append(Data(frame[4..<count]))
    }
    if !packets.isEmpty, !flow.writePackets(packets, withProtocols: protocols) {
      failure()
      return
    }
    if receivedProbe { finishProbe(true) }
  }
}

/// A DNS round trip through the same packet transport used for client traffic.
/// The response is consumed here because the query did not originate in the operating system.
private struct TunnelDNSProbe {
  static let sourceAddress = "10.26.0.1"
  static let resolverAddress = "1.1.1.1"
  private static let source: [UInt8] = [10, 26, 0, 1]
  private static let resolver: [UInt8] = [1, 1, 1, 1]
  private static let question = [UInt8(7)] + Array("example".utf8) + [3] +
    Array("com".utf8) + [0, 0, 1, 0, 1] // A, IN
  private let transaction = UInt16.random(in: .min ... .max)
  private let sourcePort = UInt16.random(in: 49152 ... .max)

  var packet: Data {
    var dns = Self.bytes(transaction) + [1, 0, 0, 1, 0, 0, 0, 0, 0, 0]
    dns.append(contentsOf: Self.question)
    let udp = Self.bytes(sourcePort) + [0, 53] + Self.bytes(UInt16(8 + dns.count)) + [0, 0]
    var ip: [UInt8] = [0x45, 0] + Self.bytes(UInt16(20 + udp.count + dns.count)) +
      [0, 0, 0, 0, 64, 17, 0, 0] + Self.source + Self.resolver
    var sum: UInt32 = 0
    for index in stride(from: 0, to: 20, by: 2) {
      sum += UInt32(Self.word(ip[index], ip[index + 1]))
    }
    while sum >> 16 != 0 { sum = (sum & 0xffff) + (sum >> 16) }
    ip.replaceSubrange(10..<12, with: Self.bytes(~UInt16(truncatingIfNeeded: sum)))
    return Data(ip + udp + dns)
  }

  func matches(_ packet: ArraySlice<UInt8>) -> Bool {
    let first = packet.startIndex
    guard packet.count >= 20, packet[first] >> 4 == 4, packet[first + 9] == 17,
          packet[(first + 12)..<(first + 16)].elementsEqual(Self.resolver),
          packet[(first + 16)..<(first + 20)].elementsEqual(Self.source) else { return false }
    let headerLength = Int(packet[first] & 0x0f) * 4
    let udp = first + headerLength
    let dns = udp + 8
    guard headerLength >= 20, packet.endIndex >= dns + 12 + Self.question.count,
          Self.word(packet[udp], packet[udp + 1]) == 53,
          Self.word(packet[udp + 2], packet[udp + 3]) == sourcePort,
          Self.word(packet[dns], packet[dns + 1]) == transaction,
          packet[dns + 2] & 0x82 == 0x80, packet[dns + 3] & 0x0f == 0,
          Self.word(packet[dns + 4], packet[dns + 5]) == 1,
          Self.word(packet[dns + 6], packet[dns + 7]) > 0 else { return false }
    return packet[(dns + 12)..<(dns + 12 + Self.question.count)].elementsEqual(Self.question)
  }

  private static func bytes(_ value: UInt16) -> [UInt8] {
    [UInt8(value >> 8), UInt8(truncatingIfNeeded: value)]
  }

  private static func word(_ high: UInt8, _ low: UInt8) -> UInt16 {
    UInt16(high) << 8 | UInt16(low)
  }
}
