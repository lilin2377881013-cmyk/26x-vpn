import 'dart:async';
import 'dart:io';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:twentysixx_client/services/api_client.dart';
import 'package:twentysixx_client/services/connection_errors.dart';

void main() {
  test(
    'session cookies survive a timeout and are cleared on a confirmed 401',
    () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      final origin = 'http://127.0.0.1:${server.port}';
      FlutterSecureStorage.setMockInitialValues({
        '$origin/26x_session_cookie': 'saved-session',
        '$origin/26x_csrf_cookie': 'saved-csrf',
      });
      final api = ApiClient(
        baseUrl: origin,
        requestTimeout: const Duration(milliseconds: 300),
      );
      addTearDown(() async {
        api.close();
        await server.close(force: true);
      });
      final paths = <String>[];
      server.listen((request) {
        paths.add(request.uri.path);
        if (paths.length == 1) return;
        final session = request.cookies.any(
          (c) => c.name == '26x_session' && c.value == 'saved-session',
        );
        request.response.statusCode = paths.length == 2 && session ? 200 : 401;
        request.response.write('{"id":"member","email":"member@example.test"}');
        unawaited(request.response.close());
      });
      await expectLater(
        api.restoreSession(),
        throwsA(
          isA<ApiException>().having(
            (e) => e.message,
            'stage',
            contains('API-SESSION-RESTORE'),
          ),
        ),
      );
      expect(await api.restoreSession(), isTrue);
      expect(api.account?.id, 'member');
      expect(await api.restoreSession(), isFalse);
      expect(await api.restoreSession(), isFalse);
      expect(paths, List.filled(3, '/api/v1/me'));
    },
  );

  test(
    'request with no response times out and the next attempt works',
    () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      var calls = 0;
      server.listen((request) {
        calls++;
        // A TCP connection succeeds, but no HTTP response arrives.
        if (calls == 1) {
          return;
        }
        request.response.headers.contentType = ContentType.json;
        request.response.write('{"items":[]}');
        unawaited(request.response.close());
      });
      final api = ApiClient(
        baseUrl: 'http://127.0.0.1:${server.port}',
        requestTimeout: const Duration(milliseconds: 150),
      );
      addTearDown(() async {
        api.close();
        await server.close(force: true);
      });
      final watch = Stopwatch()..start();
      await expectLater(
        api.devices(),
        throwsA(
          isA<ApiException>().having(
            (e) => e.message,
            'stage',
            contains('API-DEVICES'),
          ),
        ),
      );
      expect(watch.elapsed, lessThan(const Duration(seconds: 2)));
      expect(await api.devices(), isEmpty);
    },
  );

  test(
    'DNS, HTTPS probe, DNS tunnel, routes and core errors remain distinct',
    () {
      final codes = [
        'node_dns_failed',
        'node_unreachable',
        'probe_execution_failed',
        'tunnel_dns_failed',
        'tunnel_routes_failed',
        'tunnel_core_failed',
      ];
      final messages = codes.map(tunnelErrorMessage).toSet();
      expect(messages.length, codes.length);
      expect(tunnelErrorMessage('tunnel_dns_failed'), contains('VPN-TUN-DNS'));
      expect(tunnelErrorMessage('node_unreachable'), contains('VPN-HTTPS'));
      expect(
        tunnelErrorMessage('private-key=secret'),
        isNot(contains('secret')),
      );
      expect(
        apiRequestStage('DELETE', '/api/v1/me/devices/id/connection-lease'),
        isNot(apiRequestStage('PUT', '/api/v1/me/devices/id/connection-lease')),
      );
    },
  );
}
