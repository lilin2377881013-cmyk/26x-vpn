import 'dart:async';
import 'dart:io' show Platform;

import 'package:flutter/services.dart';

import '../models.dart';
import 'connection_errors.dart';
import 'windows_tunnel.dart';
import 'xray_config_builder.dart';

enum TunnelState {
  disconnected,
  connecting,
  connected,
  disconnecting,
  unavailable,
  error,
}

class TunnelSnapshot {
  const TunnelSnapshot(this.state, {this.message});
  final TunnelState state;
  final String? message;
}

class TunnelController {
  static const _commands = MethodChannel('26x/tunnel');
  static const _events = EventChannel('26x/tunnel/events');

  late final _windows = WindowsTunnel(onState: _readNativeState);
  final _changes = StreamController<TunnelSnapshot>.broadcast();
  StreamSubscription<dynamic>? _nativeEvents;
  TunnelSnapshot _current = const TunnelSnapshot(TunnelState.disconnected);

  Stream<TunnelSnapshot> get changes => _changes.stream;
  TunnelSnapshot get current => _current;

  Future<void> initialize() async {
    if (Platform.isWindows) {
      _readNativeState(_windows.status);
      return;
    }
    try {
      _nativeEvents ??= _events.receiveBroadcastStream().listen(
        _readNativeState,
        onError: (_) {
          _emit(
            const TunnelSnapshot(
              TunnelState.unavailable,
              message: '系统 VPN 模块尚未配置',
            ),
          );
        },
      );
      final status = await _commands
          .invokeMapMethod<String, dynamic>('status')
          .timeout(const Duration(seconds: 15));
      if (status == null) {
        _emit(
          const TunnelSnapshot(
            TunnelState.unavailable,
            message: '系统 VPN 模块尚未配置',
          ),
        );
        return;
      }
      _readNativeState(status);
    } on MissingPluginException {
      _emit(
        const TunnelSnapshot(
          TunnelState.unavailable,
          message: '当前平台的 VPN 接入模块尚未安装',
        ),
      );
    } on TimeoutException {
      _emit(
        const TunnelSnapshot(
          TunnelState.error,
          message: '状态读取 [VPN-STATUS-TIMEOUT]：系统未返回 VPN 状态，请重试。',
        ),
      );
    } on PlatformException {
      _emit(
        const TunnelSnapshot(
          TunnelState.error,
          message: '状态读取 [VPN-STATUS]：无法读取系统 VPN 状态',
        ),
      );
    }
  }

  Future<void> connect(VpnNode node) async {
    final profile = node.connectionProfile;
    if (!node.connectable || profile == null) {
      _emit(
        const TunnelSnapshot(
          TunnelState.unavailable,
          message: '此节点还没有分配设备专属凭据，暂时不能连接',
        ),
      );
      return;
    }
    _emit(const TunnelSnapshot(TunnelState.connecting));
    try {
      final xrayJson = XrayConfigBuilder.build(profile);
      final response =
          await (Platform.isWindows
                  ? _windows.connect(xrayJson)
                  : _commands.invokeMapMethod<String, dynamic>('connect', {
                      'nodeId': node.id,
                      'xrayJson': xrayJson,
                    }))
              .timeout(const Duration(seconds: 60));
      if (response?['state'] == 'connected') {
        _emit(const TunnelSnapshot(TunnelState.connected));
      } else {
        _emit(
          TunnelSnapshot(
            TunnelState.unavailable,
            message: _safeEngineMessage(response?['code'] as String? ?? ''),
          ),
        );
      }
    } on MissingPluginException {
      _emit(
        const TunnelSnapshot(
          TunnelState.unavailable,
          message: '当前平台的 VPN 接入模块尚未安装',
        ),
      );
    } on PlatformException catch (error) {
      _emit(
        TunnelSnapshot(
          TunnelState.error,
          message: _safeEngineMessage(error.code),
        ),
      );
    } on TimeoutException {
      await disconnect();
      _emit(
        TunnelSnapshot(
          TunnelState.error,
          message: _safeEngineMessage('tunnel_start_timeout'),
        ),
      );
    } on FormatException {
      _emit(
        const TunnelSnapshot(
          TunnelState.error,
          message: '节点连接配置无效，请联系 26x 支持。',
        ),
      );
    } catch (_) {
      _emit(const TunnelSnapshot(TunnelState.error, message: '连接失败，请稍后重试'));
    }
  }

  Future<void> disconnect() async {
    _emit(const TunnelSnapshot(TunnelState.disconnecting));
    try {
      final response =
          await (Platform.isWindows
                  ? _windows.disconnect()
                  : _commands.invokeMapMethod<String, dynamic>('disconnect'))
              .timeout(const Duration(seconds: 20));
      if (response != null) {
        _readNativeState(response);
      } else {
        _emit(
          TunnelSnapshot(
            TunnelState.error,
            message: _safeEngineMessage('tunnel_stop_failed'),
          ),
        );
      }
    } on TimeoutException {
      _emit(
        TunnelSnapshot(
          TunnelState.error,
          message: _safeEngineMessage('tunnel_stop_timeout'),
        ),
      );
    } on MissingPluginException {
      _emit(
        const TunnelSnapshot(
          TunnelState.unavailable,
          message: '当前平台的 VPN 接入模块尚未安装',
        ),
      );
    } on PlatformException catch (error) {
      _emit(
        TunnelSnapshot(
          TunnelState.error,
          message: _safeEngineMessage(error.code),
        ),
      );
    }
  }

  String _safeEngineMessage(String code) =>
      tunnelErrorMessage(code, windows: Platform.isWindows);

  void _readNativeState(dynamic value) {
    if (value is! Map) return;
    final name = value['state'] as String?;
    final code = value['code'] as String?;
    final message = code != null
        ? _safeEngineMessage(code)
        : value['message'] as String?;
    final state = switch (name) {
      'connecting' => TunnelState.connecting,
      'connected' => TunnelState.connected,
      'disconnecting' => TunnelState.disconnecting,
      'unavailable' => TunnelState.unavailable,
      'error' => TunnelState.error,
      _ => TunnelState.disconnected,
    };
    _emit(TunnelSnapshot(state, message: message));
  }

  void _emit(TunnelSnapshot snapshot) {
    _current = snapshot;
    if (!_changes.isClosed) _changes.add(snapshot);
  }

  Future<void> dispose() async {
    if (Platform.isWindows) await _windows.disconnect();
    await _nativeEvents?.cancel();
    await _changes.close();
  }
}
