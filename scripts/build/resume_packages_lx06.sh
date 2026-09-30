#!/usr/bin/env bash
set -euo pipefail

source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../lib" && pwd)/paths.sh"

host_ip="$(sed -n 's/^nameserver[[:space:]]*//p' /etc/resolv.conf | head -n 1)"
proxy_url="http://${host_ip}:7897"

export http_proxy="$proxy_url"
export https_proxy="$proxy_url"
export HTTP_PROXY="$proxy_url"
export HTTPS_PROXY="$proxy_url"
export ALL_PROXY="$proxy_url"
export all_proxy="$proxy_url"
export no_proxy="localhost,127.0.0.1"
export NO_PROXY="$no_proxy"
export PATH="$XIAOAI_ROOT/tools/cmake-compat:$PATH"


cd "$XIAOAI_PATCH_DIR"
log="build-packages/build-lx06.log"
if MODEL=LX06 ./packages.sh -j4 >>"$log" 2>&1; then
  echo "PACKAGES_BUILD_OK"
else
  status=$?
  echo "PACKAGES_BUILD_FAILED=$status"
  exit "$status"
fi
