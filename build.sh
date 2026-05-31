#!/usr/bin/env bash
# Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
# SPDX-License-Identifier: MIT
# HDD APM Tool build script for Linux/macOS
#
# Usage:
#   ./build.sh                    # Build linux/amd64 + windows/amd64
#   ./build.sh -windows           # Build windows/amd64 + windows/arm64
#   ./build.sh -linux             # Build linux/amd64 + linux/arm64
#   ./build.sh -darwin            # Build darwin/amd64 + darwin/arm64
#   ./build.sh -amd64             # Build all platforms for amd64 only
#   ./build.sh -arm64             # Build all platforms for arm64 only
#   ./build.sh -linux -amd64      # Build linux/amd64 only
#   ./build.sh -linux -arm64      # Build linux/arm64 only
#   ./build.sh -windows -amd64    # Build windows/amd64 only
#   ./build.sh -windows -arm64    # Build windows/arm64 only
#   ./build.sh -darwin -amd64     # Build darwin/amd64 only
#   ./build.sh -darwin -arm64     # Build darwin/arm64 only
#   ./build.sh -all               # Build all platform/arch combinations (linux+windows+darwin)
#   ./build.sh -test              # Run unit tests
#   ./build.sh -coverage          # Run tests with coverage
#   ./build.sh -clean             # Clean build artifacts
#   ./build.sh -linux -deb        # Build linux + create .deb packages
#   ./build.sh -linux -rpm        # Build linux + create .rpm packages
#   ./build.sh -linux -deb -rpm   # Build linux + both .deb and .rpm
#
# All builds use CGO_ENABLED=0 (pure Go).
#
# Filename convention: hdd-apm-tool_<VERSION>-<OS>-<ARCH>[.exe]
set -e

BINARY="hdd-apm-tool"
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "dev")

# Read base version from version/version_base.txt (env VERSION overrides)
VERSION_BASE_FILE="$(dirname "$0")/version/version_base.txt"
if [ -z "${VERSION}" ] && [ -f "${VERSION_BASE_FILE}" ]; then
  VERSION=$(head -1 "${VERSION_BASE_FILE}" 2>/dev/null | tr -cd '0-9.')
fi
VERSION="${VERSION:-1.0.0}"

# Auto-increment build number (or use BUILD_NUMBER env var to pin an exact value)
# When BUILD_NUMBER is set externally, the file is NOT written -- callers manage versioning.
BUILD_NUMBER_FILE="$(dirname "$0")/version/build_number.txt"
SKIP_BUILD_NUMBER_BUMP=false
if [ -n "${BUILD_NUMBER}" ]; then
  SKIP_BUILD_NUMBER_BUMP=true
else
  BUILD_NUMBER=0
  if [ -f "${BUILD_NUMBER_FILE}" ]; then
    BUILD_NUMBER=$(head -1 "${BUILD_NUMBER_FILE}" 2>/dev/null | tr -cd '0-9')
    [ -z "${BUILD_NUMBER}" ] && BUILD_NUMBER=0
  fi
fi
if ! $SKIP_BUILD_NUMBER_BUMP; then
  # Use flock to protect against concurrent build invocations producing
  # duplicate build numbers or corrupting the file. Falls back to a
  # non-atomic write when flock is not available.
  (
    exec 200>"${BUILD_NUMBER_FILE}.lock"
    flock -x 200
    _cur=$(head -1 "${BUILD_NUMBER_FILE}" 2>/dev/null | tr -cd '0-9')
    _cur=$([ -n "${_cur}" ] && echo "${_cur}" || echo "0")
    printf '%s\n' "$((_cur + 1))" > "${BUILD_NUMBER_FILE}"
  ) 2>/dev/null || printf '%s\n' "$((BUILD_NUMBER + 1))" > "${BUILD_NUMBER_FILE}"
  # Re-read from the file so this shell sees the value actually written,
  # even if a concurrent process incremented it first.
  BUILD_NUMBER=$(head -1 "${BUILD_NUMBER_FILE}" 2>/dev/null | tr -cd '0-9')
  [ -z "${BUILD_NUMBER}" ] && BUILD_NUMBER=1
fi

FULL_VERSION="${VERSION}.${BUILD_NUMBER}"
MODULE="github.com/secu-tools/hdd-apm-tool/internal/app"
LDFLAGS="-X ${MODULE}.version=${VERSION} -X ${MODULE}.commit=${COMMIT} -X ${MODULE}.buildNumber=${BUILD_NUMBER}"
BUILD_DIR="build"

echo "HDD APM Tool Build Script"
echo "========================"
echo "Version: ${FULL_VERSION}"
echo "Commit:  ${COMMIT}"
echo ""

# -- Detect nfpm (needed for -deb / -rpm packaging) --------------------
NFPM_AVAILABLE=false
if command -v nfpm &>/dev/null; then
  NFPM_AVAILABLE=true
  echo "nfpm:    found ($(command -v nfpm))"
fi
echo ""

# Parse arguments
BUILD_WINDOWS=false
BUILD_LINUX=false
BUILD_DARWIN=false
INCLUDE_AMD64=false
INCLUDE_ARM64=false
RUN_TEST=false
RUN_COVERAGE=false
RUN_CLEAN=false
BUILD_DEB=false
BUILD_RPM=false
HAS_PLATFORM=false
HAS_ARCH=false

for arg in "$@"; do
  case "$arg" in
    -windows)  BUILD_WINDOWS=true; HAS_PLATFORM=true ;;
    -linux)    BUILD_LINUX=true;   HAS_PLATFORM=true ;;
    -darwin)   BUILD_DARWIN=true;  HAS_PLATFORM=true ;;
    -amd64)    INCLUDE_AMD64=true; HAS_ARCH=true ;;
    -arm64)    INCLUDE_ARM64=true; HAS_ARCH=true ;;
    -all)      BUILD_WINDOWS=true; BUILD_LINUX=true; BUILD_DARWIN=true; INCLUDE_AMD64=true; INCLUDE_ARM64=true; HAS_PLATFORM=true; HAS_ARCH=true ;;
    -test)     RUN_TEST=true ;;
    -coverage) RUN_COVERAGE=true ;;
    -clean)    RUN_CLEAN=true ;;
    -deb)      BUILD_DEB=true ;;
    -rpm)      BUILD_RPM=true ;;
    *)         echo "Unknown flag: $arg"; exit 1 ;;
  esac
done

# ------------------------------------------------------------------
# Clean
# ------------------------------------------------------------------
if $RUN_CLEAN; then
  echo "Cleaning ${BUILD_DIR} ..."
  rm -rf "${BUILD_DIR}"
  echo "Done."
  exit 0
fi

# ------------------------------------------------------------------
# Test
# ------------------------------------------------------------------
if $RUN_TEST; then
  echo "Running tests ..."
  go test -buildvcs=false -count=1 ./...
  echo "Tests passed."
  exit 0
fi

if $RUN_COVERAGE; then
  echo "Running tests with coverage ..."
  go test -buildvcs=false ./... -coverprofile=coverage.out -count=1
  go tool cover -html=coverage.out -o coverage.html
  echo "Coverage report: coverage.html"
  exit 0
fi

# Auto-install nfpm if -deb or -rpm requested and not yet found
if ($BUILD_DEB || $BUILD_RPM) && ! $NFPM_AVAILABLE; then
  echo "nfpm: not found -- auto-installing..."
  go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest 2>/dev/null
  if command -v nfpm &>/dev/null; then
    NFPM_AVAILABLE=true
    echo "nfpm: installed ($(command -v nfpm))"
  else
    echo "ERROR: nfpm auto-install failed"
    echo "  Install manually: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest"
    exit 1
  fi
fi

# ------------------------------------------------------------------
# Determine targets
# ------------------------------------------------------------------
if ! $HAS_PLATFORM; then
  BUILD_WINDOWS=true
  BUILD_LINUX=true
fi
if ! $HAS_ARCH; then
  INCLUDE_AMD64=true
  INCLUDE_ARM64=true
fi

# Wipe build directory
rm -rf "${BUILD_DIR}"

# build_one <goos> <goarch> <ext>
build_one() {
  local goos="$1" goarch="$2" ext="$3"
  local subdir="${BUILD_DIR}/${goos}"
  mkdir -p "${subdir}"
  local outname="${BINARY}_${FULL_VERSION}-${goos}-${goarch}${ext}"
  local outpath="${subdir}/${outname}"
  echo "  Building ${outpath}..."
  if CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" \
      go build -buildvcs=false -trimpath -ldflags "${LDFLAGS} -s -w" -o "${outpath}" ./; then
    local size
    size=$(stat -f%z "${outpath}" 2>/dev/null || stat -c%s "${outpath}" 2>/dev/null || echo 0)
    echo "    -> $((size / 1024)) KB"
  else
    echo "    FAILED: ${outpath}"
    return 1
  fi
}

SUCCESS=0
FAILED=0
run_build() {
  if build_one "$@"; then SUCCESS=$((SUCCESS + 1)); else FAILED=$((FAILED + 1)); fi
}

if $BUILD_WINDOWS; then
  $INCLUDE_AMD64 && run_build "windows" "amd64" ".exe"
  $INCLUDE_ARM64 && run_build "windows" "arm64" ".exe"
fi
if $BUILD_LINUX; then
  $INCLUDE_AMD64 && run_build "linux" "amd64" ""
  $INCLUDE_ARM64 && run_build "linux" "arm64" ""
fi
if $BUILD_DARWIN; then
  $INCLUDE_AMD64 && run_build "darwin" "amd64" ""
  $INCLUDE_ARM64 && run_build "darwin" "arm64" ""
fi

echo ""
echo "Build complete: ${SUCCESS} succeeded, ${FAILED} failed."
[ "${FAILED}" -eq 0 ] || exit 1

# -- Package Linux binaries with nfpm if -deb or -rpm requested --------
package_nfpm() {
  local binary_path="$1" goarch="$2" format="$3"
  local pkg_arch
  if [ "$format" = "deb" ]; then
    case "$goarch" in
      amd64) pkg_arch="amd64" ;;
      arm64) pkg_arch="arm64" ;;
      *)     pkg_arch="$goarch" ;;
    esac
  else
    case "$goarch" in
      amd64) pkg_arch="x86_64" ;;
      arm64) pkg_arch="aarch64" ;;
      *)     pkg_arch="$goarch" ;;
    esac
  fi

  local out_dir
  out_dir=$(dirname "$binary_path")
  local base_name
  base_name=$(basename "$binary_path")
  local pkg_file="${out_dir}/${base_name}.${format}"

  local tmp_yaml
  tmp_yaml=$(mktemp /tmp/nfpm_XXXXXX.yaml)
  cat > "$tmp_yaml" <<NFPMEOF
name: hdd-apm-tool
arch: ${pkg_arch}
version: ${FULL_VERSION}
maintainer: Jack L. (Cpt-JackL) <https://jack-l.com>
description: HDD APM Tool - ATA Advanced Power Management configuration tool for spinning HDDs
homepage: https://github.com/secu-tools/hdd-apm-tool
license: MIT
contents:
  - src: ${binary_path}
    dst: /usr/bin/hdd-apm-tool
    file_info:
      mode: 0755
NFPMEOF

  echo "  Packaging ${pkg_file}..."
  if nfpm pkg --config "$tmp_yaml" --packager "$format" --target "$pkg_file"; then
    local size
    size=$(stat -f%z "$pkg_file" 2>/dev/null || stat -c%s "$pkg_file" 2>/dev/null || echo 0)
    echo "    -> $((size / 1024)) KB"
  else
    echo "    FAILED: ${pkg_file}"
  fi
  rm -f "$tmp_yaml"
}

if $NFPM_AVAILABLE && ($BUILD_DEB || $BUILD_RPM); then
  echo ""
  echo "Packaging Linux binaries..."
  for bin in "${BUILD_DIR}"/linux/hdd-apm-tool_*-linux-*; do
    [ -f "$bin" ] || continue
    case "$bin" in *.deb|*.rpm) continue ;; esac
    arch=""
    case "$bin" in
      *-linux-amd64) arch="amd64" ;;
      *-linux-arm64) arch="arm64" ;;
    esac
    [ -z "$arch" ] && continue
    $BUILD_DEB && package_nfpm "$bin" "$arch" "deb"
    $BUILD_RPM && package_nfpm "$bin" "$arch" "rpm"
  done
fi

echo ""
echo "Build complete. Output in ${BUILD_DIR}/"
find "${BUILD_DIR}" -type f | sort
