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

void main() {
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
