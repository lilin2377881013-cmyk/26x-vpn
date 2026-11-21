import 'dart:async';
import 'dart:convert';
import 'dart:io';

/// Owns the libXray worker and its inherited stdin lifetime pipe.
class WindowsTunnel {
  WindowsTunnel({
    required this.onState,
    Future<Process> Function()? startWorker,
  }) : _startWorker = startWorker ?? _startInstalledWorker;

  final Future<Process> Function() _startWorker;
  Future<void>? _writeConfig;

  final void Function(Map<String, String>) onState;
  Future<Process>? _launch;
  Process? _process;
  Future<Map<String, String>>? _stopping;
  Map<String, String> status = const {'state': 'disconnected'};

  static Future<Process> _startInstalledWorker() {
    final runtime = File(Platform.resolvedExecutable).parent;
    final engine = File('${runtime.path}\\26x-tunnel.exe');
    return Process.start(engine.path, const [], workingDirectory: runtime.path);
  }

  Future<Map<String, String>> connect(String config) async {
    if (_launch != null || _process != null || _stopping != null) {
      return const {'code': 'tunnel_busy'};
    }
    final launch = _launch = _startWorker();
    late final Process process;
    try {
      process = await launch;
      _process = process;
    } on ProcessException {
      return const {'code': 'engine_unavailable'};
    } finally {
      _launch = null;
    }

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
              'node_dns_failed',
              'tunnel_busy',
              'tunnel_dns_failed',
              'tunnel_data_failed',
              'probe_execution_failed',
              'tunnel_core_failed',
              'tunnel_start_failed',
              'tunnel_stop_failed',
            }.contains(line)) {
              complete({'code': line});
            }
          },
          onError: (_) => complete(const {'code': 'tunnel_start_failed'}),
          onDone: () async {
            if (started.isCompleted) return;
            final exit = await process.exitCode;
            complete({
              'code': switch (exit) {
                2 => 'worker_exit_2',
                70 => 'worker_cleanup_timeout',
                71 => 'worker_dns_rollback_timeout',
                72 => 'worker_data_rollback_timeout',
                _ => 'tunnel_start_failed',
              },
            });
          },
        );
    unawaited(process.stderr.drain<void>());
    unawaited(
      process.exitCode.then((_) {
        if (_process != process) return;
        _process = null;
        _writeConfig = null;
        if (_stopping == null && status['state'] == 'connected') {
          _publish(const {'state': 'error', 'code': 'tunnel_data_failed'});
        }
      }),
    );

    var failure = 'tunnel_start_failed';
    try {
      if (_stopping != null) {
        await _stopping;
        return const {'code': 'tunnel_start_failed'};
      }
      process.stdin.writeln(config);
      _writeConfig = process.stdin.flush();
      await _writeConfig!.timeout(const Duration(seconds: 3));
      final reply = await started.future.timeout(const Duration(seconds: 42));
      if (reply['state'] == 'connected' &&
          _process == process &&
          _stopping == null) {
        return _publish(reply);
      }
      failure = reply['code'] ?? failure;
    } on TimeoutException {
      failure = 'tunnel_start_timeout';
    } on IOException {
      // The worker has stopped reading its input.
    } on StateError {
      // A concurrent disconnect already closed stdin.
    }
    if (_process == process) {
      final stopped = await disconnect();
      if (stopped['state'] == 'error') return stopped;
    }
    return {'code': failure};
  }

  Future<Map<String, String>> disconnect() =>
      _stopping ??= _stop().whenComplete(() => _stopping = null);

  Future<Map<String, String>> _stop() async {
    final launch = _launch;
    if (launch != null) {
      try {
        await launch;
      } on ProcessException {
        return _publish(const {'state': 'disconnected'});
      }
    }
    final process = _process;
    if (process == null) return _publish(const {'state': 'disconnected'});
    try {
      try {
        // IOSink.close throws while flush is still bound to its stream.
        // Finish the configuration write first, even on immediate disconnect.
        await _writeConfig?.timeout(const Duration(seconds: 2));
        await process.stdin.close().timeout(const Duration(seconds: 2));
      } on IOException {
        // The worker may already be exiting.
      } on StateError {
        // A closed input can no longer deliver EOF. Bound the exit wait below.
      }
      await process.exitCode.timeout(const Duration(seconds: 11));
    } on TimeoutException {
      // Terminate only this app's tracked worker. Wintun and its dynamic
      // filtering session belong to that process.
      process.kill();
      try {
        await process.exitCode.timeout(const Duration(seconds: 3));
      } on TimeoutException {
        return _publish(const {
          'state': 'error',
          'code': 'tunnel_stop_timeout',
        });
      }
    }
    return _publish(const {'state': 'disconnected'});
  }

  Map<String, String> _publish(Map<String, String> snapshot) {
    status = snapshot;
    onState(snapshot);
    return snapshot;
  }
}
