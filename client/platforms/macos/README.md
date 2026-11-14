# macOS tunnel integration

The macOS project in `../../macos` embeds a Packet Tunnel system extension for
Developer ID distribution. The extension uses Apple's packet flow and a local
datagram socket pair to transfer IP packets to and from the pinned libXray
engine. It checks HTTPS through the selected node before applying routes and
starting the engine. It then sends a DNS query through the running packet
transport and requires a matching response before reporting successful startup.
The app uses the state of its own system VPN session.

## Local testing with Xcode

1. Use a full Xcode compatible with the host and an Apple Development signing
   identity. Generate the engine with `scripts/build_libxray.sh macos` from the
   Flutter client directory.
2. Set `DEVELOPMENT_TEAM` in `macos/Runner/Configs/Signing.local.xcconfig` or
   pass it to `xcodebuild`. The Debug configuration uses automatic development
   signing for both the app and the packet tunnel system extension.
3. Prepare the Flutter build and then build the workspace. Set the API URL to
   the environment serving the test account and device credentials.

   ```sh
   FLUTTER_SWIFT_PACKAGE_MANAGER=true flutter pub get
   FLUTTER_SWIFT_PACKAGE_MANAGER=true flutter build macos --config-only --debug --no-pub --dart-define=API_BASE_URL=https://26xvpn.com
   xcodebuild -workspace macos/Runner.xcworkspace -scheme Runner -configuration Debug \
     -allowProvisioningUpdates -allowProvisioningDeviceRegistration build
   ```

## Direct distribution

1. Use a full Xcode compatible with the host and install Go 1.27.3 or later.
   From the Flutter client directory, run `scripts/build_libxray.sh macos`
   to generate the macOS XCFramework. The app targets macOS 13 or later.
2. In `macos/Runner/Configs/Signing.local.xcconfig`, set `DEVELOPMENT_TEAM`,
   `VPN_APP_PROFILE`, and `VPN_TUNNEL_PROFILE`. Use the Developer ID
   Application identity and separate provisioning profiles for
   `com.26xvpn.client` and `com.26xvpn.client.PacketTunnel`, or override
   `VPN_APP_BUNDLE_ID` with the registered app ID. The host needs the system
   extension install entitlement. Both profiles must support the Network
   Extension entitlement for Developer ID distribution.
3. Generate Flutter's Swift package with the Keychain plugin, then build.

   ```sh
   FLUTTER_SWIFT_PACKAGE_MANAGER=true flutter pub get
   FLUTTER_SWIFT_PACKAGE_MANAGER=true flutter build macos --release --dart-define=API_BASE_URL=https://26xvpn.com
   ```

   Check that the app contains the signed extension in
   `Contents/Library/SystemExtensions` before notarizing the app for direct distribution.

## Connection acceptance

Install the signed build in Applications. Sign in with a test subscription,
register this Mac and assign it a credential for the verified Hong Kong VLESS
endpoint. Open 26x and approve its system extension on the first connection.
Verify that a fresh application request and DNS lookup both reach the expected
exit via 26x, with the other VPN disconnected for this test. Disconnect 26x
and confirm that the original route and DNS are restored.

On 14 November 2026, an Apple Development-signed build on the test Mac
established a system VPN through the existing Hong Kong VLESS + REALITY node.
With Auto VPN disconnected, an external HTTPS request reported the node's
exit address and a DNS query through the tunnel returned an answer. After
disconnecting, the original hotspot interface and exit address were restored.
The same Mac test passed with a production HTTPS login and a separately
provisioned device credential. A second device was then registered through
the production API, automatically provisioned on the Hong Kong node, and
verified using the pinned Xray client. Revoking that second device stopped
fresh connections using its credential. Direct distribution using
Developer ID still requires verification.

The Hong Kong exit can carry HTTPS and DNS traffic. An unauthenticated OpenAI
API request from that exit returned HTTP 403 with
`unsupported_country_region_territory`; ChatGPT also returned HTTP 403. The
tunnel's generic connectivity check does not guarantee access to individual
services.

## IPv4 split routing

The system extension uses the bundled
[`direct-ipv4-cidrs.txt`](../../tunnel-assets/direct-ipv4-cidrs.txt)
snapshot to install 5,491 IPv4 exclusions. Matching destinations use the
underlying network interface. The provider keeps the VPN endpoint's host
exception and sets tunnel defaults for all other IPv4 destinations and IPv6.
The VPN DNS settings and IPv6 routing are unchanged by the IPv4 CIDR list.
