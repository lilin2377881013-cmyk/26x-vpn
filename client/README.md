# 26x client

This directory contains a shared Flutter app and generated runner projects for iOS,
macOS, Android, and Windows. The public app name is 26x. Each platform's application
identifier is still a placeholder and must be replaced with an identifier controlled
by the owner before release.

## Local development

From this directory:

```sh
flutter pub get
flutter analyze
flutter run --dart-define=API_BASE_URL=https://your-api-domain
```

Choose an installed device with `flutter devices` or pass its ID with `-d`. iOS and
macOS builds require full Xcode; a Windows build must be produced on Windows. The
runner projects were generated with Flutter 3.50 and their platform build targets
have not been compiled on this host.

Do not run `flutter create .` in this directory: that can overwrite the app's
`lib/main.dart`, package configuration, and analysis rules. If regenerating platform
runners, create a separate scratch Flutter app and copy only the desired platform
folders and `.metadata` file. Keep the existing Dart sources and `pubspec.yaml`.

## Native VPN integration status

- The shared Flutter MVP includes sign-in, account and device management, node
  selection, connect/disconnect states, and a client view for the Standard and
  Premium plan tiers. Plan periods, device limits, and quotas come from the API;
  client checkout and billing are not implemented. The screens are functional
  scaffolding, not a finished store-ready design.
- Android now has source for the `VpnService` permission flow, TUN lifecycle,
  protected outbound sockets, and the pinned libXray invocation. The native AAR is
  not checked in; build it with `scripts/build_libxray.sh android` after installing
  Go, a JDK, and Android SDK. This host does not currently have a working JDK or
  Android SDK, so the Android runner has not been compiled here.
- macOS has a Packet Tunnel system extension and an engine integration.
  See [macOS build and connection acceptance](platforms/macos/README.md) for the
  signed build and live verification steps. iOS needs a packet tunnel target.
  Windows needs a signed TUN/service adapter and authenticated IPC.
- The local API supports assigning existing protocol credentials to devices.
  A signed client requires a deployed API, an active test subscription, and a
  credential for the registered device.

## Native engine artifacts

The build helper pins libXray `v26.11.9` and its gomobile toolchain version. It
places the Android AAR under `android/app/libs/` and the Apple XCFramework under
`macos/Frameworks/` or `ios/Frameworks/`. For a Mac build, use
`scripts/build_libxray.sh macos`. The artifacts are generated locally. Apple
builds require full Xcode. Android builds require a JDK and Android SDK.

## App identifiers and signing

Replace `com.example.twentysixx_client` and the matching generated bundle IDs with an
owner-controlled reverse-DNS identifier. Keep the Android Kotlin package path aligned
with its namespace. Configure production signing through the platform's secure
keychain/secret store, never in source control. For the agreed signed iOS IPA handoff,
see [`../docs/ios-ipa-delivery.md`](../docs/ios-ipa-delivery.md).
