import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:twentysixx_client/models.dart';
import 'package:twentysixx_client/screens/home_screen.dart';
import 'package:twentysixx_client/services/api_client.dart';
import 'package:twentysixx_client/services/tunnel_controller.dart';

class _Tunnel extends TunnelController {
  @override
  Future<void> initialize() async {}
}

class _Accounts extends ApiClient {
  _Accounts({this.failRestore = false, this.member = true});
  bool failRestore;
  final bool member;
  int restores = 0;
  int identities = 0;
  int trials = 0;

  @override
  ApiClient createTrialClient() => this;

  @override
  Future<bool> restoreSession() async {
    restores++;
    if (failRestore) throw const ApiException('恢复账号 [API-SESSION-RESTORE]请求超时');
    if (!member) return false;
    account = const Account(
      id: '1',
      email: 'member@example.test',
      status: 'active',
      planTier: 'standard',
    );
    return true;
  }

  @override
  Future<({String code, DateTime? endsAt})> identifyTrial(
    String platform,
  ) async {
    identities++;
    return (code: 'GUEST', endsAt: null);
  }

  @override
  Future<({Account account, Device device})> startTrial(String platform) async {
    trials++;
    throw StateError('unexpected trial');
  }

  @override
  Future<List<VpnPlan>> availablePlans() async => [];
  @override
  Future<List<Device>> devices() async => [
    const Device(
      id: 'pc',
      label: 'Test PC',
      platform: 'windows',
      createdAt: null,
    ),
  ];
  @override
  Future<String?> localDeviceId() async => 'pc';
  @override
  Future<void> rememberLocalDevice(String id) async {}
  @override
  Future<void> syncDeviceSystem(String deviceId) async {}
  @override
  Future<List<VpnNode>> loadNodes({String? deviceId}) async => [];
}

class _LeasedAccount extends _Accounts {
  final calls = <String>[];
  final reservation = Completer<void>();
  final release = Completer<void>();
  bool authorized = false;

  @override
  Future<List<VpnNode>> loadNodes({String? deviceId}) async {
    calls.add('nodes');
    connectionLeases = regionalLeases = true;
    return [
      VpnNode(
        id: 'hk',
        code: 'hk',
        name: 'HK',
        region: 'HK',
        status: 'online',
        endpointId: 'endpoint',
        protocol: 'hysteria2',
        host: 'example.test',
        port: 443,
        transport: 'udp',
        connectable: authorized,
        credentialStatus: authorized ? 'active' : 'pending',
      ),
    ];
  }

  @override
  Future<ConnectionLease> reserveConnection(
    String deviceId, {
    String? token,
    String? region,
  }) async {
    calls.add('reserve $region');
    await reservation.future;
    authorized = true;
    return ConnectionLease(
      deviceId,
      'fixture',
      const Duration(seconds: 90),
      endpointId: 'endpoint',
    );
  }

  @override
  Future<void> releaseConnection(ConnectionLease lease) async {
    calls.add('release');
    await release.future;
    authorized = false;
  }
}

class _TestTunnel extends _Tunnel {
  final events = StreamController<TunnelSnapshot>.broadcast();
  @override
  TunnelSnapshot current = const TunnelSnapshot(TunnelState.disconnected);
  @override
  Stream<TunnelSnapshot> get changes => events.stream;
  final stopped = Completer<void>();
  @override
  Future<void> connect(VpnNode node) async {
    expect(node.endpointId, 'endpoint');
    events.add(current = const TunnelSnapshot(TunnelState.connected));
  }

  @override
  Future<void> disconnect() async {
    events.add(current = const TunnelSnapshot(TunnelState.disconnecting));
    await stopped.future;
    events.add(current = const TunnelSnapshot(TunnelState.disconnected));
  }
}

void main() {
  testWidgets(
    'connect fetches the assigned profile immediately and disconnect waits only for the tunnel',
    (tester) async {
      final api = _LeasedAccount();
      final tunnel = _TestTunnel();
      await tester.pumpWidget(
        MaterialApp(
          home: HomeScreen(api: api, tunnel: tunnel),
        ),
      );
      await tester.pumpAndSettle();
      expect(api.calls, ['nodes']);
      await tester.tap(find.text('连接 VPN'));
      await tester.pump();
      expect(find.text('正在准备连接'), findsWidgets);
      expect(api.calls, ['nodes', 'reserve HK']);
      api.reservation.complete();
      await tester.pump();
      await tester.pump();
      expect(find.text('已连接'), findsOneWidget);
      expect(api.calls, ['nodes', 'reserve HK', 'nodes']);
      await tester.tap(find.text('断开连接'));
      await tester.pump();
      expect(find.text('正在断开'), findsWidgets);
      expect(api.calls.contains('release'), isFalse);
      tunnel.stopped.complete();
      await tester.pump();
      await tester.pump();
      expect(find.text('未连接'), findsOneWidget);
      expect(api.calls.where((c) => c == 'release'), hasLength(1));
      await tester.tap(find.text('连接 VPN'));
      await tester.pump();
      expect(api.calls.last, 'release');
      api.release.complete();
      await tester.pump();
      await tester.pump();
      expect(find.text('已连接'), findsOneWidget);
      expect(api.calls, [
        'nodes',
        'reserve HK',
        'nodes',
        'release',
        'reserve HK',
        'nodes',
      ]);
      await tester.pumpWidget(const SizedBox());
      await tunnel.events.close();
    },
  );

  testWidgets(
    'a returning member can recover after an offline launch without starting a trial',
    (tester) async {
      final api = _Accounts(failRestore: true);
      await tester.pumpWidget(
        MaterialApp(
          home: HomeScreen(api: api, tunnel: _Tunnel()),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.textContaining('API-SESSION-RESTORE'), findsOneWidget);
      await tester.tap(find.text('重试恢复账号'));
      await tester.pumpAndSettle();
      expect(api.restores, 2);
      expect(api.identities, 0);
      expect(api.trials, 0);
      api.failRestore = false;
      await tester.tap(find.byTooltip('刷新'));
      await tester.pumpAndSettle();
      expect(api.restores, 3);
      expect(api.identities, 0);
      expect(find.text('连接 VPN'), findsOneWidget);
      await tester.tap(find.text('我的'));
      await tester.pumpAndSettle();
      expect(find.text('member@example.test'), findsOneWidget);
      expect(find.textContaining('游客识别码'), findsNothing);
    },
  );

  testWidgets(
    'a fresh guest still gets an identity without consuming the trial',
    (tester) async {
      final api = _Accounts(member: false);
      await tester.pumpWidget(
        MaterialApp(
          home: HomeScreen(api: api, tunnel: _Tunnel()),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.text('我的'));
      await tester.pumpAndSettle();
      expect(find.text('游客识别码 GUEST'), findsOneWidget);
      expect(api.identities, 1);
      expect(api.trials, 0);
    },
  );
}
