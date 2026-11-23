import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import '../models.dart';
import 'connection_errors.dart';

class ApiException implements Exception {
  const ApiException(this.message, {this.statusCode});
  final String message;
  final int? statusCode;

  @override
  String toString() => message;
}

class ConnectionLease {
  const ConnectionLease(
    this.deviceId,
    this.token,
    this.validFor, {
    this.endpointId,
  });

  final String deviceId;
  final String token;
  final Duration validFor;
  final String? endpointId;
}

class ApiClient {
  ApiClient({
    String? baseUrl,
    bool trialSession = false,
    this.requestTimeout = const Duration(seconds: 20),
  }) : _trialSession = trialSession,
       _baseUri = Uri.parse(
         baseUrl ??
             const String.fromEnvironment(
               'API_BASE_URL',
               defaultValue: 'http://127.0.0.1:8080',
             ),
       );

  final Duration requestTimeout;
  final Uri _baseUri;
  final bool _trialSession;
  ApiClient createTrialClient() => ApiClient(
    baseUrl: _baseUri.origin,
    trialSession: true,
    requestTimeout: requestTimeout,
  );
  final HttpClient _http = HttpClient()
    ..connectionTimeout = const Duration(seconds: 12);
  String get _deviceIdKey => _storageKey(
    account?.trialCode == null
        ? '26x_device_id/${account?.id ?? ""}'
        : '26x_trial_device_id',
  );
  String get _sessionCookieKey => _storageKey(
    _trialSession ? '26x_guest_session_cookie' : '26x_session_cookie',
  );
  String get _csrfCookieKey =>
      _storageKey(_trialSession ? '26x_guest_csrf_cookie' : '26x_csrf_cookie');
  final FlutterSecureStorage _secureStorage = const FlutterSecureStorage(
    mOptions: MacOsOptions(usesDataProtectionKeychain: false),
  );
  final Map<String, String> _cookies = {};
  String? _csrfToken;
  String? _localDeviceId;
  Account? account;
  bool connectionLeases = false;

  bool get isSignedIn => _cookies.containsKey('26x_session') && account != null;

  Future<Account> signIn(String email, String password) =>
      _authenticate('/api/v1/auth/session', email, password, '邮箱或密码不正确');

  Future<Account> register(String email, String password) =>
      _authenticate('/api/v1/auth/register', email, password, '注册失败，请稍后重试');

  Future<Account> _authenticate(
    String endpoint,
    String email,
    String password,
    String failureMessage,
  ) async {
    final result = await _request(
      'POST',
      endpoint,
      body: {'email': email.trim(), 'password': password},
    );
    if (result.statusCode != HttpStatus.created) {
      throw ApiException(
        _errorMessage(result.body, failureMessage),
        statusCode: result.statusCode,
      );
    }
    _captureCookies(result.cookies);
    await _persistSession();
    return loadAccount();
  }

  Future<String>? _trialInstallationLoad;

  Future<String> _trialInstallation() {
    return _trialInstallationLoad ??= _readTrialInstallation().catchError((
      Object error,
    ) {
      _trialInstallationLoad = null;
      throw error;
    });
  }

  Future<String> _readTrialInstallation() async {
    final key = _storageKey('26x_trial_installation');
    var installation = await _secureStorage.read(key: key);
    if (installation == null) {
      final random = Random.secure();
      installation = base64Url
          .encode(List.generate(32, (_) => random.nextInt(256)))
          .replaceAll('=', '');
      await _secureStorage.write(key: key, value: installation);
    }
    return installation;
  }

  Future<({String code, DateTime? endsAt})> identifyTrial(
    String platform,
  ) async {
    final result = await _request(
      'POST',
      '/api/v1/auth/trial',
      stage: '读取游客识别码 [API-TRIAL-ID]',
      body: {
        'installation': await _trialInstallation(),
        'platform': platform,
        'identifyOnly': true,
      },
    );
    if (result.statusCode != HttpStatus.ok) {
      throw ApiException(_errorMessage(result.body, '无法读取试用识别码'));
    }
    final json = _decode(result.body);
    return (
      code: json['code'] as String,
      endsAt: DateTime.tryParse(json['endsAt'] as String? ?? ''),
    );
  }

  Future<({Account account, Device device})> startTrial(String platform) async {
    final installation = await _trialInstallation();
    final result = await _request(
      'POST',
      '/api/v1/auth/trial',
      body: {'installation': installation, 'platform': platform},
    );
    if (result.statusCode != HttpStatus.created) {
      throw ApiException(_errorMessage(result.body, '试用暂未开放'));
    }
    final trial = await loadAccount();
    final registered = (await devices()).firstOrNull;
    if (registered == null) {
      throw const ApiException('找不到试用设备，请稍后重试。');
    }
    await rememberLocalDevice(registered.id);
    unawaited(syncDeviceSystem(registered.id));
    await _persistSession();
    return (account: trial, device: registered);
  }

  Future<String> sendSupportMessage(String message) async {
    final result = await _request(
      'POST',
      '/api/v1/me/support',
      csrf: true,
      body: {'message': message},
    );
    if (result.statusCode != HttpStatus.created) {
      throw ApiException(_errorMessage(result.body, '留言发送失败，请稍后重试'));
    }
    return _decode(result.body)['id'] as String;
  }

  Future<Uri> checkout(String planCode) async {
    final result = await _request(
      'POST',
      '/api/v1/me/checkout',
      csrf: true,
      body: {'planCode': planCode},
    );
    if (result.statusCode != HttpStatus.created) {
      throw ApiException(
        _errorMessage(result.body, '支付暂未开放'),
        statusCode: result.statusCode,
      );
    }
    final value = _decode(result.body)['payUrl'];
    final uri = value is String ? Uri.tryParse(value) : null;
    if (uri == null ||
        uri.scheme != 'https' ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty) {
      throw const ApiException('支付链接无效，请联系 26x 支持。');
    }
    return uri;
  }

  Future<bool> restoreSession() async {
    final token = await _secureStorage.read(key: _sessionCookieKey);
    final csrf = await _secureStorage.read(key: _csrfCookieKey);
    if (token == null || token.isEmpty || csrf == null || csrf.isEmpty) {
      return false;
    }
    _cookies['26x_session'] = token;
    _cookies['26x_csrf'] = csrf;
    try {
      await loadAccount(stage: '恢复账号 [API-SESSION-RESTORE]');
      return true;
    } on ApiException catch (error) {
      if (error.statusCode == HttpStatus.unauthorized) {
        await _clearSession();
        return false;
      }
      rethrow;
    }
  }

  Future<Account> loadAccount({String? stage}) async {
    final result = await _request('GET', '/api/v1/me', stage: stage);
    if (result.statusCode != HttpStatus.ok) {
      throw ApiException(
        _errorMessage(result.body, '无法读取账户信息'),
        statusCode: result.statusCode,
      );
    }
    final json = _decode(result.body);
    _csrfToken = json['csrfToken'] as String?;
    final next = Account.fromJson(json);
    if (account?.id != next.id) _localDeviceId = null;
    account = next;
    return account!;
  }

  Future<void> changePassword({
    required String currentPassword,
    required String newPassword,
  }) async {
    final result = await _request(
      'POST',
      '/api/v1/me/password',
      csrf: true,
      body: {'currentPassword': currentPassword, 'newPassword': newPassword},
    );
    if (result.statusCode != HttpStatus.noContent) {
      throw ApiException(
        _errorMessage(result.body, '密码更新失败'),
        statusCode: result.statusCode,
      );
    }
    _cookies.clear();
    await _clearSession();
  }

  Future<List<Device>> devices() async {
    final result = await _request('GET', '/api/v1/me/devices');
    if (result.statusCode != HttpStatus.ok) {
      throw ApiException(
        _errorMessage(result.body, '无法读取设备列表'),
        statusCode: result.statusCode,
      );
    }
    final items = _decode(result.body)['items'] as List<dynamic>? ?? const [];
    return items
        .whereType<Map<String, dynamic>>()
        .map(Device.fromJson)
        .toList();
  }

  Future<List<VpnPlan>> availablePlans() async {
    final result = await _request('GET', '/api/v1/plans');
    if (result.statusCode != HttpStatus.ok) {
      throw ApiException(
        _errorMessage(result.body, '无法读取套餐列表'),
        statusCode: result.statusCode,
      );
    }
    final items = _decode(result.body)['items'] as List<dynamic>? ?? const [];
    return items
        .whereType<Map<String, dynamic>>()
        .map(VpnPlan.fromJson)
        .toList();
  }

  Future<Device> registerDevice({
    required String platform,
    required String label,
  }) async {
    final key = _storageKey('26x_device_installation_v1');
    var installation = await _secureStorage.read(key: key);
    if (installation == null) {
      final random = Random.secure();
      installation = base64Url.encode(
        List.generate(24, (_) => random.nextInt(256)),
      );
      await _secureStorage.write(key: key, value: installation);
    }
    final result = await _request(
      'POST',
      '/api/v1/me/devices',
      csrf: true,
      body: {
        'platform': platform,
        'label': label,
        'appVersion': '0.1.1',
        'installationToken': installation,
      },
    );
    if (result.statusCode != HttpStatus.created &&
        result.statusCode != HttpStatus.ok) {
      throw ApiException(
        result.statusCode == HttpStatus.conflict
            ? '登录设备已满，请在「我的 → 已登录设备」移除一台。'
            : _errorMessage(result.body, '无法自动登记本机'),
        statusCode: result.statusCode,
      );
    }
    final device = Device.fromJson(
      _decode(result.body)..['createdAt'] = DateTime.now().toIso8601String(),
    );
    _localDeviceId = device.id;
    await _secureStorage.write(key: _deviceIdKey, value: device.id);
    await syncDeviceSystem(device.id);
    return device;
  }

  Future<void> syncDeviceSystem(String deviceId) async {
    if (!Platform.isIOS) return;
    try {
      const channel = MethodChannel('26x/tunnel');
      final system = await channel.invokeMapMethod<String, String>(
        'deviceSystem',
      );
      if (system == null) return;
      await _request(
        'PUT',
        '/api/v1/me/devices/${Uri.encodeComponent(deviceId)}/system',
        csrf: true,
        body: system,
      );
    } on Exception {
      // Device reporting can retry on the next session without blocking VPN use.
    }
  }

  Future<String?> localDeviceId() async {
    final registered = _localDeviceId ??= await _secureStorage.read(
      key: _deviceIdKey,
    );
    if (registered != null) return registered;
    if (account?.trialCode == null) {
      final legacy = await _secureStorage.read(
        key: _storageKey('26x_device_id'),
      );
      if (legacy != null) return legacy;
    }
    const testDevice = String.fromEnvironment('VPN_TEST_DEVICE_ID');
    return kDebugMode && testDevice.isNotEmpty ? testDevice : null;
  }

  Future<void> rememberLocalDevice(String id) async {
    _localDeviceId = id;
    await _secureStorage.write(key: _deviceIdKey, value: id);
  }

  Future<void> updateDeviceLabel(String id, String label) async {
    final response = await _request(
      'PUT',
      '/api/v1/me/devices/${Uri.encodeComponent(id)}/label',
      csrf: true,
      body: {'label': label},
    );
    if (response.statusCode != HttpStatus.noContent) {
      throw ApiException(_errorMessage(response.body, '无法更新设备型号'));
    }
  }

  String _storageKey(String key) {
    // Preserve local preview storage while isolating production sessions.
    if (_baseUri.origin == 'http://127.0.0.1:18260') return key;
    return '${_baseUri.origin}/$key';
  }

  Future<void> revokeDevice(String deviceId) async {
    final result = await _request(
      'DELETE',
      '/api/v1/me/devices/${Uri.encodeComponent(deviceId)}',
      csrf: true,
    );
    if (result.statusCode != HttpStatus.noContent) {
      throw ApiException(
        _errorMessage(result.body, '设备撤销失败'),
        statusCode: result.statusCode,
      );
    }
    if (deviceId == await localDeviceId()) {
      _localDeviceId = null;
      await _secureStorage.delete(key: _deviceIdKey);
    }
  }

  bool regionalLeases = false;

  Future<List<VpnNode>> loadNodes({String? deviceId}) async {
    if (deviceId == null || deviceId.isEmpty) return const [];
    final query = '?deviceId=${Uri.encodeQueryComponent(deviceId)}';
    var result = await _request('GET', '/api/v1/nodes$query');
    final legacy = result.statusCode == HttpStatus.notFound;
    if (legacy) {
      result = await _request('GET', '/api/v1/client/bootstrap$query');
    }
    if (result.statusCode != HttpStatus.ok) {
      throw ApiException(
        _errorMessage(result.body, '无法读取节点列表'),
        statusCode: result.statusCode,
      );
    }
    final response = _decode(result.body);
    connectionLeases = response['connectionLeases'] == true;
    regionalLeases = response['regionalLeases'] == true;
    final items = response['items'] as List<dynamic>? ?? const [];
    final nodes = items.whereType<Map<String, dynamic>>();
    // Older servers list endpoints but serve their profiles separately.
    final missingProfiles = nodes
        .where(
          (node) =>
              !node.containsKey('connectionProfile') ||
              (node['connectable'] == true &&
                  !VpnNode.fromJson(node).connectable),
        )
        .toList();
    if (!legacy && missingProfiles.isNotEmpty) {
      final credentials = await _request(
        'GET',
        '/api/v1/devices/${Uri.encodeComponent(deviceId)}/credentials',
      );
      if (credentials.statusCode == HttpStatus.ok) {
        final issued = _decode(credentials.body)['items'] as List? ?? const [];
        final profiles = {
          for (final item in issued.whereType<Map<String, dynamic>>())
            item['endpointId']: item['connectionProfile'],
        };
        for (final node in missingProfiles) {
          final endpoint = node['endpoint'];
          if (endpoint is Map && profiles.containsKey(endpoint['id'])) {
            node['connectionProfile'] = profiles[endpoint['id']];
          }
        }
      } else if (credentials.statusCode != HttpStatus.notFound) {
        throw ApiException(
          _errorMessage(credentials.body, '无法读取设备凭据'),
          statusCode: credentials.statusCode,
        );
      }
    }
    return nodes.map(VpnNode.fromJson).toList();
  }

  Future<ConnectionLease> reserveConnection(
    String deviceId, {
    String? token,
    String? region,
  }) async {
    final result = await _request(
      'PUT',
      '/api/v1/me/devices/${Uri.encodeComponent(deviceId)}/connection-lease',
      csrf: true,
      body: {'token': ?token, 'region': ?region},
    );
    if (result.statusCode != HttpStatus.ok) {
      final error = _decode(result.body)['error'];
      final code = error is Map ? error['code'] : null;
      throw ApiException(switch (code) {
        'traffic_quota_exceeded' => '本月流量额度已用完。',
        'connection_limit_reached' => '已达到套餐同时在线上限，请先断开另一台设备。',
        'device_already_connected' => '本机仍占用一个连接名额，请稍后重试。',
        'connection_lease_expired' => '连接名额已失效，请重新连接。',
        'connection_releasing' => '上次连接正在关闭，请稍后重试。',
        'region_unavailable' => '该地区当前没有可分配服务器，请稍后重试或选择其他地区。',
        _ => '暂时无法确认 VPN 连接名额。',
      }, statusCode: result.statusCode);
    }
    final json = _decode(result.body);
    final issued = json['token'];
    final seconds = json['validForSeconds'];
    if (issued is! String || issued.isEmpty || seconds is! int || seconds < 1) {
      throw const ApiException('服务器返回的连接名额无效。');
    }
    return ConnectionLease(
      deviceId,
      issued,
      Duration(seconds: seconds),
      endpointId: json['endpointId'] as String?,
    );
  }

  Future<void> releaseConnection(ConnectionLease lease) async {
    final response = await _request(
      'DELETE',
      '/api/v1/me/devices/${Uri.encodeComponent(lease.deviceId)}/connection-lease',
      csrf: true,
      body: {'token': lease.token},
    );
    if (response.statusCode != HttpStatus.noContent) {
      throw const ApiException('无法释放上次连接名额，请重试。');
    }
  }

  Future<void> signOut() async {
    try {
      if (_cookies.containsKey('26x_session')) {
        await _request('DELETE', '/api/v1/auth/session', csrf: true);
      }
    } finally {
      await _clearSession();
    }
  }

  Future<_Response> _request(
    String method,
    String path, {
    bool csrf = false,
    Object? body,
    String? stage,
  }) async {
    final uri = _baseUri.resolve(path);
    final requestStage = stage ?? apiRequestStage(method, uri.path);
    if (uri.scheme != 'https' &&
        !{'localhost', '127.0.0.1', '::1'}.contains(uri.host)) {
      throw const ApiException('账户连接必须使用 HTTPS');
    }
    try {
      return await _sendRequest(
        method,
        uri,
        csrf: csrf,
        body: body,
        stage: requestStage,
      );
    } on HandshakeException {
      throw ApiException('$requestStage安全连接校验失败，请检查系统时间或联系支持。');
    } on HttpException {
      throw ApiException('$requestStage连接中断，请重试。');
    } on SocketException {
      throw ApiException('$requestStage无法连接，请检查网络后重试。');
    }
  }

  Future<_Response> _sendRequest(
    String method,
    Uri uri, {
    required bool csrf,
    required Object? body,
    required String stage,
  }) async {
    HttpClientRequest? activeRequest;
    var timedOut = false;
    Future<_Response> send() async {
      final request = await _http.openUrl(method, uri);
      activeRequest = request;
      if (timedOut) {
        request.abort();
        throw const ApiException('请求已取消');
      }
      request.headers.set(HttpHeaders.acceptHeader, 'application/json');
      request.headers.set(HttpHeaders.userAgentHeader, '26x/0.1.0');
      if (_cookies.isNotEmpty) {
        request.headers.set(
          HttpHeaders.cookieHeader,
          _cookies.entries.map((e) => '${e.key}=${e.value}').join('; '),
        );
      }
      if (csrf && _csrfToken != null) {
        request.headers.set('X-CSRF-Token', _csrfToken!);
      }
      if (body != null) {
        request.headers.contentType = ContentType.json;
        request.write(jsonEncode(body));
      }
      final response = await request.close();
      final responseBody = await utf8.decoder.bind(response).join();
      if (timedOut) throw const ApiException('请求已取消');
      final cookies = response.cookies
          .map((cookie) => '${cookie.name}=${cookie.value}')
          .toList();
      _captureCookies(cookies);
      return _Response(response.statusCode, responseBody, cookies);
    }

    return send().timeout(
      requestTimeout,
      onTimeout: () {
        timedOut = true;
        activeRequest?.abort();
        throw ApiException('$stage请求超时，请重试。');
      },
    );
  }

  void _captureCookies(List<String> setCookies) {
    for (final value in setCookies) {
      final pair = value.split(';').first.split('=');
      if (pair.length == 2 &&
          (pair.first == '26x_session' || pair.first == '26x_csrf')) {
        _cookies[pair.first] = pair.last;
      }
    }
  }

  Future<void> _persistSession() async {
    final token = _cookies['26x_session'];
    final csrf = _cookies['26x_csrf'];
    if (token != null) {
      await _secureStorage.write(key: _sessionCookieKey, value: token);
    }
    if (csrf != null) {
      await _secureStorage.write(key: _csrfCookieKey, value: csrf);
    }
  }

  Future<void> _clearSession() async {
    _cookies.clear();
    _csrfToken = null;
    account = null;
    _localDeviceId = null;
    await _secureStorage.delete(key: _sessionCookieKey);
    await _secureStorage.delete(key: _csrfCookieKey);
  }

  Map<String, dynamic> _decode(String body) {
    final value = jsonDecode(body);
    if (value is! Map<String, dynamic>) {
      throw const ApiException('服务器返回了无法识别的数据');
    }
    return value;
  }

  String _errorMessage(String body, String fallback) {
    try {
      final error = _decode(body)['error'];
      if (error is Map<String, dynamic> && error['message'] is String) {
        return error['message'] as String;
      }
    } catch (_) {
      return fallback;
    }
    return fallback;
  }

  void close() => _http.close(force: true);
}

class _Response {
  const _Response(this.statusCode, this.body, this.cookies);
  final int statusCode;
  final String body;
  final List<String> cookies;
}
