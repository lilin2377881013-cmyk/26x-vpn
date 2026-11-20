import 'dart:async';
import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:twentysixx_client/services/api_client.dart';
import 'package:twentysixx_client/services/connection_errors.dart';

void main() {
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
