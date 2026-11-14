# iOS signed IPA delivery

## Agreed boundary

The project deliverable is a signed `.ipa` prepared for the owner to upload to App Store Connect/TestFlight. The owner handles upload, review submission, tester groups, and invitations.

## Current state

No IPA can be produced from the current scaffold. The Dart client was analyzed using Flutter 3.50 and generated app runners are present, but this host has no full Xcode, Apple signing assets, Team ID, or Packet Tunnel entitlement. The Xcode project still lacks the separate packet-tunnel extension target, and no protocol engine is linked. The client therefore remains unavailable for real VPN traffic. Complete the engine and iOS integration milestones before creating a release archive.

## Required owner configuration

- Apple Developer Team ID and App Store Connect app record.
- Final reverse-domain bundle identifier owned by the developer account.
- App Store distribution signing identity and provisioning profile.
- Network Extension `packet-tunnel-provider` capability for the app/extension identifiers.
- App-specific privacy wording, support URL, and export-compliance answers for encryption.

Signing assets must be supplied through the build host's secure keychain or CI secret store, never committed to this repository or pasted into source files. The current workspace host only has Command Line Tools, not full Xcode, so it cannot produce an Apple archive or signed IPA.

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
