# Windows tunnel adapter

The Dart side expects `MethodChannel("26x/tunnel")` and `EventChannel("26x/tunnel/events")`.
Register them from the generated Windows runner and implement the channel only after
the signed TUN driver/service and protocol engine are available. The current Windows
build intentionally has no channel implementation, so Flutter reports the tunnel as
unavailable instead of claiming it is connected.

Before release, the service must enforce least privilege, authenticate IPC from the
desktop app, install/upgrade/remove the driver safely, and report state from the actual
TUN service process. Do not use a UI-only connected flag.
