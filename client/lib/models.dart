class Account {
  const Account({
    required this.id,
    required this.email,
    required this.status,
    this.trialCode,
    this.planName,
    this.planTier,
    this.subscriptionEndsAt,
  });

  final String id;
  final String email;
  final String status;
  final String? trialCode;
  final String? planName;
  final String? planTier;
  final DateTime? subscriptionEndsAt;

  factory Account.fromJson(Map<String, dynamic> json) => Account(
    id: json['id'] as String? ?? '',
    email: json['email'] as String? ?? '',
    status: json['status'] as String? ?? 'unknown',
    trialCode: json['trialCode'] as String?,
    planName: json['planName'] as String?,
    planTier: json['planTier'] as String?,
    subscriptionEndsAt: DateTime.tryParse(
      json['subscriptionEndsAt'] as String? ?? '',
    ),
  );
}

class VpnPlan {
  const VpnPlan({
    required this.id,
    required this.code,
    required this.name,
    required this.tier,
    required this.durationDays,
    required this.maxDevices,
    this.priceCents,
    this.currency = 'CNY',
    this.quotaBytes,
  });

  final String id;
  final String code;
  final String name;
  final String tier;
  final int durationDays;
  final int maxDevices;
  final int? priceCents;
  final String currency;
  final int? quotaBytes;

  factory VpnPlan.fromJson(Map<String, dynamic> json) => VpnPlan(
    id: json['id'] as String? ?? '',
    code: json['code'] as String? ?? '',
    name: json['name'] as String? ?? '',
    tier: json['tier'] as String? ?? 'standard',
    durationDays: json['durationDays'] as int? ?? 0,
    maxDevices: json['maxDevices'] as int? ?? 1,
    priceCents: json['priceCents'] as int?,
    currency: json['currency'] as String? ?? 'CNY',
    quotaBytes: json['quotaBytes'] as int?,
  );
}

class VpnNode {
  const VpnNode({
    required this.id,
    required this.code,
    required this.name,
    required this.region,
    required this.status,
    required this.protocol,
    required this.host,
    required this.port,
    required this.transport,
    required this.connectable,
    required this.credentialStatus,
    this.endpointId = '',
    this.connectionProfile,
  });

  final String id;
  final String code;
  final String name;
  final String region;
  final String status;
  final String protocol;
  final String host;
  final int port;
  final String transport;
  final bool connectable;
  final String credentialStatus;
  final String endpointId;
  final ConnectionProfile? connectionProfile;

  bool matchesEndpoint(VpnNode other) =>
      id == other.id &&
      (endpointId.isNotEmpty && other.endpointId.isNotEmpty
          ? endpointId == other.endpointId
          : protocol == other.protocol &&
                host == other.host &&
                port == other.port);

  factory VpnNode.fromJson(Map<String, dynamic> json) {
    final endpoint = json['endpoint'] is Map<String, dynamic>
        ? json['endpoint'] as Map<String, dynamic>
        : <String, dynamic>{};
    final profileJson = json['connectionProfile'];
    final profile = profileJson is Map<String, dynamic>
        ? ConnectionProfile.fromJson(profileJson)
        : null;
    return VpnNode(
      id: json['id'] as String? ?? '',
      code: json['code'] as String? ?? '',
      name: json['name'] as String? ?? '未命名节点',
      region: json['region'] as String? ?? '',
      status: json['status'] as String? ?? 'unknown',
      protocol: endpoint['protocol'] as String? ?? '',
      host: endpoint['host'] as String? ?? '',
      port: endpoint['port'] as int? ?? 0,
      transport: endpoint['transport'] as String? ?? '',
      // A backend status flag alone cannot make a node connectable. The
      // authenticated API must also provide a complete per-device profile.
      connectable:
          json['connectable'] == true && (profile?.isSupported ?? false),
      credentialStatus: json['credentialStatus'] as String? ?? 'unavailable',
      endpointId: endpoint['id'] as String? ?? '',
      connectionProfile: profile,
    );
  }
}

/// A short-lived credential profile delivered by the authenticated API for
/// this device. Do not log, stringify, or include this object in analytics.
class ConnectionProfile {
  const ConnectionProfile({
    required this.protocol,
    required this.host,
    required this.port,
    required this.credential,
    this.serverName,
    this.publicKey,
    this.shortId,
    this.fingerprint,
    this.flow,
    this.spiderX,
    this.alpn,
    this.hysteriaObfsType,
    this.hysteriaObfsPassword,
  });

  final String protocol;
  final String host;
  final int port;
  final String credential;
  final String? serverName;
  final String? publicKey;
  final String? shortId;
  final String? fingerprint;
  final String? flow;
  final String? spiderX;
  final List<String>? alpn;
  final String? hysteriaObfsType;
  final String? hysteriaObfsPassword;

  bool get isSupported {
    if (host.trim().isEmpty ||
        host.contains(RegExp(r'\s')) ||
        port < 1 ||
        port > 65535 ||
        credential.isEmpty) {
      return false;
    }
    return switch (protocol) {
      // Xray's current Hysteria2 transport has no Salamander obfuscation
      // field. Fail closed instead of silently omitting server settings.
      'hysteria2' =>
        (serverName?.isNotEmpty ?? false) &&
            (hysteriaObfsType?.isEmpty ?? true) &&
            (hysteriaObfsPassword?.isEmpty ?? true),
      'vless_reality' =>
        (serverName?.isNotEmpty ?? false) &&
            (publicKey?.isNotEmpty ?? false) &&
            (shortId != null) &&
            (fingerprint?.isNotEmpty ?? false),
      _ => false,
    };
  }

  factory ConnectionProfile.fromJson(Map<String, dynamic> json) =>
      ConnectionProfile(
        protocol: json['protocol'] as String? ?? '',
        host: json['host'] as String? ?? '',
        port: json['port'] as int? ?? 0,
        credential: json['credential'] as String? ?? '',
        serverName: json['serverName'] as String?,
        publicKey: json['publicKey'] as String?,
        shortId: json['shortId'] as String?,
        fingerprint: json['fingerprint'] as String?,
        flow: json['flow'] as String?,
        spiderX: json['spiderX'] as String?,
        alpn: (json['alpn'] as List<dynamic>?)?.whereType<String>().toList(),
        hysteriaObfsType: json['obfsType'] as String?,
        hysteriaObfsPassword: json['obfsPassword'] as String?,
      );
}

class Device {
  const Device({
    required this.id,
    required this.label,
    required this.platform,
    required this.createdAt,
  });

  final String id;
  final String label;
  final String platform;
  final DateTime? createdAt;

  factory Device.fromJson(Map<String, dynamic> json) => Device(
    id: json['id'] as String? ?? '',
    label: json['label'] as String? ?? '设备',
    platform: json['platform'] as String? ?? '',
    createdAt: DateTime.tryParse(json['createdAt'] as String? ?? ''),
  );
}
