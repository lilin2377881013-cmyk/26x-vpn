import 'dart:async';
import 'dart:io' show Platform;

import 'package:material_ui/material_ui.dart';

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

  Future<void> _signIn() async {
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      _account = await widget.api.signIn(_email.text, _password.text);
      await _loadDevices();
      unawaited(_loadPlans());
      if (mounted) setState(() => _password.clear());
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
    if (_device != null) await _loadNodes();
    if (_device == null) {
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
          const Text(
            '使用管理员为你创建的账户登录。',
            style: TextStyle(color: Color(0xFF66777D), height: 1.5),
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
            autofillHints: const [AutofillHints.password],
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
                  : const Text(
                      '登录账户',
                      style: TextStyle(
                        fontSize: 15,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
            ),
          ),
          const SizedBox(height: 18),
          const Text(
            '账户由 26x 管理员开通；暂不支持客户端自助注册。',
            style: TextStyle(
              fontSize: 12,
              color: Color(0xFF809096),
              height: 1.5,
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
      backgroundColor: active ? background : const Color(0xFFF1F5F5),
      appBar: AppBar(
        backgroundColor: background,
        foregroundColor: Colors.white,
        centerTitle: true,
        title: Text(const ['26x', '套餐', '我的'][_tab]),
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

  Widget _detailsPage() => Center(
    child: ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 600),
      child: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          if (_tab == 1) ...[
            _planOverviewCard(),
            const SizedBox(height: 16),
            _planTierCard('standard', '普通档', Icons.shield_outlined),
            const SizedBox(height: 12),
            _planTierCard('premium', '豪华档', Icons.workspace_premium_outlined),
            if (_planError != null) _messageBox(_planError!),
            const SizedBox(height: 12),
            const Text('套餐由管理员开通。'),
          ] else
            _accountCard(),
          if (_message != null) ...[
            const SizedBox(height: 12),
            _messageBox(_message!),
          ],
        ],
      ),
    ),
  );

  Widget _accountCard() => _surface(
    padding: const EdgeInsets.all(20),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            const Icon(Icons.person_outline_rounded, color: Color(0xFF087E79)),
            const SizedBox(width: 9),
            const Text(
              '账户',
              style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Text(
                _account!.email,
                textAlign: TextAlign.end,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(color: Color(0xFF607178), fontSize: 12),
              ),
            ),
          ],
        ),
        Align(
          alignment: Alignment.centerRight,
          child: TextButton.icon(
            onPressed: _busy ? null : _changePassword,
            icon: const Icon(Icons.key_rounded, size: 15),
            label: const Text('修改密码', style: TextStyle(fontSize: 11)),
          ),
        ),
        const SizedBox(height: 16),
        Row(
          children: [
            const Text('本机设备', style: TextStyle(color: Color(0xFF6D7D83))),
            const Spacer(),
            if (_device != null)
              Text(
                _device!.label,
                style: const TextStyle(fontWeight: FontWeight.w600),
              ),
          ],
        ),
        if (_device == null) ...[
          const SizedBox(height: 12),
          const Text(
            '此安装尚未登记。登记后才能查看与本机相关的节点。',
            style: TextStyle(color: Color(0xFF718188), fontSize: 13),
          ),
          const SizedBox(height: 12),
          OutlinedButton.icon(
            onPressed: _busy ? null : _registerDevice,
            icon: const Icon(Icons.add_rounded),
            label: const Text('注册本机设备'),
          ),
        ],
        if (_devices.isNotEmpty) ...[
          const Divider(height: 28),
          const Text(
            '账户设备',
            style: TextStyle(color: Color(0xFF6D7D83), fontSize: 12),
          ),
          const SizedBox(height: 6),
          ..._devices.map(
            (device) => Padding(
              padding: const EdgeInsets.symmetric(vertical: 5),
              child: Row(
                children: [
                  Icon(
                    _platformIcon(device.platform),
                    size: 17,
                    color: const Color(0xFF698087),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      '${device.label}${device.id == _device?.id ? ' · 本机' : ''}',
                      style: const TextStyle(fontSize: 12),
                    ),
                  ),
                  TextButton(
                    onPressed: _busy ? null : () => _revokeDevice(device),
                    child: const Text('撤销', style: TextStyle(fontSize: 11)),
                  ),
                ],
              ),
            ),
          ),
        ],
        if (_account!.subscriptionEndsAt != null) ...[
          const Divider(height: 28),
          Row(
            children: [
              const Text('订阅有效期至', style: TextStyle(color: Color(0xFF6D7D83))),
              const Spacer(),
              Text(
                _date(_account!.subscriptionEndsAt!),
                style: const TextStyle(fontWeight: FontWeight.w600),
              ),
            ],
          ),
        ],
        const SizedBox(height: 12),
        TextButton.icon(
          onPressed: _busy ? null : _signOut,
          icon: const Icon(Icons.logout_rounded),
          label: const Text('退出登录'),
        ),
      ],
    ),
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

  Widget _planOverviewCard() => _surface(
    padding: const EdgeInsets.all(18),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Row(
          children: [
            Icon(Icons.workspace_premium_outlined, color: Color(0xFF087E79)),
            SizedBox(width: 9),
            Text(
              '套餐档位',
              style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
            ),
          ],
        ),
        Text(
          _account?.planName ?? '当前：暂未开通套餐',
          style: const TextStyle(color: Color(0xFF607178), fontSize: 13),
        ),
        const SizedBox(height: 12),
        Row(
          children: [
            _tierBadge('普通档', selected: _account?.planTier == 'standard'),
            const SizedBox(width: 8),
            _tierBadge('豪华档', selected: _account?.planTier == 'premium'),
          ],
        ),
      ],
    ),
  );

  Widget _tierBadge(String label, {required bool selected}) => Container(
    padding: const EdgeInsets.symmetric(horizontal: 11, vertical: 7),
    decoration: BoxDecoration(
      color: selected ? const Color(0xFFE1F3ED) : const Color(0xFFF2F5F5),
      borderRadius: BorderRadius.circular(20),
      border: Border.all(
        color: selected ? const Color(0xFF8CCAC0) : const Color(0xFFE5EBEB),
      ),
    ),
    child: Text(
      label,
      style: TextStyle(
        color: selected ? const Color(0xFF087E79) : const Color(0xFF65767B),
        fontSize: 12,
        fontWeight: FontWeight.w600,
      ),
    ),
  );

  Widget _planTierCard(String tier, String title, IconData icon) {
    final plans = _plans.where((plan) => plan.tier == tier).toList();
    return _surface(
      padding: const EdgeInsets.all(17),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, color: const Color(0xFF087E79)),
              const SizedBox(width: 9),
              Text(
                title,
                style: const TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const Spacer(),
              if (_account?.planTier == tier)
                const Text(
                  '当前档位',
                  style: TextStyle(
                    color: Color(0xFF13856C),
                    fontSize: 12,
                    fontWeight: FontWeight.w700,
                  ),
                ),
            ],
          ),
          const SizedBox(height: 10),
          if (plans.isEmpty)
            const Text(
              '此档位暂无已开放的套餐周期',
              style: TextStyle(color: Color(0xFF718188), fontSize: 13),
            )
          else
            ...plans.map(
              (plan) => Padding(
                padding: const EdgeInsets.only(top: 7),
                child: Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            plan.name,
                            style: const TextStyle(fontWeight: FontWeight.w600),
                          ),
                          const SizedBox(height: 3),
                          Text(
                            '${plan.durationDays} 天 · ${plan.maxDevices} 台 · ${_quotaLabel(plan)}',
                            style: const TextStyle(
                              color: Color(0xFF718188),
                              fontSize: 11,
                            ),
                          ),
                        ],
                      ),
                    ),
                    Text(
                      _priceLabel(plan),
                      style: const TextStyle(
                        fontSize: 16,
                        fontWeight: FontWeight.w800,
                        color: Color(0xFF087E79),
                      ),
                    ),
                  ],
                ),
              ),
            ),
        ],
      ),
    );
  }

  Widget _connectionCard() {
    const muted = Color(0xFFAAB6C6);
    final color = _isConnected
        ? const Color(0xFF71DBC3)
        : const Color(0xFFE2EAF4);
    final node = _selectedNode;
    final region = node == null ? null : _regionCode(node);
    final label = node == null ? '选择地区' : _nodeRegions[region] ?? node.name;
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

  Future<void> _showRegions() => showModalBottomSheet<void>(
    context: context,
    showDragHandle: true,
    useSafeArea: true,
    builder: (context) => SingleChildScrollView(
      padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('选择地区', style: Theme.of(context).textTheme.titleLarge),
          const SizedBox(height: 14),
          for (final region in _nodeRegions.entries)
            _nodeRegion(
              region.value,
              _nodes.where((node) => _regionCode(node) == region.key),
            ),
          ..._nodes
              .where((node) => !_nodeRegions.containsKey(_regionCode(node)))
              .map(_nodeTile),
        ],
      ),
    ),
  );

  String _regionCode(VpnNode node) => node.region.trim().toUpperCase();

  Widget _nodeRegion(String name, Iterable<VpnNode> nodes) {
    final node =
        nodes.where((node) => node.connectable).firstOrNull ??
        nodes.firstOrNull;
    if (node == null) {
      return ListTile(title: Text(name), trailing: const Text('待发布'));
    }
    return _nodeTile(node, name: name);
  }

  Widget _nodeTile(VpnNode node, {String? name}) {
    final selected =
        _selectedNode != null &&
        _regionCode(_selectedNode!) == _regionCode(node);
    return Padding(
      padding: const EdgeInsets.only(bottom: 9),
      child: InkWell(
        borderRadius: BorderRadius.circular(15),
        onTap:
            _busy ||
                _isConnecting ||
                (_isConnected && (selected || !node.connectable))
            ? null
            : () {
                Navigator.of(context).pop();
                if (_isConnected) {
                  unawaited(_connectTo(node, switchRegion: true));
                } else {
                  setState(() => _selectedNode = node);
                }
              },
        child: Container(
          padding: const EdgeInsets.all(13),
          decoration: BoxDecoration(
            color: selected ? const Color(0xFFE8F4F1) : const Color(0xFFF7F9F9),
            borderRadius: BorderRadius.circular(15),
            border: Border.all(
              color: selected
                  ? const Color(0xFF8CCAC0)
                  : const Color(0xFFE6ECEC),
            ),
          ),
          child: Row(
            children: [
              const Icon(Icons.public_rounded, color: Color(0xFF087E79)),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      name ?? node.name,
                      style: const TextStyle(fontWeight: FontWeight.w700),
                    ),
                  ],
                ),
              ),
              Text(
                node.connectable ? '可连接' : '待配置',
                style: TextStyle(
                  color: node.connectable
                      ? const Color(0xFF13856C)
                      : const Color(0xFF977942),
                  fontSize: 11,
                  fontWeight: FontWeight.w700,
                ),
              ),
            ],
          ),
        ),
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
