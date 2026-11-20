# Client release 0.1.8 (24) — 2026-11-20

Android and the Windows installer are published on the website. The macOS download remains the previous notarized version; macOS/iOS 0.1.8 source packages were prepared for owner signing.

Client source: `146293973ba460bb11e830cd0960389ed22b558c`. Server reconnect fix: `75c642c`.

## Published files

- android: https://26xvpn.com/downloads/26x-android-0.1.8.apk
  - SHA-256: `1676b6d9d52c7ab68d7e0f290d45a70b35c789c717634db3183c057db92af655`
- windows: https://26xvpn.com/downloads/26x-VPN-0.1.8-Setup-x64.exe
  - SHA-256: `ed380f81d2084fb4d94ea45f897394a15cfd21a19cb37f1b8f1df2c6a81ee084`

## Validation

- Android package is aligned, signed and verified; its signing certificate matches 0.1.7 for in-place upgrades.
- Windows CI passed client recovery tests, installer installation/uninstallation, runtime assets, worker validation, Wintun DNS/HTTPS and route cleanup.
- Origin and mirror file hashes matched before publishing. Public HTTPS HEAD and range checks returned the expected bytes; homepage and downloads API reference the new URLs.
- Apple source packages contain the corresponding native projects, shared Dart/tunnel code and pinned LibXray XCFramework; UTF-8 filenames, symbolic links, SHA-256 manifests and archive CRCs were verified. No signing keys, provisioning profiles or server secrets are included.
- Apple unsigned release compilation passed; the new Apple builds have not been signed, notarized, installed or accepted on user hardware.

See `connection-recovery-2026-11-20.md` for the server/client division and diagnostic codes.
