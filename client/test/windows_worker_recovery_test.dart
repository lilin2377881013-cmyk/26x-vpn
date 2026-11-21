import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:twentysixx_client/services/windows_tunnel.dart';

void main() {
  test(
    'Windows supervisor cancels starts and reaps workers that ignore EOF',
    () async {
      final dir = await Directory.systemTemp.createTemp('26x-worker-test');
      addTearDown(() => dir.delete(recursive: true));
      final fixture = File('${dir.path}/worker.dart');
      await fixture.writeAsString(r"""
import 'dart:async';
import 'dart:convert';
import 'dart:io';
Future<void> main() async {
  var hang = false;
  await for (final line in stdin.transform(utf8.decoder).transform(const LineSplitter())) {
    hang = line == 'hang';
    stdout.writeln('connected');
  }
  if (hang) Timer.periodic(const Duration(seconds: 1), (_) {});
}
""");
      final children = <Process>[];
      final sdk = Platform.environment['FLUTTER_ROOT'];
      final dart = sdk == null
          ? 'dart'
          : '$sdk/bin/cache/dart-sdk/bin/${Platform.isWindows ? 'dart.exe' : 'dart'}';
      final tunnel = WindowsTunnel(
        onState: (_) {},
        startWorker: () async {
          final proc = await Process.start(dart, [fixture.path]);
          children.add(proc);
          return proc;
        },
      );
      addTearDown(() async {
        await tunnel.disconnect();
      });
      final launch = tunnel.connect('normal');
      expect((await tunnel.connect('duplicate'))['code'], 'tunnel_busy');
      final cancel = tunnel.disconnect();
      await launch;
      expect((await cancel)['state'], 'disconnected');
      expect(await children.single.exitCode, isA<int>());
      expect((await tunnel.connect('hang'))['state'], 'connected');
      final stopped = await Future.wait([
        tunnel.disconnect(),
        tunnel.disconnect(),
      ]);
      expect(
        stopped.every((result) => result['state'] == 'disconnected'),
        isTrue,
      );
      expect(await children.last.exitCode, isA<int>());
      expect((await tunnel.connect('normal'))['state'], 'connected');
      expect((await tunnel.disconnect())['state'], 'disconnected');
    },
    timeout: const Timeout(Duration(seconds: 45)),
  );
}
