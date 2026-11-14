import 'dart:convert';

import '../models.dart';

/// Converts an authenticated, device-specific connection profile into the
/// minimal Xray runtime config used by the native tunnel service.
///
/// The operating-system TUN file descriptor is injected by the native adapter
/// immediately before the core starts. Credentials are never written to disk
/// or logged by this builder.
class XrayConfigBuilder {
  const XrayConfigBuilder._();

  static String build(ConnectionProfile profile) {
    _validate(profile);

    final Map<String, dynamic> outbound;
    if (profile.protocol == 'hysteria2') {
      outbound = _hysteria2(profile);
    } else if (profile.protocol == 'vless_reality') {
      outbound = _vlessReality(profile);
    } else {
      throw const FormatException('Unsupported VPN protocol');
    }

    final config = <String, dynamic>{
      'log': {'loglevel': 'warning'},
      'inbounds': [
        {
          'tag': '26x-tun',
          'port': 0,
          'protocol': 'tun',
          'settings': {
            'name': '26x-tun',
            'desc': '26x VPN',
            'mtu': 1500,
            'gateway': ['10.26.0.1/30', 'fd26:26::1/126'],
            'userLevel': 0,
            // The OS owns routes and DNS on mobile. Xray must bind its own
            // outbound sockets to the underlying network to avoid a loop.
            'autoOutboundsInterface': 'auto',
          },
        },
      ],
      'outbounds': [
        outbound,
        {'tag': 'block', 'protocol': 'blackhole'},
      ],
      'routing': {
        'domainStrategy': 'AsIs',
        'rules': [
          {
            'type': 'field',
            'inboundTag': ['26x-tun'],
            'outboundTag': '26x-proxy',
          },
        ],
      },
    };

    return jsonEncode(config);
  }

  static Map<String, dynamic> _hysteria2(ConnectionProfile profile) => {
    'tag': '26x-proxy',
    'protocol': 'hysteria',
    'settings': {'version': 2, 'address': profile.host, 'port': profile.port},
    'streamSettings': {
      'network': 'hysteria',
      'security': 'tls',
      'tlsSettings': {
        'serverName': profile.serverName,
        'allowInsecure': false,
        'alpn': profile.alpn ?? ['h3'],
      },
      'hysteriaSettings': {
        'version': 2,
        'auth': profile.credential,
        'udpIdleTimeout': 60,
      },
    },
  };

  static Map<String, dynamic> _vlessReality(ConnectionProfile profile) => {
    'tag': '26x-proxy',
    'protocol': 'vless',
    'settings': {
      'address': profile.host,
      'port': profile.port,
      'id': profile.credential,
      'encryption': 'none',
      if (profile.flow case final flow? when flow.isNotEmpty) 'flow': flow,
    },
    'streamSettings': {
      'network': 'raw',
      'security': 'reality',
      'realitySettings': {
        'serverName': profile.serverName,
        'fingerprint': profile.fingerprint,
        // Xray's current outbound schema names the Reality public key
        // `password` (the server inbound counterpart uses `privateKey`).
        'password': profile.publicKey,
        'shortId': profile.shortId,
        'spiderX': profile.spiderX ?? '/',
      },
    },
  };

  static void _validate(ConnectionProfile profile) {
    if (profile.host.trim().isEmpty ||
        profile.host.contains(RegExp(r'\s')) ||
        profile.port < 1 ||
        profile.port > 65535 ||
        profile.credential.isEmpty) {
      throw const FormatException('VPN profile is incomplete');
    }

    switch (profile.protocol) {
      case 'hysteria2':
        if (profile.serverName == null || profile.serverName!.isEmpty) {
          throw const FormatException('Hysteria2 TLS server name is missing');
        }
      case 'vless_reality':
        if (profile.serverName == null ||
            profile.serverName!.isEmpty ||
            profile.publicKey == null ||
            profile.publicKey!.isEmpty ||
            profile.shortId == null ||
            profile.fingerprint == null ||
            profile.fingerprint!.isEmpty) {
          throw const FormatException('VLESS Reality profile is incomplete');
        }
      default:
        throw const FormatException('Unsupported VPN protocol');
    }
  }
}
