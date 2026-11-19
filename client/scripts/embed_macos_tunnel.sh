#!/bin/bash
set -euo pipefail

# The Runner target depends on PacketTunnel, so its product is ready here.
# Resolve the configured identifier at build time when another signing team
# uses an app ID different from the default project product reference.
extension_name="${VPN_TUNNEL_BUNDLE_ID:?}.systemextension"
extension_source="${BUILT_PRODUCTS_DIR:?}/${extension_name}"
extension_destination="${TARGET_BUILD_DIR:?}/${SYSTEM_EXTENSIONS_FOLDER_PATH:?}/${extension_name}"

if [[ ! -d "${extension_source}" ]]; then
  echo "error: Missing VPN system extension: ${extension_source}" >&2
  exit 1
fi

mkdir -p "$(dirname "${extension_destination}")"
/usr/bin/ditto "${extension_source}" "${extension_destination}"
