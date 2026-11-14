#!/usr/bin/env bash
set -euo pipefail

ENGINE_VERSION="v26.11.9"
GOMOBILE_VERSION="v0.0.0-20261011034528-197770444867"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLIENT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
PROJECT_ROOT="$(cd "${CLIENT_DIR}/.." && pwd)"
WORKSPACE_ROOT="$(cd "${CLIENT_DIR}/../../.." && pwd)"
SOURCE_DIR="${LIBXRAY_SOURCE_DIR:-${PROJECT_ROOT}/work/libXray-${ENGINE_VERSION}}"
TARGET="${1:-}"

case "${TARGET}" in
  android|ios|apple|macos) ;;
  *)
    echo "Usage: $0 android|ios|apple|macos" >&2
    exit 2
    ;;
esac

if ! command -v go >/dev/null 2>&1; then
  for go_bin_dir in \
    "${WORKSPACE_ROOT}/work/toolchains/go1.27.3/go/bin" \
    "${WORKSPACE_ROOT}/work/toolchains/go/bin"; do
    if [[ -x "${go_bin_dir}/go" ]]; then
      export PATH="${go_bin_dir}:${PATH}"
      break
    fi
  done
fi

if ! command -v go >/dev/null 2>&1; then
  echo "Go 1.27.3 or newer is required (automatic toolchain download is enabled by Go)." >&2
  exit 1
fi

if [[ ! -d "${SOURCE_DIR}/.git" ]]; then
  mkdir -p "$(dirname "${SOURCE_DIR}")"
  git clone --branch "${ENGINE_VERSION}" --depth 1 https://github.com/XTLS/libXray.git "${SOURCE_DIR}"
fi

PINNED_COMMIT="$(git -C "${SOURCE_DIR}" rev-parse --verify "refs/tags/${ENGINE_VERSION}^{commit}" 2>/dev/null || true)"
CURRENT_COMMIT="$(git -C "${SOURCE_DIR}" rev-parse HEAD 2>/dev/null || true)"
if [[ -z "${PINNED_COMMIT}" || "${CURRENT_COMMIT}" != "${PINNED_COMMIT}" ]]; then
  echo "${SOURCE_DIR} is not pinned to ${ENGINE_VERSION}; set LIBXRAY_SOURCE_DIR to a clean tagged checkout." >&2
  exit 1
fi

export LIBXRAY_GOMOBILE_VERSION="${GOMOBILE_VERSION}"
if [[ "${TARGET}" == "android" ]]; then
  if [[ -z "${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}" ]]; then
    echo "Set ANDROID_HOME or ANDROID_SDK_ROOT and install a JDK before building Android." >&2
    exit 1
  fi
  if ! java -version >/dev/null 2>&1; then
    echo "A working JDK is required to build the Android AAR." >&2
    exit 1
  fi
  python3 "${SOURCE_DIR}/build/main.py" android
  mkdir -p "${CLIENT_DIR}/android/app/libs"
  cp "${SOURCE_DIR}/libXray.aar" "${CLIENT_DIR}/android/app/libs/libXray.aar"
else
  if ! command -v xcodebuild >/dev/null 2>&1 || ! xcodebuild -version >/dev/null 2>&1; then
    echo "Full Xcode is required to build the Apple framework." >&2
    exit 1
  fi
  if [[ "${TARGET}" == "macos" || "${TARGET}" == "ios" ]]; then
    build_dir="$(mktemp -d "${TMPDIR:-/tmp}/26x-libxray.XXXXXXXX")"
    trap 'rm -rf "${build_dir}"' EXIT
    bind_target=macos
    bind_version=(-macosversion 13.0)
    if [[ "${TARGET}" == "ios" ]]; then
      bind_target=ios,iossimulator
      bind_version=(-iosversion 15.0)
    fi
    (
      cd "${SOURCE_DIR}"
      go build -mod=readonly -o "${build_dir}/gomobile" golang.org/x/mobile/cmd/gomobile
      go build -mod=readonly -o "${build_dir}/gobind" golang.org/x/mobile/cmd/gobind
      PATH="${build_dir}:${PATH}" "${build_dir}/gomobile" bind \
        -target "${bind_target}" "${bind_version[@]}" -o "${build_dir}/LibXray.xcframework" .
    )
    apple_framework="${build_dir}/LibXray.xcframework"
  else
    python3 "${SOURCE_DIR}/build/main.py" apple gomobile
    apple_framework="${SOURCE_DIR}/LibXray.xcframework"
  fi
  if [[ "${TARGET}" != "macos" ]]; then
    mkdir -p "${CLIENT_DIR}/ios/Frameworks"
    rm -rf "${CLIENT_DIR}/ios/Frameworks/LibXray.xcframework"
    cp -R "${apple_framework}" "${CLIENT_DIR}/ios/Frameworks/LibXray.xcframework"
  fi
  if [[ "${TARGET}" != "ios" ]]; then
    mkdir -p "${CLIENT_DIR}/macos/Frameworks"
    rm -rf "${CLIENT_DIR}/macos/Frameworks/LibXray.xcframework"
    cp -R "${apple_framework}" "${CLIENT_DIR}/macos/Frameworks/LibXray.xcframework"
  fi
fi

echo "libXray ${ENGINE_VERSION} ${TARGET} artifact installed."
