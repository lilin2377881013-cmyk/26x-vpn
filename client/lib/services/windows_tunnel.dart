import 'dart:async';
import 'dart:convert';
import 'dart:io';

/// Owns the libXray worker and its inherited stdin lifetime pipe.
class WindowsTunnel {
  WindowsTunnel({required this.onState});

  final void Function(Map<String, String>) onState;
  Process? _process;
  Future<int>? _exit;
  bool _stopping = false;
  Map<String, String> status = const {'state': 'disconnected'};

  Future<Map<String, String>> connect(String config) async {
    if (_process != null) return const {'code': 'tunnel_busy'};
    final runtime = File(Platform.resolvedExecutable).parent;
    final engine = File('${runtime.path}\\26x-tunnel.exe');
    if (!await engine.exists()) return const {'code': 'engine_unavailable'};

    Process process;
    try {
      process = await Process.start(
        engine.path,
        const [],
        workingDirectory: runtime.path,
      );
    } on ProcessException {
      return const {'code': 'engine_unavailable'};
    }

    _process = process;
    _exit = process.exitCode;
    _stopping = false;
    final started = Completer<Map<String, String>>();
    void complete(Map<String, String> reply) {
      if (!started.isCompleted) started.complete(reply);
    }

    process.stdout
        .transform(utf8.decoder)
        .transform(const LineSplitter())
        .listen(
          (line) {
            if (line == 'connected') {
              complete(const {'state': 'connected'});
            } else if (const {
              'vpn_permission_denied',
              'engine_unavailable',
              'profile_invalid',
              'node_unreachable',
              'probe_execution_failed',
              'tunnel_core_failed',
              'tunnel_start_failed',
            }.contains(line)) {
              complete({'code': line});
            }
          },
          onError: (_) => complete(const {'code': 'tunnel_start_failed'}),
          onDone: () => complete(const {'code': 'tunnel_start_failed'}),
        );
    unawaited(process.stderr.drain<void>());
    unawaited(
      process.exitCode.then((exitCode) {
        if (_process != process) return;
        _process = null;
        _exit = null;
        if (status['state'] == 'connected' || _stopping) {
          _publish({
            'state': _stopping && exitCode == 0 ? 'disconnected' : 'error',
          });
        }
      }),
    );

    process.stdin.writeln(config);
    try {
      await process.stdin.flush();
      final reply = await started.future.timeout(const Duration(seconds: 35));
      if (reply['state'] == 'connected' && _process == process) {
        _publish(reply);
        return reply;
      }
      await disconnect();
      return {'code': reply['code'] ?? 'tunnel_start_failed'};
    } catch (_) {
      await disconnect();
      return const {'code': 'tunnel_start_failed'};
    }
  }

  Future<Map<String, String>> disconnect() async {
    final process = _process;
    final exit = _exit;
    if (process == null || exit == null) {
      return _publish(const {'state': 'disconnected'});
    }
    _stopping = true;
    try {
      await process.stdin.close();
    } on IOException {
      // The worker may have closed its stdin as it exited.
    }
    try {
      final code = await exit.timeout(const Duration(seconds: 12));
      return _publish({'state': code == 0 ? 'disconnected' : 'error'});
    } on TimeoutException {
      return _publish(const {'state': 'error'});
    }
  }

  Map<String, String> _publish(Map<String, String> snapshot) {
    status = snapshot;
    onState(snapshot);
    return snapshot;
  }
}
