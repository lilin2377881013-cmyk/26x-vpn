import NetworkExtension

/// Observe only this session. Also sample it on registration and at the deadline
/// in case a transition has already occurred before a notification is delivered.
final class TunnelStatusWait {
  private let connection: NEVPNConnection
  private let expected: NEVPNStatus
  private var observation: NSObjectProtocol?
  private var timeoutWork: DispatchWorkItem?
  private var completion: ((Bool) -> Void)?
  private var sawConnecting = false

  private init(_ connection: NEVPNConnection, expected: NEVPNStatus, completion: @escaping (Bool) -> Void) {
    self.connection = connection
    self.expected = expected
    self.completion = completion
  }

  static func until(_ expected: NEVPNStatus, on connection: NEVPNConnection,
                    timeout: TimeInterval, completion: @escaping (Bool) -> Void) {
    let wait = TunnelStatusWait(connection, expected: expected, completion: completion)
    let start = { wait.observe(timeout: timeout) }
    if Thread.isMainThread { start() } else { DispatchQueue.main.async(execute: start) }
  }

  private func observe(timeout: TimeInterval) {
    observation = NotificationCenter.default.addObserver(
      forName: .NEVPNStatusDidChange, object: connection, queue: .main
    ) { [weak self] _ in self?.check() }
    // The deadline keeps this wait alive even when no notification arrives.
    let work = DispatchWorkItem { self.check(expired: true) }
    timeoutWork = work
    DispatchQueue.main.asyncAfter(deadline: .now() + timeout, execute: work)
    check()
  }

  private func check(expired: Bool = false) {
    let status = connection.status
    let stopped = status == .disconnected || status == .invalid
    let reached = status == expected || (expected == .disconnected && stopped)
    sawConnecting = sawConnecting || status == .connecting || status == .reasserting
    guard reached || expired || (expected == .connected && sawConnecting && stopped),
          let complete = completion else { return }
    completion = nil
    if let observation { NotificationCenter.default.removeObserver(observation) }
    observation = nil
    timeoutWork?.cancel()
    timeoutWork = nil
    complete(reached)
  }
}
