#!/usr/bin/env bash
set -euo pipefail

if [[ ${1:-} != amd64 && ${1:-} != arm64 ]]; then
  echo "Usage: $0 amd64|arm64 /path/to/wintun.dll" >&2
  exit 2
fi
if [[ ! -f ${2:-} ]]; then
  echo "Provide the official Wintun DLL for the selected Windows architecture." >&2
  exit 2
fi

client_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_dir="${LIBXRAY_SOURCE_DIR:-${client_dir}/../work/libXray-v26.11.9}"
driver_dir="$(cd "$(dirname "$2")" && pwd)"
driver="${driver_dir}/$(basename "$2")"
if [[ ! -d "${source_dir}/.git" ]]; then
  mkdir -p "$(dirname "${source_dir}")"
  git clone --branch v26.11.9 --depth 1 https://github.com/XTLS/libXray.git "${source_dir}"
fi

tagged_commit="$(git -C "${source_dir}" rev-parse 'refs/tags/v26.11.9^{commit}')"
if [[ "$(git -C "${source_dir}" rev-parse HEAD)" != "${tagged_commit}" ]]; then
  echo "Use a libXray checkout at v26.11.9." >&2
  exit 1
fi

output="${client_dir}/windows/runtime"
mkdir -p "${output}"
(
  cd "${source_dir}"
  CGO_ENABLED=0 GOOS=windows GOARCH="$1" go build -mod=readonly -trimpath \
    -o "${output}/26x-tunnel.exe" "${client_dir}/windows/tunnel/main.go"
)
cp "${driver}" "${output}/wintun.dll"
