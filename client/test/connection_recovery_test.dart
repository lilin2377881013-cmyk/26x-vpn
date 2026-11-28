import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:twentysixx_client/services/api_client.dart';
import 'package:twentysixx_client/services/connection_errors.dart';

void main() {
  test(
    'inline profiles need one request and older split responses still work',
    () async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      final api = ApiClient(baseUrl: 'http://127.0.0.1:${server.port}');
      addTearDown(() async {
        api.close();
        await server.close(force: true);
      });
      final paths = <String>[];
      final profile = {
        'protocol': 'hysteria2',
        'host': 'example.test',
        'port': 443,
        'credential': 'fixture',
        'serverName': 'example.test',
      };
      final complete = {
        'endpoint': {'id': 'one'},
        'connectable': true,
        'connectionProfile': profile,
      };
      final pending = {
        'endpoint': {'id': 'two'},
        'connectable': false,
        'connectionProfile': null,
      };
      final split = {
        'endpoint': {'id': 'three'},
        'connectable': true,
      };
      var legacy = false;
      var items = [complete, pending];
      server.listen((request) {
        final path = request.uri.path;
        paths.add(path);
        if (legacy && path == '/api/v1/nodes') {
          request.response.statusCode = 404;
        } else if (path.endsWith('/credentials')) {
          request.response.write(
            jsonEncode({
              'items': [
                {'endpointId': 'three', 'connectionProfile': profile},
              ],
            }),
          );
        } else {
          request.response.write(
            jsonEncode({'connectionLeases': true, 'items': items}),
          );
        }
        unawaited(request.response.close());
      });
      expect(
        (await api.loadNodes(deviceId: 'device')).map((n) => n.connectable),
        [true, false],
      );
      expect(paths, ['/api/v1/nodes']);
      paths.clear();
      items = [complete, pending, split];
      expect(
        (await api.loadNodes(deviceId: 'device')).map((n) => n.connectable),
        [true, false, true],
      );
      expect(paths, ['/api/v1/nodes', '/api/v1/devices/device/credentials']);
      paths.clear();
      legacy = true;
      items = [complete];
      expect(
        (await api.loadNodes(deviceId: 'device')).single.connectable,
        isTrue,
      );
      expect(paths, ['/api/v1/nodes', '/api/v1/client/bootstrap']);
    },
  );

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
        'tunnel_route_file_unreadable',
        'tunnel_route_file_invalid',
        'tunnel_route_apply_failed',
        'tunnel_core_failed',
      ];
      final messages = codes.map(tunnelErrorMessage).toSet();
      expect(messages.length, codes.length);
      expect(tunnelErrorMessage('tunnel_dns_failed'), contains('VPN-TUN-DNS'));
      expect(tunnelErrorMessage('node_unreachable'), contains('VPN-HTTPS'));
      expect(
        tunnelErrorMessage('tunnel_route_apply_failed', cause: -7),
        contains('系统错误 -7'),
      );
      expect(
        tunnelErrorMessage('tunnel_route_apply_failed', cause: 'secret'),
        isNot(contains('secret')),
      );
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
