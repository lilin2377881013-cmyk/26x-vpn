# iOS signed IPA delivery

## Agreed boundary

The project deliverable is a signed `.ipa` prepared for the owner to upload to App Store Connect/TestFlight. The owner handles upload, review submission, tester groups, and invitations.

## Current state

The GitHub Actions workflow builds an unsigned arm64 IPA with libXray and an embedded Packet Tunnel extension. The default app and extension bundle identifiers are `com.26xvpn.client` and `com.26xvpn.client.PacketTunnel`. App Store Connect requires the signed export described below.

## Required owner configuration

- Apple Developer Team ID and App Store Connect app record.
- Final reverse-domain bundle identifier owned by the developer account.
- Apple Distribution signing identity and App Store provisioning profiles for both targets.
- Network Extension `packet-tunnel-provider` capability for the app/extension identifiers.
- App-specific privacy wording, support URL, and export-compliance answers for encryption.

Supply signing assets through the build host's secure keychain or CI secret store. Set the signing team for both the Runner and PacketTunnel targets, archive the Runner scheme in Xcode, and export for App Store Connect distribution.

## Build output

- Archive with the app and packet-tunnel extension under the same team.
- Export an App Store Connect distribution IPA.
- Record build number, commit, core version, signing team, and artifact SHA-256 in a release manifest.
- Keep dSYMs and the exact source revision for crash symbolication.

## Release checks

- App and extension bundle identifiers match the provisioning profiles.
- Packet Tunnel entitlement is present in the signed extension.
- App invokes the native VPN manager and surfaces permission/certificate errors clearly.
- No test credentials, private keys, or production subscription tokens are embedded in the IPA.
- The build can be installed through the owner's TestFlight process after upload/review.

Apple's external TestFlight builds have a 90-day testing window and the first external build is reviewed. TestFlight upload and tester operations remain owner-operated.
