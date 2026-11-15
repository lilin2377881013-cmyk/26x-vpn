import 'dart:async';
import 'dart:io' show Platform;

import 'package:material_ui/material_ui.dart';
import 'package:flutter/services.dart' show Clipboard, ClipboardData;
import 'package:qr_flutter/qr_flutter.dart';
import 'package:url_launcher/url_launcher.dart';

import '../models.dart';
import '../services/api_client.dart';
import '../services/tunnel_controller.dart';

const _nodeRegions = {'HK': '香港', 'JP': '日本', 'SG': '新加坡', 'US': '美国'};

class HomeScreen extends StatefulWidget {
  const HomeScreen({required this.api, required this.tunnel, super.key});

  final ApiClient api;
  final TunnelController tunnel;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  final _email = TextEditingController();
  final _password = TextEditingController();
  StreamSubscription<TunnelSnapshot>? _tunnelSubscription;
  ConnectionLease? _lease;
  Timer? _renewal;
  Timer? _leaseExpiry;
  Future<bool>? _releaseInFlight;
  bool _leaseInvalid = false;
  Account? _account;
  List<Device> _devices = [];
  List<VpnNode> _nodes = [];
  List<VpnPlan> _plans = [];
  Device? _device;
  VpnNode? _selectedNode;
  TunnelSnapshot _tunnelState = const TunnelSnapshot(TunnelState.disconnected);
  bool _busy = false;
  int _tab = 0;
  String _planTier = 'standard';
  final _selectedPlanCodes = <String, String>{};
  static const _pageColor = Color(0xFF191D27);
  static const _panelColor = Color(0xFF242938);
  static const _gold = Color(0xFFF0CA87);
  static const _muted = Color(0xFFAFB9C9);
  static const _flags = {
    'HK': '🇭🇰',
    'JP': '🇯🇵',
    'SG': '🇸🇬',
    'US': '🇺🇸',
  };
  String? _message;
  String? _planError;

  @override
  void initState() {
    super.initState();
    _tunnelState = widget.tunnel.current;
    _tunnelSubscription = widget.tunnel.changes.listen((value) {
      if (mounted) setState(() => _tunnelState = value);
      if (value.state == TunnelState.disconnected) {
        unawaited(_releaseLease());
      }
    });
    unawaited(widget.tunnel.initialize());
    _busy = true;
    unawaited(_restoreSession());
  }

  Future<void> _restoreSession() async {
    try {
      if (await widget.api.restoreSession()) {
        _account = widget.api.account;
        await _loadDevices();
        await _loadPlans();
      }
    } catch (error) {
      _message = error.toString();
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  void dispose() {
    _renewal?.cancel();
    _leaseExpiry?.cancel();
    unawaited(_tunnelSubscription?.cancel() ?? Future<void>.value());
    _email.dispose();
    _password.dispose();
    super.dispose();
  }

  bool _registering = false;

  Future<void> _signIn() async {
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      _account = _registering
          ? await widget.api.register(_email.text, _password.text)
          : await widget.api.signIn(_email.text, _password.text);
      await _loadDevices();
      unawaited(_loadPlans());
      if (mounted) {
        setState(() {
          _password.clear();
          _registering = false;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _message = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _startTrial() async {
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      _account = await widget.api.startTrial(_platform);
      await _loadDevices();
      unawaited(_loadPlans());
    } catch (error) {
      if (mounted) setState(() => _message = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _checkout(VpnPlan plan) async {
    if (_account?.trialCode != null) {
      setState(() => _message = '试用用户请在个人页面退出试用，再注册或登录后购买。');
      return;
    }
    if (Platform.isIOS) {
      setState(() => _message = '此 iOS 版本暂未开放购买。');
      return;
    }
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      final payUrl = await widget.api.checkout(plan.code);
      if (!mounted) return;
      if (Platform.isWindows || Platform.isMacOS) {
        await showDialog<void>(
          context: context,
          builder: (dialogContext) => AlertDialog(
            title: const Text('支付宝扫码支付'),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                ColoredBox(
                  color: Colors.white,
                  child: QrImageView(data: payUrl.toString(), size: 208),
                ),
                const SizedBox(height: 12),
                const Text('用手机支付宝扫一扫，完成后点击刷新会员状态。'),
              ],
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(dialogContext).pop(),
                child: const Text('关闭'),
              ),
            ],
          ),
        );
      } else if (!await launchUrl(
        payUrl,
        mode: LaunchMode.externalApplication,
      )) {
        throw const ApiException('无法打开支付页面，请稍后重试。');
      }
      if (mounted) {
        setState(() => _message = '支付完成后请返回 26x，点击刷新会员状态。');
      }
    } catch (error) {
      if (mounted) setState(() => _message = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _loadDevices() async {
    _devices = await widget.api.devices();
    final localId = await widget.api.localDeviceId();
    _device = null;
    for (final device in _devices) {
      if (device.id == localId) {
        _device = device;
        break;
      }
    }
    final device = _device;
    if (device != null) unawaited(widget.api.syncDeviceSystem(device.id));
    final trialExpired =
        _account?.trialCode != null &&
        _account?.subscriptionEndsAt?.isAfter(DateTime.now()) != true;
    if (_device != null && !trialExpired) await _loadNodes();
    if (trialExpired) _message = '15 分钟试用已结束。';
    if (_device == null || trialExpired) {
      _nodes = [];
      _selectedNode = null;
    }
    if (mounted) setState(() {});
  }

  Future<void> _loadNodes() async {
    final previous = _selectedNode;
    _nodes = await widget.api.loadNodes(deviceId: _device?.id);
    _selectedNode =
        (previous == null
            ? null
            : _nodes
                      .where(
                        (node) =>
                            _regionCode(node) == _regionCode(previous) &&
                            node.connectable,
                      )
                      .firstOrNull ??
                  _nodes
                      .where(
                        (node) => _regionCode(node) == _regionCode(previous),
                      )
                      .firstOrNull) ??
        _nodes.where((node) => node.connectable).firstOrNull ??
        _nodes.firstOrNull;
    if (mounted) setState(() {});
  }

  Future<void> _registerDevice() async {
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      final device = await widget.api.registerDevice(
        platform: _platform,
        label: _deviceLabel,
      );
      _devices = [..._devices, device];
      _device = device;
      await _loadNodes();
    } catch (error) {
      if (mounted) setState(() => _message = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _signOut() async {
    if (_tunnelState.state == TunnelState.connected) {
      await widget.tunnel.disconnect();
    }
    await (_releaseInFlight ?? Future<void>.value());
    String? signOutError;
    try {
      await widget.api.signOut();
    } catch (error) {
      signOutError = error.toString();
    } finally {
      if (mounted) {
        setState(() {
          _tab = 0;
          _account = null;
          _devices = [];
          _nodes = [];
          _plans = [];
          _device = null;
          _selectedNode = null;
          _message = signOutError;
          _planError = null;
        });
      }
    }
  }

  Future<void> _revokeDevice(Device device) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('撤销设备？'),
        content: Text('“${device.label}”将无法继续获取 VPN 节点配置。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('撤销设备'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      if (device.id == _device?.id && _isConnected) {
        await widget.tunnel.disconnect();
      }
      await widget.api.revokeDevice(device.id);
      await _loadDevices();
    } catch (error) {
      if (mounted) setState(() => _message = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _changePassword() async {
    final currentPassword = TextEditingController();
    final newPassword = TextEditingController();
    final confirmPassword = TextEditingController();
    var dialogError = '';
    var submitting = false;
    await showDialog<void>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (dialogContext, setDialogState) => AlertDialog(
          title: const Text('修改密码'),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                TextField(
                  controller: currentPassword,
                  obscureText: true,
                  decoration: const InputDecoration(labelText: '当前密码'),
                ),
                TextField(
                  controller: newPassword,
                  obscureText: true,
                  decoration: const InputDecoration(labelText: '新密码（至少 16 位）'),
                ),
                TextField(
                  controller: confirmPassword,
                  obscureText: true,
                  decoration: const InputDecoration(labelText: '确认新密码'),
                ),
                if (dialogError.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 12),
                    child: Text(
                      dialogError,
                      style: const TextStyle(
                        color: Color(0xFFB84B45),
                        fontSize: 12,
                      ),
                    ),
                  ),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: submitting ? null : () => Navigator.pop(dialogContext),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: submitting
                  ? null
                  : () async {
                      if (newPassword.text.length < 16) {
                        setDialogState(() => dialogError = '新密码至少需要 16 个字符。');
                        return;
                      }
                      if (newPassword.text != confirmPassword.text) {
                        setDialogState(() => dialogError = '两次输入的新密码不一致。');
                        return;
                      }
                      setDialogState(() {
                        submitting = true;
                        dialogError = '';
                      });
                      try {
                        await widget.api.changePassword(
                          currentPassword: currentPassword.text,
                          newPassword: newPassword.text,
                        );
                        if (dialogContext.mounted) Navigator.pop(dialogContext);
                        if (mounted) {
                          setState(() {
                            _tab = 0;
                            _account = null;
                            _devices = [];
                            _nodes = [];
                            _device = null;
                            _selectedNode = null;
                            _message = '密码已更新，请使用新密码重新登录。';
                          });
                        }
                      } catch (error) {
                        if (dialogContext.mounted) {
                          setDialogState(() => dialogError = error.toString());
                        }
                      } finally {
                        if (dialogContext.mounted) {
                          setDialogState(() => submitting = false);
                        }
                      }
                    },
              child: submitting
                  ? const SizedBox(
                      width: 17,
                      height: 17,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Text('更新密码'),
            ),
          ],
        ),
      ),
    );
    currentPassword.dispose();
    newPassword.dispose();
    confirmPassword.dispose();
  }

  String get _platform {
    if (Platform.isIOS) return 'ios';
    if (Platform.isMacOS) return 'macos';
    if (Platform.isWindows) return 'windows';
    if (Platform.isAndroid) return 'android';
    return 'ios';
  }

  String get _deviceLabel {
    if (Platform.isIOS) return 'iPhone / iPad';
    if (Platform.isMacOS) return 'Mac';
    if (Platform.isWindows) return 'Windows PC';
    if (Platform.isAndroid) return 'Android';
    return '26x device';
  }

  bool get _isConnecting =>
      _tunnelState.state == TunnelState.connecting ||
      _tunnelState.state == TunnelState.disconnecting;
  bool get _isConnected => _tunnelState.state == TunnelState.connected;

  @override
  Widget build(BuildContext context) {
    if (_account != null) return _signedInShell();
    return Scaffold(
      body: DecoratedBox(
        decoration: const BoxDecoration(
          gradient: LinearGradient(
            begin: Alignment.topLeft,
            end: Alignment.bottomRight,
            colors: [Color(0xFFF9FBFB), Color(0xFFE9F0EF)],
          ),
        ),
        child: SafeArea(
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 560),
              child: Padding(
                padding: const EdgeInsets.fromLTRB(22, 18, 22, 28),
                child: Column(
                  children: [
                    _topBar(),
                    const SizedBox(height: 24),
                    Expanded(
                      child: _busy
                          ? const Center(child: CircularProgressIndicator())
                          : _loginCard(),
                    ),
                    const SizedBox(height: 16),
                    const Text(
                      '26x · 私密连接',
                      style: TextStyle(
                        color: Color(0xFF718188),
                        fontSize: 12,
                        letterSpacing: 1.2,
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _topBar() => Row(
    children: [
      Container(
        width: 42,
        height: 42,
        decoration: BoxDecoration(
          color: const Color(0xFF087E79),
          borderRadius: BorderRadius.circular(14),
        ),
        child: const Icon(Icons.shield_outlined, color: Colors.white),
      ),
      const SizedBox(width: 12),
      const Text(
        '26x',
        style: TextStyle(
          fontSize: 24,
          fontWeight: FontWeight.w800,
          letterSpacing: -1,
        ),
      ),
      const Spacer(),
      IconButton(
        onPressed: _busy ? null : _refresh,
        tooltip: '刷新状态',
        icon: const Icon(Icons.refresh_rounded, color: Color(0xFF52646B)),
      ),
    ],
  );

  Widget _loginCard() => SingleChildScrollView(
    child: _surface(
      padding: const EdgeInsets.all(26),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Icon(
            Icons.lock_outline_rounded,
            size: 30,
            color: Color(0xFF087E79),
          ),
          const SizedBox(height: 18),
          const Text(
            '欢迎使用 26x',
            style: TextStyle(
              fontSize: 26,
              fontWeight: FontWeight.w700,
              letterSpacing: -0.6,
            ),
          ),
          const SizedBox(height: 8),
          Text(
            _registering ? '填写邮箱并设置至少 12 位的密码。' : '登录、注册或开始免费试用。',
            style: const TextStyle(color: Color(0xFF66777D), height: 1.5),
          ),
          const SizedBox(height: 26),
          TextField(
            controller: _email,
            keyboardType: TextInputType.emailAddress,
            autofillHints: const [AutofillHints.username],
            decoration: _inputDecoration('邮箱地址', Icons.alternate_email_rounded),
          ),
          const SizedBox(height: 14),
          TextField(
            controller: _password,
            obscureText: true,
            autofillHints: [
              _registering ? AutofillHints.newPassword : AutofillHints.password,
            ],
            onSubmitted: (_) {
              if (!_busy) unawaited(_signIn());
            },
            decoration: _inputDecoration('密码', Icons.key_rounded),
          ),
          if (_message != null) ...[
            const SizedBox(height: 14),
            _messageBox(_message!),
          ],
          const SizedBox(height: 20),
          SizedBox(
            width: double.infinity,
            height: 52,
            child: FilledButton(
              onPressed: _busy ? null : _signIn,
              style: FilledButton.styleFrom(
                backgroundColor: const Color(0xFF087E79),
                shape: RoundedRectangleBorder(
                  borderRadius: BorderRadius.circular(15),
                ),
              ),
              child: _busy
                  ? const SizedBox(
                      width: 20,
                      height: 20,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        color: Colors.white,
                      ),
                    )
                  : Text(
                      _registering ? '注册账户' : '登录账户',
                      style: const TextStyle(
                        fontSize: 15,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
            ),
          ),
          Center(
            child: TextButton(
              onPressed: _busy
                  ? null
                  : () => setState(() {
                      _registering = !_registering;
                      _password.clear();
                      _message = null;
                    }),
              child: Text(_registering ? '已有账号，返回登录' : '注册新账号'),
            ),
          ),
          if (!_registering)
            Center(
              child: TextButton(
                onPressed: _busy ? null : _startTrial,
                child: const Text('新用户免费试用 15 分钟'),
              ),
            ),
        ],
      ),
    ),
  );

  Widget _signedInShell() {
    const background = Color(0xFF151922);
    const accent = Color(0xFF71DBC3);
    final active = _tab == 0;
    return Scaffold(
      backgroundColor: _pageColor,
      appBar: AppBar(
        backgroundColor: background,
        foregroundColor: Colors.white,
        centerTitle: true,
        title: Text(const ['26x', '会员套餐', '个人中心'][_tab]),
        actions: [
          IconButton(
            tooltip: '刷新',
            onPressed: _busy || _isConnecting ? null : _refresh,
            icon: const Icon(Icons.refresh_rounded),
          ),
          const SizedBox(width: 8),
        ],
      ),
      body: SafeArea(
        bottom: false,
        child: active ? _connectionCard() : _detailsPage(),
      ),
      bottomNavigationBar: NavigationBar(
        height: 68,
        backgroundColor: const Color(0xFF202631),
        indicatorColor: const Color(0xFF334A49),
        selectedIndex: _tab,
        labelTextStyle: WidgetStateProperty.resolveWith(
          (states) => TextStyle(
            color: states.contains(WidgetState.selected)
                ? accent
                : const Color(0xFFC1C9D3),
            fontWeight: FontWeight.w600,
          ),
        ),
        onDestinationSelected: (index) => setState(() => _tab = index),
        destinations: const [
          NavigationDestination(
            icon: Icon(Icons.bolt_outlined, color: Color(0xFFC1C9D3)),
            selectedIcon: Icon(Icons.bolt_rounded, color: accent),
            label: '连接',
          ),
          NavigationDestination(
            icon: Icon(
              Icons.workspace_premium_outlined,
              color: Color(0xFFC1C9D3),
            ),
            selectedIcon: Icon(Icons.workspace_premium_rounded, color: accent),
            label: '套餐',
          ),
          NavigationDestination(
            icon: Icon(Icons.person_outline_rounded, color: Color(0xFFC1C9D3)),
            selectedIcon: Icon(Icons.person_rounded, color: accent),
            label: '我的',
          ),
        ],
      ),
    );
  }

  Widget _detailsPage() => Theme(
    data: ThemeData.dark(useMaterial3: true),
    child: Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 780),
        child: ListView(
          padding: const EdgeInsets.fromLTRB(24, 24, 24, 32),
          children: [
            if (_tab == 1) _plansPage() else _accountCard(),
            if (_message != null) ...[
              const SizedBox(height: 12),
              _messageBox(_message!),
            ],
          ],
        ),
      ),
    ),
  );

  Widget _plansPage() {
    final premium = _planTier == 'premium';
    final plans = _plans.where((plan) => plan.tier == _planTier).toList()
      ..sort((a, b) => a.durationDays.compareTo(b.durationDays));
    final selectedPlan =
        plans
            .where((plan) => plan.code == _selectedPlanCodes[_planTier])
            .firstOrNull ??
        plans.where((plan) => plan.durationDays == 90).firstOrNull ??
        plans.firstOrNull;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Center(
          child: SegmentedButton<String>(
            segments: const [
              ButtonSegment(value: 'standard', label: Text('普通档')),
              ButtonSegment(value: 'premium', label: Text('豪华档')),
            ],
            selected: {_planTier},
            onSelectionChanged: (selection) =>
                setState(() => _planTier = selection.single),
            showSelectedIcon: false,
            style: ButtonStyle(
              foregroundColor: WidgetStateProperty.resolveWith(
                (states) => states.contains(WidgetState.selected)
                    ? _gold
                    : Colors.white,
              ),
              backgroundColor: WidgetStateProperty.resolveWith(
                (states) => states.contains(WidgetState.selected)
                    ? _panelColor
                    : _pageColor,
              ),
            ),
          ),
        ),
        const SizedBox(height: 32),
        const Text(
          '套餐权益',
          style: TextStyle(
            color: Colors.white,
            fontSize: 19,
            fontWeight: FontWeight.w700,
          ),
        ),
        const SizedBox(height: 14),
        LayoutBuilder(
          builder: (context, bounds) {
            final columns = bounds.maxWidth < 360 ? 2 : 4;
            final width = (bounds.maxWidth - 12 * (columns - 1)) / columns;
            return Wrap(
              spacing: 12,
              runSpacing: 20,
              children: [
                SizedBox(
                  width: width,
                  child: _benefit('全平台支持', icon: Icons.devices_outlined),
                ),
                SizedBox(
                  width: width,
                  child: _benefit('同时在线数', value: premium ? '2 台' : '1 台'),
                ),
                SizedBox(
                  width: width,
                  child: _benefit(
                    premium ? '极速音频图片' : '流畅音频图片',
                    icon: premium
                        ? Icons.bolt_rounded
                        : Icons.graphic_eq_rounded,
                  ),
                ),
                SizedBox(
                  width: width,
                  child: _benefit('高清视频', value: premium ? '4K' : '720P'),
                ),
              ],
            );
          },
        ),
        const SizedBox(height: 14),
        Text(
          '可登录 ${premium ? 6 : 3} 台设备',
          style: const TextStyle(color: _muted),
        ),
        const SizedBox(height: 32),
        const Text(
          '套餐期限',
          style: TextStyle(
            color: Colors.white,
            fontSize: 19,
            fontWeight: FontWeight.w700,
          ),
        ),
        const SizedBox(height: 14),
        if (_planError != null)
          _messageBox('套餐加载失败，请刷新重试。')
        else if (plans.isEmpty)
          const Text('此档位暂无可用套餐。', style: TextStyle(color: _muted))
        else
          LayoutBuilder(
            builder: (context, bounds) {
              final columns = bounds.maxWidth >= 300 ? 3 : 1;
              final width = (bounds.maxWidth - 20 * (columns - 1)) / columns;
              return Wrap(
                spacing: 20,
                runSpacing: 12,
                children: [
                  for (final plan in plans)
                    SizedBox(
                      width: width,
                      child: _planOption(
                        plan,
                        selected: plan.code == selectedPlan?.code,
                      ),
                    ),
                ],
              );
            },
          ),
        if (selectedPlan != null && _planError == null) ...[
          const SizedBox(height: 32),
          SizedBox(
            width: double.infinity,
            child: FilledButton(
              onPressed: _busy ? null : () => _checkout(selectedPlan),
              style: FilledButton.styleFrom(
                backgroundColor: _gold,
                foregroundColor: const Color(0xFF292115),
                minimumSize: const Size.fromHeight(54),
                shape: RoundedRectangleBorder(
                  borderRadius: BorderRadius.circular(14),
                ),
              ),
              child: Text(_busy ? '请稍候…' : '开通会员'),
            ),
          ),
          if (!Platform.isIOS && _account?.trialCode == null)
            TextButton(
              onPressed: _busy ? null : _refresh,
              child: const Text('刷新会员状态'),
            ),
        ],
        const SizedBox(height: 16),
      ],
    );
  }

  Widget _planOption(VpnPlan plan, {required bool selected}) => Semantics(
    selected: selected,
    button: true,
    child: Material(
      color: _panelColor,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(16),
        side: BorderSide(
          color: selected ? _gold : Colors.transparent,
          width: 1.5,
        ),
      ),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: _busy
            ? null
            : () => setState(() => _selectedPlanCodes[plan.tier] = plan.code),
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 24, horizontal: 8),
          child: Column(
            children: [
              Text(
                '${plan.durationDays} 天',
                style: const TextStyle(color: _muted),
              ),
              const SizedBox(height: 12),
              FittedBox(
                child: Text(
                  _priceLabel(plan),
                  style: const TextStyle(
                    color: _gold,
                    fontSize: 30,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
              const SizedBox(height: 10),
              Text(
                _quotaLabel(plan),
                style: const TextStyle(color: _muted, fontSize: 12),
              ),
            ],
          ),
        ),
      ),
    ),
  );

  Widget _benefit(String label, {IconData? icon, String? value}) => Column(
    children: [
      Container(
        width: 72,
        height: 42,
        alignment: Alignment.center,
        decoration: BoxDecoration(
          gradient: const LinearGradient(
            colors: [Color(0xFFFFE5BB), Color(0xFFE2B276)],
          ),
          borderRadius: BorderRadius.circular(9),
        ),
        child: icon != null
            ? Icon(icon, size: 27, color: const Color(0xFF24212A))
            : Text(
                value!,
                style: const TextStyle(
                  color: Color(0xFF24212A),
                  fontSize: 21,
                  fontWeight: FontWeight.w800,
                ),
              ),
      ),
      const SizedBox(height: 10),
      Text(
        label,
        textAlign: TextAlign.center,
        style: const TextStyle(color: _muted),
      ),
    ],
  );

  Widget _accountCard() {
    final account = _account!;
    final endsAt = account.subscriptionEndsAt;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          account.trialCode == null
              ? account.email
              : '试用识别码 ${account.trialCode}',
          style: const TextStyle(
            color: Colors.white,
            fontSize: 18,
            fontWeight: FontWeight.w600,
          ),
        ),
        const SizedBox(height: 20),
        Container(
          padding: const EdgeInsets.all(22),
          decoration: BoxDecoration(
            gradient: const LinearGradient(
              colors: [Color(0xFFFFDEA3), Color(0xFFCEA460)],
            ),
            borderRadius: BorderRadius.circular(18),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                account.planName ?? '暂未开通套餐',
                style: const TextStyle(
                  color: Color(0xFF292115),
                  fontSize: 21,
                  fontWeight: FontWeight.w800,
                ),
              ),
              if (endsAt != null) ...[
                const SizedBox(height: 8),
                Text(
                  account.trialCode == null
                      ? '有效期至 ${_date(endsAt)}'
                      : '试用截止 ${MaterialLocalizations.of(context).formatTimeOfDay(TimeOfDay.fromDateTime(endsAt.toLocal()))}',
                  style: const TextStyle(color: Color(0xFF4C3920)),
                ),
              ],
              TextButton(
                style: TextButton.styleFrom(
                  foregroundColor: const Color(0xFF292115),
                  padding: EdgeInsets.zero,
                ),
                onPressed: () => setState(() => _tab = 1),
                child: const Text('查看套餐'),
              ),
            ],
          ),
        ),
        const SizedBox(height: 20),
        ListTile(
          leading: const Icon(Icons.lock_outline),
          title: const Text('修改密码'),
          trailing: const Icon(Icons.chevron_right),
          onTap: _busy || account.trialCode != null ? null : _changePassword,
        ),
        const Divider(height: 1),
        ExpansionTile(
          leading: const Icon(Icons.devices_outlined),
          title: const Text('设备管理'),
          subtitle: Text(_device == null ? '本机未登记' : '${_devices.length} 台已登记'),
          children: [
            if (_device == null)
              ListTile(
                title: const Text('注册本机设备'),
                onTap: _busy ? null : _registerDevice,
              ),
            for (final device in _devices)
              ListTile(
                leading: Icon(_platformIcon(device.platform)),
                title: Text(
                  '${device.label}${device.id == _device?.id ? ' · 本机' : ''}',
                ),
                trailing: TextButton(
                  onPressed: _busy ? null : () => _revokeDevice(device),
                  child: const Text('撤销'),
                ),
              ),
          ],
        ),
        const Divider(height: 1),
        ListTile(
          leading: const Icon(Icons.qr_code_rounded),
          title: const Text('防伪防丢失'),
          trailing: const Icon(Icons.chevron_right),
          onTap: _showOfficialLink,
        ),
        const Divider(height: 1),
        ListTile(
          leading: const Icon(Icons.chat_bubble_outline),
          title: const Text('联系我们'),
          trailing: const Icon(Icons.chevron_right),
          onTap: _showSupport,
        ),
        const Divider(height: 1),
        ListTile(
          leading: const Icon(Icons.logout_rounded),
          title: const Text('退出登录'),
          onTap: _busy ? null : _signOut,
        ),
      ],
    );
  }

  Future<void> _showOfficialLink() async {
    const website = 'https://26xvpn.com';
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (context) => Scaffold(
          backgroundColor: _pageColor,
          appBar: AppBar(
            title: const Text('防伪防丢失'),
            foregroundColor: Colors.white,
            backgroundColor: _pageColor,
          ),
          body: SafeArea(
            child: Center(
              child: SingleChildScrollView(
                padding: const EdgeInsets.all(24),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Text(
                      '保存 26x 官网二维码',
                      style: TextStyle(
                        color: _gold,
                        fontSize: 23,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 24),
                    QrImageView(
                      data: website,
                      size: 216,
                      backgroundColor: Colors.white,
                      padding: const EdgeInsets.all(22),
                      semanticsLabel: '26x 官网二维码',
                    ),
                    const SizedBox(height: 16),
                    TextButton(
                      onPressed: () async {
                        try {
                          if (await launchUrl(
                            Uri.parse(website),
                            mode: LaunchMode.externalApplication,
                          )) {
                            return;
                          }
                        } catch (_) {
                          // The copy action is available when the browser cannot open.
                        }
                        if (context.mounted) {
                          ScaffoldMessenger.of(context).showSnackBar(
                            const SnackBar(content: Text('无法打开浏览器，请复制官网链接。')),
                          );
                        }
                      },
                      child: const Text(
                        website,
                        style: TextStyle(color: _gold),
                      ),
                    ),
                    OutlinedButton.icon(
                      onPressed: () async {
                        await Clipboard.setData(
                          const ClipboardData(text: website),
                        );
                        if (context.mounted) {
                          ScaffoldMessenger.of(context).showSnackBar(
                            const SnackBar(content: Text('已复制官网链接')),
                          );
                        }
                      },
                      icon: const Icon(Icons.copy_outlined, color: _gold),
                      label: const Text(
                        '复制官网链接',
                        style: TextStyle(color: _gold),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _showSupport() => Navigator.of(context).push<void>(
    MaterialPageRoute(builder: (_) => _SupportPage(api: widget.api)),
  );

  Future<void> _loadPlans() async {
    try {
      final plans = await widget.api.availablePlans();
      if (!mounted) return;
      setState(() {
        _plans = plans;
        _planError = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _planError = error.toString());
    }
  }

  Widget _connectionCard() {
    const muted = Color(0xFFAAB6C6);
    final color = _isConnected
        ? const Color(0xFF71DBC3)
        : const Color(0xFFE2EAF4);
    final node = _selectedNode;
    final region = node == null ? null : _regionCode(node);
    final label = node == null ? '选择地区' : _nodeRegions[region] ?? node.name;
    final isTrial = _account?.trialCode != null;
    final status = switch (_tunnelState.state) {
      TunnelState.connected => '已连接',
      TunnelState.connecting => '正在连接',
      TunnelState.disconnecting => '正在断开',
      TunnelState.error => '连接异常',
      TunnelState.unavailable => '尚未就绪',
      TunnelState.disconnected => '未连接',
    };
    return DecoratedBox(
      decoration: const BoxDecoration(
        gradient: RadialGradient(
          center: Alignment(0, -.6),
          radius: 1.15,
          colors: [Color(0xFF273542), Color(0xFF151922)],
        ),
      ),
      child: LayoutBuilder(
        builder: (context, bounds) => SingleChildScrollView(
          padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 28),
          child: ConstrainedBox(
            constraints: BoxConstraints(
              minHeight: (bounds.maxHeight - 56)
                  .clamp(0, double.infinity)
                  .toDouble(),
            ),
            child: Center(
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 420),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(
                      status,
                      style: TextStyle(
                        color: _isConnected ? color : muted,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    const SizedBox(height: 24),
                    OutlinedButton(
                      onPressed: _busy || _isConnecting || _device == null
                          ? null
                          : _toggleTunnel,
                      style: OutlinedButton.styleFrom(
                        fixedSize: Size.square(
                          bounds.maxHeight < 460 ? 128 : 164,
                        ),
                        shape: const CircleBorder(),
                        foregroundColor: color,
                        disabledForegroundColor: muted,
                        backgroundColor: _isConnected
                            ? const Color(0xFF203C3D)
                            : const Color(0xFF202A36),
                        side: BorderSide(
                          color: _isConnected ? color : const Color(0xFF445365),
                          width: 2,
                        ),
                      ),
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          if (_isConnecting)
                            SizedBox.square(
                              dimension: 32,
                              child: CircularProgressIndicator(
                                color: color,
                                strokeWidth: 2,
                              ),
                            )
                          else
                            const Icon(
                              Icons.power_settings_new_rounded,
                              size: 38,
                            ),
                          const SizedBox(height: 10),
                          Text(
                            _isConnecting
                                ? status
                                : _isConnected
                                ? '断开连接'
                                : '连接 VPN',
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(height: 36),
                    if (_device == null)
                      FilledButton.tonalIcon(
                        onPressed: _busy ? null : _registerDevice,
                        icon: const Icon(Icons.add_rounded),
                        label: const Text('注册本机设备'),
                      )
                    else if (isTrial)
                      const Text('15 分钟试用', style: TextStyle(color: muted))
                    else
                      OutlinedButton(
                        onPressed: _busy || _isConnecting ? null : _showRegions,
                        style: OutlinedButton.styleFrom(
                          foregroundColor: Colors.white,
                          backgroundColor: const Color(0xFF252F3D),
                          side: const BorderSide(color: Color(0xFF425065)),
                          minimumSize: const Size(224, 52),
                        ),
                        child: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            if (_flags[region] case final String flag)
                              Text(flag, style: const TextStyle(fontSize: 20))
                            else
                              const Icon(Icons.public_rounded, size: 20),
                            const SizedBox(width: 12),
                            Text(label),
                            const SizedBox(width: 24),
                            const Icon(Icons.chevron_right_rounded, size: 20),
                          ],
                        ),
                      ),
                    if ((_message ?? _tunnelState.message)
                        case final String message) ...[
                      const SizedBox(height: 16),
                      _messageBox(message),
                    ],
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _showRegions() async {
    final node = await Navigator.of(context).push<VpnNode>(
      MaterialPageRoute(
        builder: (routeContext) {
          final codes = _nodes.map(_regionCode).toSet();
          final regions = {
            ..._nodeRegions,
            for (final code in codes.where(
              (code) => !_nodeRegions.containsKey(code),
            ))
              code: code,
          };
          final premium = _account?.planTier == 'premium';
          return Theme(
            data: ThemeData.dark(useMaterial3: true),
            child: DefaultTabController(
              length: 2,
              initialIndex: premium ? 1 : 0,
              child: Scaffold(
                backgroundColor: _pageColor,
                appBar: AppBar(
                  backgroundColor: _pageColor,
                  foregroundColor: Colors.white,
                  centerTitle: true,
                  title: const Text('选择线路'),
                  bottom: const TabBar(
                    tabs: [
                      Tab(text: '普通线路'),
                      Tab(text: '豪华线路'),
                    ],
                  ),
                ),
                body: TabBarView(
                  children: [
                    for (final premiumTab in [false, true])
                      ListView(
                        children: [
                          const ColoredBox(
                            color: _panelColor,
                            child: Padding(
                              padding: EdgeInsets.symmetric(
                                horizontal: 24,
                                vertical: 14,
                              ),
                              child: Text(
                                '海外线路',
                                style: TextStyle(color: _muted),
                              ),
                            ),
                          ),
                          for (final region in regions.entries)
                            _regionRow(
                              routeContext,
                              region.key,
                              premiumTab ? '豪华${region.value}专线' : region.value,
                              locked: premiumTab && !premium,
                            ),
                        ],
                      ),
                  ],
                ),
              ),
            ),
          );
        },
      ),
    );
    if (!mounted || node == null || _busy || _isConnecting) return;
    if (_isConnected) {
      await _connectTo(node, switchRegion: true);
    } else {
      setState(() => _selectedNode = node);
    }
  }

  String _regionCode(VpnNode node) => node.region.trim().toUpperCase();

  Widget _regionRow(
    BuildContext routeContext,
    String code,
    String name, {
    required bool locked,
  }) {
    final nodes = _nodes.where((node) => _regionCode(node) == code);
    final node =
        nodes.where((node) => node.connectable).firstOrNull ??
        nodes.firstOrNull;
    final selected =
        !locked && _selectedNode != null && _regionCode(_selectedNode!) == code;
    final available = node?.connectable == true;
    return ListTile(
      contentPadding: const EdgeInsets.symmetric(horizontal: 24, vertical: 5),
      leading: _flags[code] == null
          ? const Icon(Icons.public_rounded)
          : Text(_flags[code]!, style: const TextStyle(fontSize: 24)),
      title: Text(name),
      selected: selected,
      selectedColor: _gold,
      onTap:
          locked || node == null || (_isConnected && (selected || !available))
          ? null
          : () => Navigator.of(routeContext).pop(node),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (locked)
            const Text('豪华会员可用', style: TextStyle(color: _muted))
          else if (!available)
            Text(
              node == null ? '待发布' : '待配置',
              style: const TextStyle(color: _muted),
            ),
          const SizedBox(width: 12),
          Icon(
            selected ? Icons.radio_button_checked : Icons.radio_button_off,
            size: 20,
            color: selected ? _gold : _muted,
          ),
        ],
      ),
    );
  }

  Future<void> _toggleTunnel() async {
    setState(() => _message = null);
    if (_isConnected) {
      await widget.tunnel.disconnect();
      if (widget.tunnel.current.state == TunnelState.disconnected) {
        await _releaseLease();
      }
      return;
    }
    await _connectTo(_selectedNode);
  }

  Future<void> _connectTo(
    VpnNode? requested, {
    bool switchRegion = false,
  }) async {
    if (_busy) return;
    final device = _device;
    if (requested == null || device == null) {
      setState(() => _message = '当前没有可连接节点。');
      return;
    }
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      if (switchRegion) {
        final disconnected = widget.tunnel.changes
            .firstWhere(
              (state) =>
                  state.state == TunnelState.disconnected ||
                  state.state == TunnelState.error ||
                  state.state == TunnelState.unavailable,
            )
            .timeout(const Duration(seconds: 20));
        await Future.wait([
          widget.tunnel.disconnect().timeout(const Duration(seconds: 20)),
          disconnected,
        ]);
        if (widget.tunnel.current.state != TunnelState.disconnected) {
          throw const ApiException('未能断开当前地区，请稍后再试。');
        }
        if (!mounted) return;
        _selectedNode = requested;
      }
      await _loadNodes();
      final region = _regionCode(requested);
      final regional =
          widget.api.regionalLeases && _nodeRegions.containsKey(region);
      VpnNode? selectedEndpoint(String? endpointId) {
        if (endpointId != null) {
          return _nodes
              .where((node) => node.endpointId == endpointId)
              .firstOrNull;
        }
        final candidates = _nodes.where((node) => _regionCode(node) == region);
        return candidates.where((node) => node.connectable).firstOrNull ??
            candidates.firstOrNull;
      }

      var node = selectedEndpoint(null);
      if (!mounted) return;
      final requiresLease = widget.api.connectionLeases;
      if (node == null || (!requiresLease && !node.connectable)) {
        setState(() => _message = '所选节点已不可用，请重新选择。');
        return;
      }
      if (requiresLease) {
        if (!await _releaseLease()) {
          throw const ApiException('上次连接名额尚未释放，请稍后重试。');
        }
        final clock = Stopwatch()..start();
        final lease = await widget.api.reserveConnection(
          device.id,
          region: regional ? region : null,
        );
        if (!mounted) {
          await widget.api.releaseConnection(lease);
          return;
        }
        _leaseInvalid = false;
        _trackLease(lease, clock.elapsed);
        if (regional && lease.endpointId == null) {
          throw StateError('服务器未分配连接线路，请重试');
        }
        node = selectedEndpoint(lease.endpointId);
        for (
          var attempt = 0;
          attempt < 12 && node?.connectable != true;
          attempt++
        ) {
          await Future<void>.delayed(const Duration(seconds: 1));
          if (!mounted || _lease == null || _leaseInvalid) break;
          await _loadNodes();
          node = selectedEndpoint(lease.endpointId);
        }
        if (!mounted ||
            _lease == null ||
            _leaseInvalid ||
            node == null ||
            !node.connectable) {
          await _releaseLease();
          if (mounted) {
            setState(() => _message = '节点配置尚未就绪，请稍后重试。');
          }
          return;
        }
      }
      await widget.tunnel.connect(node);
      if (requiresLease &&
          (_lease == null || _leaseInvalid) &&
          widget.tunnel.current.state == TunnelState.connected) {
        await widget.tunnel.disconnect();
      }
      if (widget.tunnel.current.state != TunnelState.connected) {
        await _releaseLease();
      }
    } on TimeoutException {
      if (mounted) setState(() => _message = '等待 VPN 断开超时，请稍后重试。');
    } catch (error) {
      if (widget.tunnel.current.state != TunnelState.connected &&
          widget.tunnel.current.state != TunnelState.disconnecting) {
        await _releaseLease();
      }
      if (mounted) setState(() => _message = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _trackLease(ConnectionLease lease, Duration elapsed) {
    _lease = lease;
    _renewal?.cancel();
    _leaseExpiry?.cancel();
    final remaining = lease.validFor - elapsed;
    _leaseExpiry = Timer(remaining, () => unawaited(_leaseLost(lease)));
    _scheduleRenewal(lease, const Duration(seconds: 30));
  }

  void _scheduleRenewal(ConnectionLease lease, Duration delay) {
    _renewal = Timer(delay, () async {
      final clock = Stopwatch()..start();
      try {
        final next = await widget.api.reserveConnection(
          lease.deviceId,
          token: lease.token,
        );
        if (mounted && !_leaseInvalid && identical(lease, _lease)) {
          _trackLease(next, clock.elapsed);
        }
      } catch (error) {
        if (!mounted || _leaseInvalid || !identical(lease, _lease)) return;
        final status = error is ApiException ? error.statusCode : null;
        if (status != null && status >= 400 && status < 500) {
          await _leaseLost(lease);
        } else {
          _scheduleRenewal(lease, const Duration(seconds: 5));
        }
      }
    });
  }

  Future<void> _leaseLost(ConnectionLease lease) async {
    if (_leaseInvalid || !identical(lease, _lease)) return;
    _leaseInvalid = true;
    _renewal?.cancel();
    _leaseExpiry?.cancel();
    await widget.tunnel.disconnect();
    if (widget.tunnel.current.state == TunnelState.disconnected) {
      await _releaseLease();
    }
    if (mounted) {
      setState(() => _message = 'VPN 连接名额已失效，请重新连接。');
    }
  }

  Future<bool> _releaseLease() async {
    final pending = _releaseInFlight;
    if (pending != null) return pending;
    final lease = _lease;
    if (lease == null) return true;
    _renewal?.cancel();
    _leaseExpiry?.cancel();
    final release = widget.api
        .releaseConnection(lease)
        .timeout(const Duration(seconds: 5))
        .then((_) => true, onError: (Object _) => false);
    _releaseInFlight = release;
    final released = await release;
    if (released && identical(_lease, lease)) _lease = null;
    if (identical(release, _releaseInFlight)) _releaseInFlight = null;
    return released;
  }

  Future<void> _refresh() async {
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      if (_account == null) {
        await widget.tunnel.initialize();
      } else {
        _account = await widget.api.loadAccount();
        await _loadDevices();
        await _loadPlans();
      }
    } catch (error) {
      if (mounted) setState(() => _message = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Widget _surface({
    required Widget child,
    EdgeInsetsGeometry padding = const EdgeInsets.all(18),
  }) => Container(
    width: double.infinity,
    padding: padding,
    decoration: BoxDecoration(
      color: Colors.white,
      borderRadius: BorderRadius.circular(22),
      border: Border.all(color: const Color(0xFFE8EEEE)),
      boxShadow: const [
        BoxShadow(
          color: Color(0x080D292E),
          blurRadius: 16,
          offset: Offset(0, 7),
        ),
      ],
    ),
    child: child,
  );

  InputDecoration _inputDecoration(String label, IconData icon) =>
      InputDecoration(
        labelText: label,
        prefixIcon: Icon(icon, size: 19),
        filled: true,
        fillColor: const Color(0xFFF7F9F9),
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: BorderSide.none,
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: const BorderSide(color: Color(0xFFE4EBEB)),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(14),
          borderSide: const BorderSide(color: Color(0xFF087E79), width: 1.4),
        ),
      );

  Widget _messageBox(String text) => Container(
    width: double.infinity,
    padding: const EdgeInsets.all(12),
    decoration: BoxDecoration(
      color: const Color(0xFFFFF4E5),
      borderRadius: BorderRadius.circular(12),
    ),
    child: Text(
      text,
      style: const TextStyle(
        color: Color(0xFF765824),
        fontSize: 12,
        height: 1.45,
      ),
    ),
  );

  String _date(DateTime value) =>
      '${value.year}-${value.month.toString().padLeft(2, '0')}-${value.day.toString().padLeft(2, '0')}';

  String _quotaLabel(VpnPlan plan) => plan.quotaBytes == null
      ? '不限流量'
      : '${(plan.quotaBytes! / 1000000000).toStringAsFixed(0)} GB';

  String _priceLabel(VpnPlan plan) => plan.priceCents == null
      ? '待定价'
      : '¥${(plan.priceCents! / 100).toStringAsFixed(2)}';

  IconData _platformIcon(String platform) => switch (platform) {
    'ios' => Icons.phone_iphone_rounded,
    'macos' => Icons.laptop_mac_rounded,
    'windows' => Icons.desktop_windows_rounded,
    'android' => Icons.phone_android_rounded,
    _ => Icons.devices_other_rounded,
  };
}

class _SupportPage extends StatefulWidget {
  const _SupportPage({required this.api});
  final ApiClient api;

  @override
  State<_SupportPage> createState() => _SupportPageState();
}

class _SupportPageState extends State<_SupportPage> {
  final _message = TextEditingController();
  bool _sending = false;
  String? _error;
  String? _ticket;

  @override
  void dispose() {
    _message.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    if (_sending || _message.text.trim().isEmpty) return;
    setState(() {
      _sending = true;
      _error = null;
    });
    try {
      final id = await widget.api.sendSupportMessage(_message.text.trim());
      if (mounted) setState(() => _ticket = id);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: _HomeScreenState._pageColor,
    appBar: AppBar(
      title: const Text('联系我们'),
      foregroundColor: Colors.white,
      backgroundColor: _HomeScreenState._pageColor,
    ),
    body: SafeArea(
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 560),
          child: ListView(
            padding: const EdgeInsets.all(24),
            children: [
              if (_ticket case final String ticket) ...[
                const Text(
                  '留言已发送',
                  style: TextStyle(color: _HomeScreenState._gold, fontSize: 23),
                ),
                const SizedBox(height: 12),
                SelectableText(
                  '工单号 $ticket',
                  style: const TextStyle(color: Colors.white),
                ),
              ] else ...[
                const Text(
                  '留言正文会转发到 26x 的 Telegram 客服。请勿填写密码或支付信息。',
                  style: TextStyle(color: _HomeScreenState._muted),
                ),
                const SizedBox(height: 16),
                Theme(
                  data: ThemeData.dark(useMaterial3: true),
                  child: TextField(
                    controller: _message,
                    enabled: !_sending,
                    onChanged: (_) => setState(() {}),
                    maxLength: 2000,
                    minLines: 5,
                    maxLines: 9,
                    decoration: const InputDecoration(
                      labelText: '客服留言',
                      border: OutlineInputBorder(),
                      alignLabelWithHint: true,
                    ),
                  ),
                ),
                if (_error case final String message)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 12),
                    child: Text(
                      message,
                      style: const TextStyle(color: Colors.orangeAccent),
                    ),
                  ),
                const SizedBox(height: 12),
                FilledButton(
                  onPressed: _sending || _message.text.trim().isEmpty
                      ? null
                      : _send,
                  style: FilledButton.styleFrom(
                    backgroundColor: _HomeScreenState._gold,
                    foregroundColor: const Color(0xFF292115),
                    minimumSize: const Size.fromHeight(48),
                  ),
                  child: Text(_sending ? '正在发送…' : '发送留言'),
                ),
              ],
            ],
          ),
        ),
      ),
    ),
  );
}
