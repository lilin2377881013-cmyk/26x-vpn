# 26x client

The shared Flutter app runs on iOS, macOS, Android, and Windows. It uses
`GET /api/v1/nodes` and `GET /api/v1/devices/:id/credentials` with the user's
authenticated session. Older API deployments can continue to serve the
existing client bootstrap endpoint.

## Local development

```sh
flutter pub get
flutter analyze
flutter run --dart-define=API_BASE_URL=https://26xvpn.com
```

Sign in, register the device, and use a subscription with a credential
assigned to that device. Credentials are passed to each operating system's
VPN adapter at connection time.

## Native engines

Run the matching build helper before building the Flutter app.

| Platform | Engine build | System routing |
| --- | --- | --- |
| macOS | `scripts/build_libxray.sh macos` | Packet Tunnel system extension |
| iOS 15+ | `scripts/build_libxray.sh ios` | Packet Tunnel app extension |
| Android | `scripts/build_libxray.sh android` | VpnService |
| Windows | `scripts/build_windows_tunnel.sh amd64 /path/to/wintun.dll` | Wintun and Windows route table |

Build inputs include Go 1.27.3 or later and a JDK plus Android SDK for Android.
Apple builds need Xcode. Windows packaging needs Flutter and Visual Studio
on Windows. See the [macOS guide](platforms/macos/README.md) and
[Windows guide](platforms/windows/README.md).

The Apple extensions use the same Packet Tunnel provider and 5,491 IPv4
exclusions. Android uses native IPv4 exclusions from API 33 and equivalent
included routes on earlier versions. Windows similarly installs the IPv4
complement. Only IPv4 addresses present in the bundled CIDR file are included
in the direct IPv4 policy. IPv6 uses the VPN route.

## Signing and testing

For iOS, set `VPN_APP_BUNDLE_ID` to the registered app identifier and enable
the Network Extensions Packet Tunnel entitlement on the app ID and its
`.PacketTunnel` extension ID. The Xcode Runner scheme embeds that extension.
Use the signing team and compatible provisioning profiles for both. Follow
the [IPA handoff](../docs/ios-ipa-delivery.md) for distribution.

Before release, verify actual connection, a direct IPv4 site, foreign exit,
and restored route state after disconnect on each supported device. The
Windows development build requires administrator privileges to create
the adapter and routes.
